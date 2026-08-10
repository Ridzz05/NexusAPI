package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config contains process configuration. Secrets are intentionally represented
// here only long enough to construct their consumers and are never logged.
type Config struct {
	AppEnv              string
	HTTPAddr            string
	DatabaseURL         string
	RedisURL            string
	JWTSecret           string
	JWTIssuer           string
	JWTAudience         string
	DBMaxConns          int32
	DBMinConns          int32
	CORSAllowedOrigins  []string
	RateLimitRPS        int
	RateLimitBurst      int
	HTTPReadTimeout     time.Duration
	HTTPWriteTimeout    time.Duration
	HTTPIdleTimeout     time.Duration
	HTTPRequestTimeout  time.Duration
	ShutdownTimeout     time.Duration
	LoyalFitnessBaseURL string
	LoyalFitnessToken   string
	LoyalFitnessTimeout time.Duration
	ReadCacheTTL        time.Duration
}

func Load() (Config, error) {
	maxConns, err := int32Env("DB_MAX_CONNS", 10)
	if err != nil {
		return Config{}, err
	}
	minConns, err := int32Env("DB_MIN_CONNS", 2)
	if err != nil {
		return Config{}, err
	}
	rateLimitRPS, err := intEnv("RATE_LIMIT_RPS", 20)
	if err != nil {
		return Config{}, err
	}
	rateLimitBurst, err := intEnv("RATE_LIMIT_BURST", 40)
	if err != nil {
		return Config{}, err
	}
	readTimeout, err := durationEnv("HTTP_READ_TIMEOUT", 10*time.Second)
	if err != nil {
		return Config{}, err
	}
	writeTimeout, err := durationEnv("HTTP_WRITE_TIMEOUT", 15*time.Second)
	if err != nil {
		return Config{}, err
	}
	idleTimeout, err := durationEnv("HTTP_IDLE_TIMEOUT", 60*time.Second)
	if err != nil {
		return Config{}, err
	}
	requestTimeout, err := durationEnv("HTTP_REQUEST_TIMEOUT", 10*time.Second)
	if err != nil {
		return Config{}, err
	}
	shutdownTimeout, err := durationEnv("SHUTDOWN_TIMEOUT", 10*time.Second)
	if err != nil {
		return Config{}, err
	}
	loyalFitnessTimeout, err := durationEnv("LOYAL_FITNESS_TIMEOUT", 5*time.Second)
	if err != nil {
		return Config{}, err
	}
	readCacheTTL, err := durationEnv("READ_CACHE_TTL", 30*time.Second)
	if err != nil {
		return Config{}, err
	}

	origins := splitCSV(env("CORS_ALLOWED_ORIGINS", ""))
	cfg := Config{
		AppEnv:              env("APP_ENV", "development"),
		HTTPAddr:            env("HTTP_ADDR", ":8080"),
		DatabaseURL:         env("DATABASE_URL", ""),
		RedisURL:            env("REDIS_URL", ""),
		JWTSecret:           env("JWT_SECRET", ""),
		JWTIssuer:           env("JWT_ISSUER", ""),
		JWTAudience:         env("JWT_AUDIENCE", ""),
		DBMaxConns:          maxConns,
		DBMinConns:          minConns,
		CORSAllowedOrigins:  origins,
		RateLimitRPS:        rateLimitRPS,
		RateLimitBurst:      rateLimitBurst,
		HTTPReadTimeout:     readTimeout,
		HTTPWriteTimeout:    writeTimeout,
		HTTPIdleTimeout:     idleTimeout,
		HTTPRequestTimeout:  requestTimeout,
		ShutdownTimeout:     shutdownTimeout,
		LoyalFitnessBaseURL: env("LOYAL_FITNESS_BASE_URL", ""),
		LoyalFitnessToken:   env("LOYAL_FITNESS_TOKEN", ""),
		LoyalFitnessTimeout: loyalFitnessTimeout,
		ReadCacheTTL:        readCacheTTL,
	}
	return cfg, cfg.Validate()
}

func (c Config) Validate() error {
	if strings.TrimSpace(c.HTTPAddr) == "" {
		return errors.New("HTTP_ADDR must not be empty")
	}
	if c.AppEnv == "" {
		return errors.New("APP_ENV must not be empty")
	}
	if c.DBMinConns < 0 || c.DBMaxConns < 1 || c.DBMinConns > c.DBMaxConns {
		return errors.New("DB_MIN_CONNS and DB_MAX_CONNS are invalid")
	}
	if c.RateLimitRPS < 1 || c.RateLimitBurst < 1 {
		return errors.New("RATE_LIMIT_RPS and RATE_LIMIT_BURST must be positive")
	}
	if c.HTTPReadTimeout <= 0 || c.HTTPWriteTimeout <= 0 || c.HTTPIdleTimeout <= 0 || c.HTTPRequestTimeout <= 0 || c.ShutdownTimeout <= 0 {
		return errors.New("HTTP and shutdown timeouts must be positive")
	}
	if c.LoyalFitnessTimeout <= 0 || c.ReadCacheTTL <= 0 {
		return errors.New("LOYAL_FITNESS_TIMEOUT and READ_CACHE_TTL must be positive")
	}
	if c.JWTSecret == "" {
		return errors.New("JWT_SECRET must be configured")
	}
	if len([]byte(c.JWTSecret)) < 32 {
		return errors.New("JWT_SECRET must contain at least 32 bytes")
	}
	if strings.EqualFold(c.AppEnv, "production") && insecureJWTSecret(c.JWTSecret) {
		return errors.New("JWT_SECRET must be replaced with a unique random value in production")
	}
	if c.DatabaseURL == "" {
		return errors.New("DATABASE_URL must be configured")
	}
	if c.RedisURL == "" {
		return errors.New("REDIS_URL must be configured")
	}
	if c.LoyalFitnessBaseURL != "" {
		parsed, err := url.Parse(c.LoyalFitnessBaseURL)
		if err != nil || parsed.Scheme == "" || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
			return errors.New("LOYAL_FITNESS_BASE_URL must be an http or https URL")
		}
	}
	if contains(c.CORSAllowedOrigins, "*") {
		return errors.New("CORS_ALLOWED_ORIGINS must contain explicit origins; '*' is not supported")
	}
	if strings.EqualFold(c.AppEnv, "production") && len(c.CORSAllowedOrigins) == 0 {
		return errors.New("CORS_ALLOWED_ORIGINS must be explicit in production")
	}
	return nil
}

func insecureJWTSecret(secret string) bool {
	switch strings.ToLower(strings.TrimSpace(secret)) {
	case "replace-with-at-least-32-random-bytes", "change-me", "changeme", "secret":
		return true
	default:
		return false
	}
}

func env(key, fallback string) string {
	value, ok := os.LookupEnv(key)
	if !ok {
		return fallback
	}
	return strings.TrimSpace(value)
}

func intEnv(key string, fallback int) (int, error) {
	value := env(key, strconv.Itoa(fallback))
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer: %w", key, err)
	}
	return parsed, nil
}

func int32Env(key string, fallback int32) (int32, error) {
	value := env(key, strconv.FormatInt(int64(fallback), 10))
	parsed, err := strconv.ParseInt(value, 10, 32)
	if err != nil {
		return 0, fmt.Errorf("%s must be a 32-bit integer: %w", key, err)
	}
	return int32(parsed), nil
}

func durationEnv(key string, fallback time.Duration) (time.Duration, error) {
	value := env(key, fallback.String())
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("%s must be a duration: %w", key, err)
	}
	return parsed, nil
}

func splitCSV(value string) []string {
	if value == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
