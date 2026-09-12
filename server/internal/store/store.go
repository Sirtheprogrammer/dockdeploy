// Package store is the only place that speaks SQL. Handlers call methods here;
// they never build queries themselves.
package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	// ErrNotFound means no row matched. Handlers map this to a 404 rather than
	// leaking pgx.ErrNoRows through the error envelope.
	ErrNotFound = errors.New("store: not found")
	// ErrConflict means a unique constraint rejected the write.
	ErrConflict = errors.New("store: conflicting record")
)

type Store struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

// Pool exposes the underlying pool for the few callers that need LISTEN/NOTIFY
// or a long-lived connection of their own.
func (s *Store) Pool() *pgxpool.Pool { return s.pool }

// tx runs fn inside a transaction, rolling back on error or panic.
func (s *Store) tx(ctx context.Context, fn func(pgx.Tx) error) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("store: begin: %w", err)
	}
	defer func() {
		// Rollback after a successful Commit is a no-op, so this is safe to
		// run unconditionally and covers the panic path too.
		_ = tx.Rollback(ctx)
	}()

	if err := fn(tx); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("store: commit: %w", err)
	}
	return nil
}

// nullable turns an empty string into a SQL NULL.
//
// Optional uuid columns like created_by would otherwise reject "" with a type
// error. Not every write has a user behind it: a webhook-triggered deploy has
// none, and neither does a fixture in a test.
func nullable(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

// wrap translates driver errors into the package's sentinels.
func wrap(op string, err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("%s: %w", op, ErrNotFound)
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23505": // unique_violation
			return fmt.Errorf("%s: %w (%s)", op, ErrConflict, pgErr.ConstraintName)
		case "23503": // foreign_key_violation
			return fmt.Errorf("%s: %w (%s)", op, ErrNotFound, pgErr.ConstraintName)
		}
	}
	return fmt.Errorf("%s: %w", op, err)
}
