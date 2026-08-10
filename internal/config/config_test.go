package config

import (
	"testing"
	"time"
)

func validConfig() Config {
	return Config{
		AppEnv:                "test",
		HTTPAddr:              ":8080",
		DatabaseURL:           "postgres://localhost/nexus",
		RedisURL:              "redis://localhost:6379",
		JWTSecret:             "01234567890123456789012345678901",
		DBMaxConns:            10,
		DBMinConns:            2,
		CORSAllowedOrigins:    []string{"http://localhost:3000"},
		RateLimitRPS:          20,
		RateLimitBurst:        40,
		HTTPReadTimeout:       10 * time.Second,
		HTTPReadHeaderTimeout: 5 * time.Second,
		HTTPWriteTimeout:      10 * time.Second,
		HTTPIdleTimeout:       10 * time.Second,
		HTTPRequestTimeout:    10 * time.Second,
		HTTPMaxHeaderBytes:    1 << 20,
		ShutdownTimeout:       10 * time.Second,
		LoyalFitnessTimeout:   5 * time.Second,
		ReadCacheTTL:          30 * time.Second,
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

	cfg = validConfig()
	cfg.AppEnv = "production"
	cfg.JWTSecret = "replace-with-at-least-32-random-bytes"
	cfg.CORSAllowedOrigins = []string{"https://app.example.com"}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected the documented placeholder JWT secret to be rejected in production")
	}

	cfg = validConfig()
	cfg.CORSAllowedOrigins = []string{"*"}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected wildcard CORS origin to be rejected")
	}
}

func TestSplitCSV(t *testing.T) {
	got := splitCSV(" http://localhost:3000, ,http://localhost:5173 ")
	if len(got) != 2 || got[0] != "http://localhost:3000" || got[1] != "http://localhost:5173" {
		t.Fatalf("unexpected origins: %#v", got)
	}
}

func TestParseCIDRs(t *testing.T) {
	prefixes, err := parseCIDRs(" 127.0.0.1/32, 2001:db8::/32 ")
	if err != nil || len(prefixes) != 2 {
		t.Fatalf("unexpected prefixes: %#v, %v", prefixes, err)
	}
	if _, err := parseCIDRs("not-a-cidr"); err == nil {
		t.Fatal("expected invalid trusted proxy CIDR to fail")
	}
}

func TestLoadParsesHardeningEnvironment(t *testing.T) {
	t.Setenv("APP_ENV", "test")
	t.Setenv("DATABASE_URL", "postgres://localhost/nexus")
	t.Setenv("REDIS_URL", "redis://localhost:6379")
	t.Setenv("JWT_SECRET", "01234567890123456789012345678901")
	t.Setenv("CORS_ALLOWED_ORIGINS", "http://localhost:3000")
	t.Setenv("TRUSTED_PROXY_CIDRS", "127.0.0.1/32,::1/128")
	t.Setenv("HTTP_READ_HEADER_TIMEOUT", "3s")
	t.Setenv("HTTP_MAX_HEADER_BYTES", "8192")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.TrustedProxyCIDRs) != 2 || cfg.HTTPReadHeaderTimeout != 3*time.Second || cfg.HTTPMaxHeaderBytes != 8192 {
		t.Fatalf("hardening config was not parsed: %#v", cfg)
	}
}
