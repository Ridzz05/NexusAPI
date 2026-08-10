package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Ridzz05/NexusAPI/internal/access"
	"github.com/Ridzz05/NexusAPI/internal/attendance"
	"github.com/Ridzz05/NexusAPI/internal/config"
	"github.com/Ridzz05/NexusAPI/internal/integration/loyalfitness"
	"github.com/Ridzz05/NexusAPI/internal/platform/cache"
	"github.com/Ridzz05/NexusAPI/internal/platform/events"
	"github.com/Ridzz05/NexusAPI/internal/platform/migrations"
	"github.com/Ridzz05/NexusAPI/internal/platform/postgres"
	"github.com/Ridzz05/NexusAPI/internal/platform/redisstore"
	"github.com/Ridzz05/NexusAPI/internal/server"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	cfg, err := config.Load()
	if err != nil {
		logger.Error("invalid configuration", "error", err)
		os.Exit(1)
	}

	rootContext, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	pool, err := postgres.Open(rootContext, cfg.DatabaseURL, cfg.DBMinConns, cfg.DBMaxConns)
	if err != nil {
		logger.Error("database startup failed", "error", err)
		os.Exit(1)
	}
	defer pool.Close()
	if err := migrations.Apply(rootContext, pool); err != nil {
		logger.Error("database migrations failed", "error", err)
		os.Exit(1)
	}

	redis, err := redisstore.Open(rootContext, cfg.RedisURL)
	if err != nil {
		logger.Error("Redis startup failed", "error", err)
		os.Exit(1)
	}
	defer redis.Close()

	authenticator, err := access.NewJWTAuthenticatorWithClaims(cfg.JWTSecret, cfg.JWTIssuer, cfg.JWTAudience)
	if err != nil {
		logger.Error("authentication startup failed", "error", err)
		os.Exit(1)
	}
	publisher := events.NewRedisPublisher(redis)
	dispatcher := events.NewDispatcher(pool, publisher, logger, 2*time.Second)
	dispatcherDone := make(chan struct{})
	go func() {
		defer close(dispatcherDone)
		dispatcher.Run(rootContext)
	}()
	var loyalFitnessReader loyalfitness.Reader
	if cfg.LoyalFitnessBaseURL != "" {
		upstreamReader, err := loyalfitness.NewHTTPReader(cfg.LoyalFitnessBaseURL, cfg.LoyalFitnessToken, &http.Client{Timeout: cfg.LoyalFitnessTimeout})
		if err != nil {
			logger.Error("Loyal Fitness adapter configuration failed", "error", err)
			os.Exit(1)
		}
		loyalFitnessReader = loyalfitness.NewCachedReader(upstreamReader, cache.NewRedisStore(redis), cfg.ReadCacheTTL)
	}

	api := server.New(cfg, server.Dependencies{
		Logger:        logger,
		Authenticator: authenticator,
		LoyalFitness:  loyalFitnessReader,
		Attendance:    attendance.NewPostgresService(pool),
		Readiness: func(ctx context.Context) error {
			if err := pool.Ping(ctx); err != nil {
				return err
			}
			return redis.Ping(ctx).Err()
		},
	})
	httpServer := api.HTTPServer()
	serverErrors := make(chan error, 1)
	go func() {
		logger.Info("HTTP server listening", "addr", cfg.HTTPAddr, "env", cfg.AppEnv)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErrors <- err
		}
	}()

	select {
	case <-rootContext.Done():
		shutdownContext, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
		defer cancel()
		if err := httpServer.Shutdown(shutdownContext); err != nil {
			logger.Error("graceful shutdown failed", "error", err)
			os.Exit(1)
		}
		select {
		case <-dispatcherDone:
		case <-shutdownContext.Done():
			logger.Warn("event dispatcher did not stop before shutdown deadline")
		}
		logger.Info("HTTP server stopped")
	case err := <-serverErrors:
		logger.Error("HTTP server stopped unexpectedly", "error", err)
		os.Exit(1)
	}

}
