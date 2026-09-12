package store

import (
	"context"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/sirtheprogrammer/docker-deployments/server/internal/auth"
)

type UserStatus string

const (
	UserActive    UserStatus = "active"
	UserSuspended UserStatus = "suspended"
)

type User struct {
	ID           string     `db:"id"           json:"id"`
	Email        string     `db:"email"        json:"email"`
	Name         string     `db:"name"         json:"name"`
	PasswordHash string     `db:"password_hash" json:"-"`
	Role         auth.Role  `db:"role"         json:"role"`
	Status       UserStatus `db:"status"       json:"status"`
	LastLoginAt  *time.Time `db:"last_login_at" json:"last_login_at"`
	CreatedAt    time.Time  `db:"created_at"   json:"created_at"`
	UpdatedAt    time.Time  `db:"updated_at"   json:"updated_at"`
}

func (u *User) IsActive() bool { return u.Status == UserActive }

// NormaliseEmail is applied on every read and write path so lookups match
// regardless of how the address was typed.
func NormaliseEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

const userColumns = `id, email, name, password_hash, role, status, last_login_at, created_at, updated_at`

// setupAdvisoryLock serialises first-admin creation across processes. The
// constant is arbitrary but must stay stable.
const setupAdvisoryLock = 8172341

// CountUsers reports how many accounts exist, which is how the API decides
// whether first-run setup is still open.
func (s *Store) CountUsers(ctx context.Context) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx, `SELECT count(*) FROM users`).Scan(&n)
	return n, wrap("store: count users", err)
}

// CreateFirstAdmin creates the initial administrator, and only ever the
// initial one.
//
// Two requests arriving together would otherwise both see an empty table and
// both create an admin, so the check and the insert share a transaction held
// behind an advisory lock.
func (s *Store) CreateFirstAdmin(ctx context.Context, email, name, passwordHash string) (*User, error) {
	var user *User
	err := s.tx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, setupAdvisoryLock); err != nil {
			return wrap("store: setup lock", err)
		}

		var existing int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM users`).Scan(&existing); err != nil {
			return wrap("store: count users", err)
		}
		if existing > 0 {
			return ErrConflict
		}

		rows, err := tx.Query(ctx, `
			INSERT INTO users (email, name, password_hash, role)
			VALUES ($1, $2, $3, 'admin')
			RETURNING `+userColumns,
			NormaliseEmail(email), name, passwordHash)
		if err != nil {
			return wrap("store: create first admin", err)
		}
		created, err := pgx.CollectExactlyOneRow(rows, pgx.RowToStructByName[User])
		if err != nil {
			return wrap("store: create first admin", err)
		}
		user = &created
		return nil
	})
	return user, err
}

func (s *Store) CreateUser(ctx context.Context, email, name, passwordHash string, role auth.Role) (*User, error) {
	rows, err := s.pool.Query(ctx, `
		INSERT INTO users (email, name, password_hash, role)
		VALUES ($1, $2, $3, $4)
		RETURNING `+userColumns,
		NormaliseEmail(email), name, passwordHash, role)
	if err != nil {
		return nil, wrap("store: create user", err)
	}
	user, err := pgx.CollectExactlyOneRow(rows, pgx.RowToStructByName[User])
	if err != nil {
		return nil, wrap("store: create user", err)
	}
	return &user, nil
}

func (s *Store) UserByEmail(ctx context.Context, email string) (*User, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+userColumns+` FROM users WHERE email = $1`, NormaliseEmail(email))
	if err != nil {
		return nil, wrap("store: user by email", err)
	}
	user, err := pgx.CollectExactlyOneRow(rows, pgx.RowToStructByName[User])
	if err != nil {
		return nil, wrap("store: user by email", err)
	}
	return &user, nil
}

func (s *Store) UserByID(ctx context.Context, id string) (*User, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+userColumns+` FROM users WHERE id = $1`, id)
	if err != nil {
		return nil, wrap("store: user by id", err)
	}
	user, err := pgx.CollectExactlyOneRow(rows, pgx.RowToStructByName[User])
	if err != nil {
		return nil, wrap("store: user by id", err)
	}
	return &user, nil
}

func (s *Store) ListUsers(ctx context.Context) ([]User, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+userColumns+` FROM users ORDER BY created_at`)
	if err != nil {
		return nil, wrap("store: list users", err)
	}
	users, err := pgx.CollectRows(rows, pgx.RowToStructByName[User])
	return users, wrap("store: list users", err)
}

// CountAdmins is used to refuse changes that would leave the instance with no
// administrator.
func (s *Store) CountAdmins(ctx context.Context) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx,
		`SELECT count(*) FROM users WHERE role = 'admin' AND status = 'active'`).Scan(&n)
	return n, wrap("store: count admins", err)
}

func (s *Store) UpdateUser(ctx context.Context, id, name string, role auth.Role, status UserStatus) (*User, error) {
	rows, err := s.pool.Query(ctx, `
		UPDATE users SET name = $2, role = $3, status = $4
		WHERE id = $1
		RETURNING `+userColumns, id, name, role, status)
	if err != nil {
		return nil, wrap("store: update user", err)
	}
	user, err := pgx.CollectExactlyOneRow(rows, pgx.RowToStructByName[User])
	if err != nil {
		return nil, wrap("store: update user", err)
	}
	return &user, nil
}

func (s *Store) UpdateUserName(ctx context.Context, id, name string) (*User, error) {
	rows, err := s.pool.Query(ctx,
		`UPDATE users SET name = $2 WHERE id = $1 RETURNING `+userColumns, id, name)
	if err != nil {
		return nil, wrap("store: update name", err)
	}
	user, err := pgx.CollectExactlyOneRow(rows, pgx.RowToStructByName[User])
	if err != nil {
		return nil, wrap("store: update name", err)
	}
	return &user, nil
}

// UpdateUserPassword rotates the hash and drops every session but the one
// given, so a password change ejects anyone else holding a stolen cookie.
func (s *Store) UpdateUserPassword(ctx context.Context, id, passwordHash, keepSessionID string) error {
	return s.tx(ctx, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `UPDATE users SET password_hash = $2 WHERE id = $1`, id, passwordHash)
		if err != nil {
			return wrap("store: update password", err)
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		_, err = tx.Exec(ctx,
			`DELETE FROM sessions WHERE user_id = $1 AND ($2 = '' OR id <> $2::uuid)`, id, keepSessionID)
		return wrap("store: prune sessions", err)
	})
}

func (s *Store) TouchUserLogin(ctx context.Context, id string) error {
	_, err := s.pool.Exec(ctx, `UPDATE users SET last_login_at = now() WHERE id = $1`, id)
	return wrap("store: touch login", err)
}

func (s *Store) DeleteUser(ctx context.Context, id string) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, id)
	if err != nil {
		return wrap("store: delete user", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
