// Package db owns the Postgres connection pool and schema migrations.
package db

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"github.com/sirtheprogrammer/docker-deployments/server/migrations"
)

// Connect opens a pool and waits for the database to accept queries.
//
// In the shipped compose stack the app and Postgres start together, so the
// first few pings routinely fail while Postgres finishes initialising. Retrying
// here is cheaper than making operators reason about container restart loops.
func Connect(ctx context.Context, url string, log *slog.Logger) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("db: parse DATABASE_URL: %w", err)
	}
	cfg.MaxConns = 16
	cfg.MinConns = 2
	cfg.MaxConnLifetime = time.Hour
	cfg.MaxConnIdleTime = 15 * time.Minute
	cfg.HealthCheckPeriod = time.Minute

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("db: create pool: %w", err)
	}

	const attempts = 30
	backoff := 250 * time.Millisecond
	for attempt := 1; ; attempt++ {
		err = pool.Ping(ctx)
		if err == nil {
			return pool, nil
		}
		if attempt == attempts || ctx.Err() != nil {
			pool.Close()
			return nil, fmt.Errorf("db: unreachable after %d attempts: %w", attempt, err)
		}
		log.Warn("database not ready, retrying", "attempt", attempt, "error", err)
		select {
		case <-ctx.Done():
			pool.Close()
			return nil, ctx.Err()
		case <-time.After(backoff):
		}
		if backoff < 2*time.Second {
			backoff *= 2
		}
	}
}

// Migrate applies any pending migrations.
//
// goose needs a database/sql handle, which pgx provides through its stdlib
// shim. The handle is opened and closed here so it does not outlive the call
// and compete with the pool for connections.
func Migrate(ctx context.Context, pool *pgxpool.Pool, log *slog.Logger) error {
	sqlDB := stdlib.OpenDBFromPool(pool)
	defer sqlDB.Close()

	goose.SetBaseFS(migrations.FS)
	goose.SetLogger(gooseLogger{log})
	if err := goose.SetDialect("postgres"); err != nil {
		return fmt.Errorf("db: set goose dialect: %w", err)
	}

	before, err := currentVersion(ctx, sqlDB)
	if err != nil {
		return err
	}
	if err := goose.UpContext(ctx, sqlDB, "."); err != nil {
		return fmt.Errorf("db: migrate: %w", err)
	}
	after, err := currentVersion(ctx, sqlDB)
	if err != nil {
		return err
	}

	if before == after {
		log.Info("schema up to date", "version", after)
	} else {
		log.Info("schema migrated", "from", before, "to", after)
	}
	return nil
}

func currentVersion(ctx context.Context, sqlDB *sql.DB) (int64, error) {
	version, err := goose.GetDBVersionContext(ctx, sqlDB)
	if err != nil {
		return 0, fmt.Errorf("db: read schema version: %w", err)
	}
	return version, nil
}

// gooseLogger routes goose's output through slog instead of the standard
// logger, so migration output matches the rest of the server's logs.
type gooseLogger struct{ log *slog.Logger }

func (g gooseLogger) Printf(format string, v ...any) {
	g.log.Debug(fmt.Sprintf(format, v...))
}

func (g gooseLogger) Fatalf(format string, v ...any) {
	g.log.Error(fmt.Sprintf(format, v...))
}
