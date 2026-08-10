package config

import (
	"testing"
	"time"
)

func validConfig() Config {
	return Config{
		AppEnv:              "test",
		HTTPAddr:            ":8080",
		DatabaseURL:         "postgres://localhost/nexus",
		RedisURL:            "redis://localhost:6379",
		JWTSecret:           "01234567890123456789012345678901",
		DBMaxConns:          10,
		DBMinConns:          2,
		CORSAllowedOrigins:  []string{"http://localhost:3000"},
		RateLimitRPS:        20,
		RateLimitBurst:      40,
		HTTPReadTimeout:     10 * time.Second,
		HTTPWriteTimeout:    10 * time.Second,
		HTTPIdleTimeout:     10 * time.Second,
		HTTPRequestTimeout:  10 * time.Second,
		ShutdownTimeout:     10 * time.Second,
		LoyalFitnessTimeout: 5 * time.Second,
		ReadCacheTTL:        30 * time.Second,
	}
}

func TestConfigValidateRequiresProductionDependencies(t *testing.T) {
	cfg := validConfig()
	cfg.JWTSecret = "short"
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected weak JWT secret to be rejected")
	}

	cfg = validConfig()
	cfg.DatabaseURL = ""
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected missing database URL to be rejected")
	}

	cfg = validConfig()
	cfg.AppEnv = "production"
	cfg.CORSAllowedOrigins = nil
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected production to require explicit CORS origins")
	}

	cfg = validConfig()
	cfg.LoyalFitnessBaseURL = "ftp://source.example"
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected unsupported adapter URL scheme to be rejected")
	}
}

func TestSplitCSV(t *testing.T) {
	got := splitCSV(" http://localhost:3000, ,http://localhost:5173 ")
	if len(got) != 2 || got[0] != "http://localhost:3000" || got[1] != "http://localhost:5173" {
		t.Fatalf("unexpected origins: %#v", got)
	}
}
