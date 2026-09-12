package store

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
)

type Session struct {
	ID         string    `db:"id"`
	UserID     string    `db:"user_id"`
	ExpiresAt  time.Time `db:"expires_at"`
	LastUsedAt time.Time `db:"last_used_at"`
	IP         *string   `db:"ip"`
	UserAgent  *string   `db:"user_agent"`
	CreatedAt  time.Time `db:"created_at"`
}

func (s *Store) CreateSession(ctx context.Context, userID string, tokenHash []byte, expiresAt time.Time, ip, userAgent string) (*Session, error) {
	if s.sqlite != nil {
		return s.sqlite.CreateSession(ctx, userID, tokenHash, expiresAt, ip, userAgent)
	}
	rows, err := s.pool.Query(ctx, `
		INSERT INTO sessions (user_id, token_hash, expires_at, ip, user_agent)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, user_id, expires_at, last_used_at, ip, user_agent, created_at`,
		userID, tokenHash, expiresAt, ip, truncate(userAgent, 512))
	if err != nil {
		return nil, wrap("store: create session", err)
	}
	session, err := pgx.CollectExactlyOneRow(rows, pgx.RowToStructByName[Session])
	if err != nil {
		return nil, wrap("store: create session", err)
	}
	return &session, nil
}

// SessionUser is the joined result the auth middleware needs on every request.
type SessionUser struct {
	Session Session
	User    User
}

// SessionByTokenHash resolves a cookie to its owner.
//
// Expiry is filtered in SQL so an expired row can never authenticate even if a
// caller forgets to check, and the suspended-user case is reported distinctly
// from "no such session" so the UI can explain why access stopped.
func (s *Store) SessionByTokenHash(ctx context.Context, tokenHash []byte) (*SessionUser, error) {
	if s.sqlite != nil {
		return s.sqlite.SessionByTokenHash(ctx, tokenHash)
	}
	row := s.pool.QueryRow(ctx, `
		SELECT s.id, s.user_id, s.expires_at, s.last_used_at, s.ip, s.user_agent, s.created_at,
		       u.id, u.email, u.name, u.password_hash, u.role, u.status,
		       u.last_login_at, u.created_at, u.updated_at
		FROM sessions s
		JOIN users u ON u.id = s.user_id
		WHERE s.token_hash = $1 AND s.expires_at > now()`, tokenHash)

	var out SessionUser
	err := row.Scan(
		&out.Session.ID, &out.Session.UserID, &out.Session.ExpiresAt, &out.Session.LastUsedAt,
		&out.Session.IP, &out.Session.UserAgent, &out.Session.CreatedAt,
		&out.User.ID, &out.User.Email, &out.User.Name, &out.User.PasswordHash, &out.User.Role,
		&out.User.Status, &out.User.LastLoginAt, &out.User.CreatedAt, &out.User.UpdatedAt,
	)
	if err != nil {
		return nil, wrap("store: session by token", err)
	}
	return &out, nil
}

// TouchSession records activity and slides the expiry forward, so an active
// user is not signed out mid-deploy.
func (s *Store) TouchSession(ctx context.Context, id string, expiresAt time.Time) error {
	if s.sqlite != nil {
		return s.sqlite.TouchSession(ctx, id, expiresAt)
	}
	_, err := s.pool.Exec(ctx,
		`UPDATE sessions SET last_used_at = now(), expires_at = $2 WHERE id = $1`, id, expiresAt)
	return wrap("store: touch session", err)
}

func (s *Store) ListSessions(ctx context.Context, userID string) ([]Session, error) {
	if s.sqlite != nil {
		return s.sqlite.ListSessions(ctx, userID)
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id, user_id, expires_at, last_used_at, ip, user_agent, created_at
		FROM sessions WHERE user_id = $1 AND expires_at > now()
		ORDER BY last_used_at DESC`, userID)
	if err != nil {
		return nil, wrap("store: list sessions", err)
	}
	sessions, err := pgx.CollectRows(rows, pgx.RowToStructByName[Session])
	return sessions, wrap("store: list sessions", err)
}

func (s *Store) DeleteSession(ctx context.Context, id string) error {
	if s.sqlite != nil {
		return s.sqlite.DeleteSession(ctx, id)
	}
	_, err := s.pool.Exec(ctx, `DELETE FROM sessions WHERE id = $1`, id)
	return wrap("store: delete session", err)
}

func (s *Store) DeleteSessionsForUser(ctx context.Context, userID string) error {
	if s.sqlite != nil {
		return s.sqlite.DeleteSessionsForUser(ctx, userID)
	}
	_, err := s.pool.Exec(ctx, `DELETE FROM sessions WHERE user_id = $1`, userID)
	return wrap("store: delete user sessions", err)
}

// PurgeExpiredSessions is called periodically; expired rows are already
// unusable, this just stops the table growing without bound.
func (s *Store) PurgeExpiredSessions(ctx context.Context) (int64, error) {
	if s.sqlite != nil {
		return s.sqlite.PurgeExpiredSessions(ctx)
	}
	tag, err := s.pool.Exec(ctx, `DELETE FROM sessions WHERE expires_at < now()`)
	if err != nil {
		return 0, wrap("store: purge sessions", err)
	}
	return tag.RowsAffected(), nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
