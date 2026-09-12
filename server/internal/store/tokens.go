package store

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
)

type APIToken struct {
	ID         string     `db:"id"           json:"id"`
	UserID     string     `db:"user_id"      json:"user_id"`
	Name       string     `db:"name"         json:"name"`
	Prefix     string     `db:"prefix"       json:"prefix"`
	LastUsedAt *time.Time `db:"last_used_at" json:"last_used_at"`
	ExpiresAt  *time.Time `db:"expires_at"   json:"expires_at"`
	CreatedAt  time.Time  `db:"created_at"   json:"created_at"`
}

const apiTokenColumns = `id, user_id, name, prefix, last_used_at, expires_at, created_at`

func (s *Store) CreateAPIToken(ctx context.Context, userID, name string, tokenHash []byte, prefix string, expiresAt *time.Time) (*APIToken, error) {
	rows, err := s.pool.Query(ctx, `
		INSERT INTO api_tokens (user_id, name, token_hash, prefix, expires_at)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING `+apiTokenColumns, userID, name, tokenHash, prefix, expiresAt)
	if err != nil {
		return nil, wrap("store: create api token", err)
	}
	token, err := pgx.CollectExactlyOneRow(rows, pgx.RowToStructByName[APIToken])
	if err != nil {
		return nil, wrap("store: create api token", err)
	}
	return &token, nil
}

// TokenUser is the joined result the auth middleware needs for bearer auth.
type TokenUser struct {
	Token APIToken
	User  User
}

// APITokenByHash resolves a bearer token. A NULL expiry means the token never
// expires, which is the common case for a CI credential.
func (s *Store) APITokenByHash(ctx context.Context, tokenHash []byte) (*TokenUser, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT t.id, t.user_id, t.name, t.prefix, t.last_used_at, t.expires_at, t.created_at,
		       u.id, u.email, u.name, u.password_hash, u.role, u.status,
		       u.last_login_at, u.created_at, u.updated_at
		FROM api_tokens t
		JOIN users u ON u.id = t.user_id
		WHERE t.token_hash = $1 AND (t.expires_at IS NULL OR t.expires_at > now())`, tokenHash)

	var out TokenUser
	err := row.Scan(
		&out.Token.ID, &out.Token.UserID, &out.Token.Name, &out.Token.Prefix,
		&out.Token.LastUsedAt, &out.Token.ExpiresAt, &out.Token.CreatedAt,
		&out.User.ID, &out.User.Email, &out.User.Name, &out.User.PasswordHash, &out.User.Role,
		&out.User.Status, &out.User.LastLoginAt, &out.User.CreatedAt, &out.User.UpdatedAt,
	)
	if err != nil {
		return nil, wrap("store: api token by hash", err)
	}
	return &out, nil
}

func (s *Store) TouchAPIToken(ctx context.Context, id string) error {
	_, err := s.pool.Exec(ctx, `UPDATE api_tokens SET last_used_at = now() WHERE id = $1`, id)
	return wrap("store: touch api token", err)
}

func (s *Store) ListAPITokens(ctx context.Context, userID string) ([]APIToken, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT `+apiTokenColumns+` FROM api_tokens WHERE user_id = $1 ORDER BY created_at DESC`, userID)
	if err != nil {
		return nil, wrap("store: list api tokens", err)
	}
	tokens, err := pgx.CollectRows(rows, pgx.RowToStructByName[APIToken])
	return tokens, wrap("store: list api tokens", err)
}

// DeleteAPIToken is scoped to the owner, so guessing another token id does not
// let one user revoke a token belonging to someone else.
func (s *Store) DeleteAPIToken(ctx context.Context, id, userID string) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM api_tokens WHERE id = $1 AND user_id = $2`, id, userID)
	if err != nil {
		return wrap("store: delete api token", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
