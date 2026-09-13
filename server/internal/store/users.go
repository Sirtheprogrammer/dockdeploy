package store

import (
	"context"
	"encoding/json"
	"errors"
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
	ID                   string     `db:"id"                      json:"id"`
	Email                string     `db:"email"                   json:"email"`
	Name                 string     `db:"name"                    json:"name"`
	PasswordHash         string     `db:"password_hash"           json:"-"`
	Role                 auth.Role  `db:"role"                    json:"role"`
	Status               UserStatus `db:"status"                  json:"status"`
	LastLoginAt          *time.Time `db:"last_login_at"           json:"last_login_at"`
	TOTPSecretID         *string    `db:"totp_secret_id"          json:"-"`
	TOTPRecoverySecretID *string    `db:"totp_recovery_secret_id" json:"-"`
	TOTPEnabled          bool       `db:"totp_enabled"            json:"totp_enabled"`
	CreatedAt            time.Time  `db:"created_at"              json:"created_at"`
	UpdatedAt            time.Time  `db:"updated_at"              json:"updated_at"`
}

func (u *User) IsActive() bool { return u.Status == UserActive }

// NormaliseEmail is applied on every read and write path so lookups match
// regardless of how the address was typed.
func NormaliseEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

const userColumns = `id, email, name, password_hash, role, status, last_login_at, totp_secret_id, totp_recovery_secret_id, totp_enabled, created_at, updated_at`

// setupAdvisoryLock serialises first-admin creation across processes. The
// constant is arbitrary but must stay stable.
const setupAdvisoryLock = 8172341

// CountUsers reports how many accounts exist, which is how the API decides
// whether first-run setup is still open.
func (s *Store) CountUsers(ctx context.Context) (int, error) {
	if s.sqlite != nil {
		return s.sqlite.CountUsers(ctx)
	}
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
	if s.sqlite != nil {
		return s.sqlite.CreateFirstAdmin(ctx, email, name, passwordHash)
	}
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
	if s.sqlite != nil {
		return s.sqlite.CreateUser(ctx, email, name, passwordHash, role)
	}
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
	if s.sqlite != nil {
		return s.sqlite.UserByEmail(ctx, email)
	}
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
	if s.sqlite != nil {
		return s.sqlite.UserByID(ctx, id)
	}
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
	if s.sqlite != nil {
		return s.sqlite.ListUsers(ctx)
	}
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
	if s.sqlite != nil {
		return s.sqlite.CountAdmins(ctx)
	}
	var n int
	err := s.pool.QueryRow(ctx,
		`SELECT count(*) FROM users WHERE role = 'admin' AND status = 'active'`).Scan(&n)
	return n, wrap("store: count admins", err)
}

func (s *Store) UpdateUser(ctx context.Context, id, name string, role auth.Role, status UserStatus) (*User, error) {
	if s.sqlite != nil {
		return s.sqlite.UpdateUser(ctx, id, name, role, status)
	}
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
	if s.sqlite != nil {
		return s.sqlite.UpdateUserName(ctx, id, name)
	}
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
	if s.sqlite != nil {
		return s.sqlite.UpdateUserPassword(ctx, id, passwordHash, keepSessionID)
	}
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
	if s.sqlite != nil {
		return s.sqlite.TouchUserLogin(ctx, id)
	}
	_, err := s.pool.Exec(ctx, `UPDATE users SET last_login_at = now() WHERE id = $1`, id)
	return wrap("store: touch login", err)
}

func (s *Store) DeleteUser(ctx context.Context, id string) error {
	if s.sqlite != nil {
		return s.sqlite.DeleteUser(ctx, id)
	}
	tag, err := s.pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, id)
	if err != nil {
		return wrap("store: delete user", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) SetUserTOTPSecret(ctx context.Context, sealer Sealer, userID, secretBase32 string) error {
	if s.sqlite != nil {
		return s.sqlite.SetUserTOTPSecret(ctx, sealer, userID, secretBase32)
	}
	user, err := s.UserByID(ctx, userID)
	if err != nil {
		return err
	}
	if user.TOTPSecretID != nil && *user.TOTPSecretID != "" {
		return s.ReplaceSecret(ctx, sealer, *user.TOTPSecretID, secretBase32)
	}
	return s.tx(ctx, func(tx pgx.Tx) error {
		id, err := insertSecret(ctx, tx, sealer, KindTOTPSecret, secretBase32)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `UPDATE users SET totp_secret_id = $1 WHERE id = $2`, id, userID)
		return wrap("store: update user totp secret id", err)
	})
}

func (s *Store) EnableUserTOTP(ctx context.Context, sealer Sealer, userID string, recoveryCodes []string) error {
	if s.sqlite != nil {
		return s.sqlite.EnableUserTOTP(ctx, sealer, userID, recoveryCodes)
	}
	payload, err := json.Marshal(recoveryCodes)
	if err != nil {
		return err
	}
	user, err := s.UserByID(ctx, userID)
	if err != nil {
		return err
	}
	if user.TOTPRecoverySecretID != nil && *user.TOTPRecoverySecretID != "" {
		if err := s.ReplaceSecret(ctx, sealer, *user.TOTPRecoverySecretID, string(payload)); err != nil {
			return err
		}
		_, err = s.pool.Exec(ctx, `UPDATE users SET totp_enabled = true WHERE id = $1`, userID)
		return wrap("store: enable user totp", err)
	}
	return s.tx(ctx, func(tx pgx.Tx) error {
		id, err := insertSecret(ctx, tx, sealer, KindTOTPRecovery, string(payload))
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `UPDATE users SET totp_recovery_secret_id = $1, totp_enabled = true WHERE id = $2`, id, userID)
		return wrap("store: enable user totp", err)
	})
}

func (s *Store) DisableUserTOTP(ctx context.Context, userID string) error {
	if s.sqlite != nil {
		return s.sqlite.DisableUserTOTP(ctx, userID)
	}
	_, err := s.pool.Exec(ctx, `UPDATE users SET totp_enabled = false, totp_secret_id = NULL, totp_recovery_secret_id = NULL WHERE id = $1`, userID)
	return wrap("store: disable user totp", err)
}

func (s *Store) GetUserTOTPSecret(ctx context.Context, sealer Sealer, user *User) (string, error) {
	if s.sqlite != nil {
		return s.sqlite.GetUserTOTPSecret(ctx, sealer, user)
	}
	if user.TOTPSecretID == nil {
		return "", errors.New("user has no totp secret")
	}
	return s.openSecret(ctx, sealer, user.TOTPSecretID)
}

func (s *Store) GetUserRecoveryCodes(ctx context.Context, sealer Sealer, user *User) ([]string, error) {
	if s.sqlite != nil {
		return s.sqlite.GetUserRecoveryCodes(ctx, sealer, user)
	}
	if user.TOTPRecoverySecretID == nil {
		return nil, nil
	}
	raw, err := s.openSecret(ctx, sealer, user.TOTPRecoverySecretID)
	if err != nil {
		return nil, err
	}
	if raw == "" {
		return nil, nil
	}
	var codes []string
	if err := json.Unmarshal([]byte(raw), &codes); err != nil {
		return nil, err
	}
	return codes, nil
}

func (s *Store) ConsumeRecoveryCode(ctx context.Context, sealer Sealer, user *User, code string) (bool, error) {
	if s.sqlite != nil {
		return s.sqlite.ConsumeRecoveryCode(ctx, sealer, user, code)
	}
	codes, err := s.GetUserRecoveryCodes(ctx, sealer, user)
	if err != nil || len(codes) == 0 {
		return false, err
	}
	target := auth.NormalizeRecoveryCode(code)
	idx := -1
	for i, c := range codes {
		if auth.NormalizeRecoveryCode(c) == target {
			idx = i
			break
		}
	}
	if idx < 0 {
		return false, nil
	}
	codes = append(codes[:idx], codes[idx+1:]...)
	payload, err := json.Marshal(codes)
	if err != nil {
		return false, err
	}
	if err := s.ReplaceSecret(ctx, sealer, *user.TOTPRecoverySecretID, string(payload)); err != nil {
		return false, err
	}
	return true, nil
}

