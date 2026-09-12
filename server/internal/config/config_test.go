package config

import (
	"encoding/base64"
	"testing"
)

func validKey() string {
	return base64.StdEncoding.EncodeToString(make([]byte, 32))
}

func TestConfigDatabaseDriver(t *testing.T) {
	key := validKey()

	t.Run("defaults to sqlite when DATABASE_URL is unset", func(t *testing.T) {
		t.Setenv("APP_ENCRYPTION_KEY", key)
		t.Setenv("SESSION_SECRET", key)
		t.Setenv("DATABASE_URL", "")
		t.Setenv("SQLITE_PATH", "")
		t.Setenv("DATABASE_DRIVER", "")

		cfg, err := Load()
		if err != nil {
			t.Fatalf("Load() error: %v", err)
		}
		if cfg.DatabaseDriver != DriverSQLite {
			t.Errorf("expected DriverSQLite, got %v", cfg.DatabaseDriver)
		}
		if cfg.DatabaseURL != "dockdeploy.db" {
			t.Errorf("expected dockdeploy.db, got %v", cfg.DatabaseURL)
		}
	})

	t.Run("uses SQLITE_PATH when DATABASE_URL is unset", func(t *testing.T) {
		t.Setenv("APP_ENCRYPTION_KEY", key)
		t.Setenv("SESSION_SECRET", key)
		t.Setenv("DATABASE_URL", "")
		t.Setenv("SQLITE_PATH", "/var/lib/dockdeploy/custom.db")
		t.Setenv("DATABASE_DRIVER", "")

		cfg, err := Load()
		if err != nil {
			t.Fatalf("Load() error: %v", err)
		}
		if cfg.DatabaseDriver != DriverSQLite {
			t.Errorf("expected DriverSQLite, got %v", cfg.DatabaseDriver)
		}
		if cfg.DatabaseURL != "/var/lib/dockdeploy/custom.db" {
			t.Errorf("expected /var/lib/dockdeploy/custom.db, got %v", cfg.DatabaseURL)
		}
	})

	t.Run("detects postgres from DATABASE_URL prefix", func(t *testing.T) {
		t.Setenv("APP_ENCRYPTION_KEY", key)
		t.Setenv("SESSION_SECRET", key)
		t.Setenv("DATABASE_URL", "postgres://user:pass@localhost:5432/dockdeploy")
		t.Setenv("DATABASE_DRIVER", "")

		cfg, err := Load()
		if err != nil {
			t.Fatalf("Load() error: %v", err)
		}
		if cfg.DatabaseDriver != DriverPostgres {
			t.Errorf("expected DriverPostgres, got %v", cfg.DatabaseDriver)
		}
		if cfg.DatabaseURL != "postgres://user:pass@localhost:5432/dockdeploy" {
			t.Errorf("unexpected database URL: %v", cfg.DatabaseURL)
		}
	})

	t.Run("detects sqlite from sqlite: prefix", func(t *testing.T) {
		t.Setenv("APP_ENCRYPTION_KEY", key)
		t.Setenv("SESSION_SECRET", key)
		t.Setenv("DATABASE_URL", "sqlite:///tmp/test.db")
		t.Setenv("DATABASE_DRIVER", "")

		cfg, err := Load()
		if err != nil {
			t.Fatalf("Load() error: %v", err)
		}
		if cfg.DatabaseDriver != DriverSQLite {
			t.Errorf("expected DriverSQLite, got %v", cfg.DatabaseDriver)
		}
		if cfg.DatabaseURL != "/tmp/test.db" {
			t.Errorf("expected /tmp/test.db, got %v", cfg.DatabaseURL)
		}
	})

	t.Run("defaults port to 8081 and app url to http://localhost:8081", func(t *testing.T) {
		t.Setenv("APP_ENCRYPTION_KEY", key)
		t.Setenv("SESSION_SECRET", key)
		t.Setenv("PORT", "")
		t.Setenv("APP_URL", "")

		cfg, err := Load()
		if err != nil {
			t.Fatalf("Load() error: %v", err)
		}
		if cfg.Port != 8081 {
			t.Errorf("expected Port 8081, got %d", cfg.Port)
		}
		if cfg.AppURL != "http://localhost:8081" {
			t.Errorf("expected AppURL http://localhost:8081, got %s", cfg.AppURL)
		}
	})
}
