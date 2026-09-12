package db

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"testing"
)

func TestSQLiteConnectAndMigrate(t *testing.T) {
	ctx := context.Background()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	sqlDB, err := ConnectSQLite(ctx, dbPath, log)
	if err != nil {
		t.Fatalf("ConnectSQLite failed: %v", err)
	}
	defer sqlDB.Close()

	if err := sqlDB.PingContext(ctx); err != nil {
		t.Fatalf("PingContext failed: %v", err)
	}

	if err := MigrateSQLite(ctx, sqlDB, log); err != nil {
		t.Fatalf("MigrateSQLite failed: %v", err)
	}

	// Verify tables exist
	var count int
	err = sqlDB.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_master WHERE type='table' AND name='users'").Scan(&count)
	if err != nil {
		t.Fatalf("querying tables failed: %v", err)
	}
	if count != 1 {
		t.Errorf("expected users table to exist, got count %d", count)
	}
}
