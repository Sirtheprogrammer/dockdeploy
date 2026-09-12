// Package config loads and validates process configuration from the environment.
//
// Every value the server needs is resolved once at startup. Anything missing or
// malformed is reported as a single combined error so an operator fixes their
// whole .env in one pass instead of restarting once per typo.
package config

import (
	"encoding/base64"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"
)

// EncryptionKeySize is the required decoded length of APP_ENCRYPTION_KEY.
// AES-256-GCM takes a 32 byte key; nothing shorter is accepted.
const EncryptionKeySize = 32

type Config struct {
	Env    string // "development" or "production"
	Port   int
	AppURL string

	DatabaseURL string

	// EncryptionKey seals credentials at rest. Losing it makes every stored
	// SSH key, registry password and deployment secret unrecoverable.
	EncryptionKey []byte
	SessionSecret []byte

	LogLevel      slog.Level
	ShutdownGrace time.Duration

	// Deployments publish on a loopback port from this range, so two
	// applications on one server cannot collide and nothing is exposed
	// publicly until nginx is put in front of it.
	DeployPortMin int
	DeployPortMax int
	// DeployRemoteRoot is where checkouts live, relative to the SSH user home.
	DeployRemoteRoot string
	// DeployConcurrency bounds how many builds run at once.
	DeployConcurrency int
}

func (c *Config) IsProduction() bool { return c.Env == "production" }

// Load reads configuration from the environment.
func Load() (*Config, error) {
	var problems []string
	note := func(format string, args ...any) {
		problems = append(problems, fmt.Sprintf(format, args...))
	}

	cfg := &Config{
		Env:           envOr("APP_ENV", "development"),
		AppURL:        strings.TrimRight(envOr("APP_URL", "http://localhost:8080"), "/"),
		DatabaseURL:   os.Getenv("DATABASE_URL"),
		ShutdownGrace: 30 * time.Second,

		DeployRemoteRoot:  envOr("DEPLOY_REMOTE_ROOT", ".dockdeploy/apps"),
		DeployConcurrency: 2,
	}

	var err error

	cfg.DeployPortMin, err = envInt("DEPLOY_PORT_MIN", 20000)
	if err != nil {
		note("%v", err)
	}
	cfg.DeployPortMax, err = envInt("DEPLOY_PORT_MAX", 29999)
	if err != nil {
		note("%v", err)
	}
	if cfg.DeployPortMin >= cfg.DeployPortMax {
		note("DEPLOY_PORT_MIN (%d) must be below DEPLOY_PORT_MAX (%d)", cfg.DeployPortMin, cfg.DeployPortMax)
	}

	cfg.DeployConcurrency, err = envInt("DEPLOY_CONCURRENCY", 2)
	if err != nil {
		note("%v", err)
	} else if cfg.DeployConcurrency < 1 || cfg.DeployConcurrency > 16 {
		note("DEPLOY_CONCURRENCY must be between 1 and 16")
	}

	if cfg.Env != "development" && cfg.Env != "production" {
		note("APP_ENV must be 'development' or 'production', got %q", cfg.Env)
	}

	port, err := strconv.Atoi(envOr("PORT", "8080"))
	if err != nil || port < 1 || port > 65535 {
		note("PORT must be a number between 1 and 65535, got %q", os.Getenv("PORT"))
	}
	cfg.Port = port

	if cfg.DatabaseURL == "" {
		note("DATABASE_URL is required (e.g. postgres://user:pass@host:5432/dockdeploy?sslmode=disable)")
	}

	cfg.EncryptionKey, err = decodeKey("APP_ENCRYPTION_KEY", EncryptionKeySize)
	if err != nil {
		note("%v", err)
	}

	// The session secret signs cookies; length is not fixed by a cipher, but
	// anything under 32 bytes is too weak to bother supporting.
	cfg.SessionSecret, err = decodeKey("SESSION_SECRET", 32)
	if err != nil {
		note("%v", err)
	}

	switch level := strings.ToLower(envOr("LOG_LEVEL", "info")); level {
	case "debug":
		cfg.LogLevel = slog.LevelDebug
	case "info":
		cfg.LogLevel = slog.LevelInfo
	case "warn":
		cfg.LogLevel = slog.LevelWarn
	case "error":
		cfg.LogLevel = slog.LevelError
	default:
		note("LOG_LEVEL must be one of debug|info|warn|error, got %q", level)
	}

	if len(problems) > 0 {
		return nil, fmt.Errorf("invalid configuration:\n  - %s", strings.Join(problems, "\n  - "))
	}
	return cfg, nil
}

// decodeKey reads a base64 encoded secret and checks its decoded length.
func decodeKey(name string, want int) ([]byte, error) {
	raw := os.Getenv(name)
	if raw == "" {
		return nil, fmt.Errorf("%s is required; generate one with: openssl rand -base64 %d", name, want)
	}
	key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(raw))
	if err != nil {
		return nil, fmt.Errorf("%s must be valid standard base64: %w", name, err)
	}
	if len(key) != want {
		return nil, fmt.Errorf("%s must decode to %d bytes, got %d; generate one with: openssl rand -base64 %d", name, want, len(key), want)
	}
	return key, nil
}

// envInt reads a numeric setting, reporting a usable message rather than
// silently falling back on a typo.
func envInt(name string, fallback int) (int, error) {
	raw := os.Getenv(name)
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return fallback, fmt.Errorf("%s must be a whole number, got %q", name, raw)
	}
	return value, nil
}

func envOr(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}
