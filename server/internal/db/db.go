// Package db owns the database connection pool (Postgres or SQLite) and schema migrations.
package db

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite"

	"github.com/sirtheprogrammer/docker-deployments/server/migrations"
)

// Connect opens a pool and waits for PostgreSQL to accept queries.
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

// ConnectSQLite opens a SQLite database file, creates any needed parent directories,
// and configures essential PRAGMA settings for high concurrency and safety (WAL mode,
// busy timeout, foreign keys, synchronous=NORMAL).
func ConnectSQLite(ctx context.Context, pathOrDSN string, log *slog.Logger) (*sql.DB, error) {
	cleanPath := pathOrDSN
	if idx := strings.Index(cleanPath, "?"); idx >= 0 {
		cleanPath = cleanPath[:idx]
	}
	cleanPath = strings.TrimPrefix(cleanPath, "file:")

	if dir := filepath.Dir(cleanPath); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, fmt.Errorf("db: create sqlite directory: %w", err)
		}
	}

	dsn := pathOrDSN
	sep := "?"
	if strings.Contains(dsn, "?") {
		sep = "&"
	}
	if !strings.Contains(dsn, "_pragma") {
		dsn += fmt.Sprintf("%s_pragma=foreign_keys(1)&_pragma=busy_timeout(10000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)", sep)
	}

	sqlDB, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("db: open sqlite: %w", err)
	}

	maxConns := runtime.NumCPU()
	if maxConns < 4 {
		maxConns = 4
	}
	sqlDB.SetMaxOpenConns(maxConns)
	sqlDB.SetMaxIdleConns(maxConns / 2)
	sqlDB.SetConnMaxLifetime(time.Hour)

	if err := sqlDB.PingContext(ctx); err != nil {
		sqlDB.Close()
		return nil, fmt.Errorf("db: ping sqlite: %w", err)
	}

	return sqlDB, nil
}

// Migrate applies any pending Postgres migrations.
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

// MigrateSQLite applies migrations for SQLite.
func MigrateSQLite(ctx context.Context, sqlDB *sql.DB, log *slog.Logger) error {
	goose.SetBaseFS(migrations.SQLiteFS)
	goose.SetLogger(gooseLogger{log})
	if err := goose.SetDialect("sqlite3"); err != nil {
		return fmt.Errorf("db: set goose dialect: %w", err)
	}

	before, err := currentVersion(ctx, sqlDB)
	if err != nil {
		return err
	}
	if err := goose.UpContext(ctx, sqlDB, "sqlite"); err != nil {
		return fmt.Errorf("db: migrate sqlite: %w", err)
	}
	after, err := currentVersion(ctx, sqlDB)
	if err != nil {
		return err
	}

	if before == after {
		log.Info("sqlite schema up to date", "version", after)
	} else {
		log.Info("sqlite schema migrated", "from", before, "to", after)
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
