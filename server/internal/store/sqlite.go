package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/sirtheprogrammer/docker-deployments/server/internal/auth"
	"github.com/sirtheprogrammer/docker-deployments/server/internal/gitx"
	"github.com/sirtheprogrammer/docker-deployments/server/internal/secrets"
	"github.com/sirtheprogrammer/docker-deployments/server/internal/sshx"
)

func newUUID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40 // Version 4
	b[8] = (b[8] & 0x3f) | 0x80 // Variant 10
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

type runPubSub struct {
	mu   sync.Mutex
	subs map[chan struct{}]struct{}
}

func newRunPubSub() *runPubSub {
	return &runPubSub{subs: make(map[chan struct{}]struct{})}
}

func (ps *runPubSub) notify() {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	for ch := range ps.subs {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

func (ps *runPubSub) subscribe() (<-chan struct{}, func()) {
	ps.mu.Lock()
	ch := make(chan struct{}, 1)
	ps.subs[ch] = struct{}{}
	ps.mu.Unlock()

	var once sync.Once
	cancel := func() {
		once.Do(func() {
			ps.mu.Lock()
			delete(ps.subs, ch)
			close(ch)
			ps.mu.Unlock()
		})
	}
	return ch, cancel
}

type sqliteStore struct {
	db     *sql.DB
	pubsub *runPubSub
}

func newSQLiteStore(db *sql.DB) *sqliteStore {
	return &sqliteStore{
		db:     db,
		pubsub: newRunPubSub(),
	}
}

func (s *sqliteStore) Ping(ctx context.Context) error {
	return s.db.PingContext(ctx)
}

func (s *sqliteStore) Close() error {
	return s.db.Close()
}

func (s *sqliteStore) SubscribeRuns(ctx context.Context) (<-chan struct{}, func(), error) {
	ch, cancel := s.pubsub.subscribe()
	return ch, cancel, nil
}

func (s *sqliteStore) tx(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: begin sqlite tx: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()
	if err := fn(tx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: commit sqlite tx: %w", err)
	}
	return nil
}

func wrapSQLite(op string, err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%s: %w", op, ErrNotFound)
	}
	errMsg := err.Error()
	if strings.Contains(errMsg, "UNIQUE constraint failed") || strings.Contains(errMsg, "constraint failed: UNIQUE") {
		return fmt.Errorf("%s: %w", op, ErrConflict)
	}
	if strings.Contains(errMsg, "FOREIGN KEY constraint failed") {
		return fmt.Errorf("%s: %w", op, ErrNotFound)
	}
	return fmt.Errorf("%s: %w", op, err)
}

// --- Users ---

func (s *sqliteStore) CountUsers(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM users`).Scan(&n)
	return n, wrapSQLite("store: count users", err)
}

func (s *sqliteStore) CreateFirstAdmin(ctx context.Context, email, name, passwordHash string) (*User, error) {
	var user *User
	err := s.tx(ctx, func(tx *sql.Tx) error {
		var count int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM users`).Scan(&count); err != nil {
			return wrapSQLite("store: count users", err)
		}
		if count > 0 {
			return ErrConflict
		}

		id := newUUID()
		now := time.Now().UTC()
		_, err := tx.ExecContext(ctx, `
			INSERT INTO users (id, email, name, password_hash, role, status, created_at, updated_at)
			VALUES ($1, $2, $3, $4, 'admin', 'active', $5, $5)`,
			id, NormaliseEmail(email), name, passwordHash, now)
		if err != nil {
			return wrapSQLite("store: create first admin", err)
		}

		user = &User{
			ID:           id,
			Email:        NormaliseEmail(email),
			Name:         name,
			PasswordHash: passwordHash,
			Role:         auth.RoleAdmin,
			Status:       UserActive,
			CreatedAt:    now,
			UpdatedAt:    now,
		}
		return nil
	})
	return user, err
}

func (s *sqliteStore) CreateUser(ctx context.Context, email, name, passwordHash string, role auth.Role) (*User, error) {
	id := newUUID()
	now := time.Now().UTC()
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO users (id, email, name, password_hash, role, status, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, 'active', $6, $6)`,
		id, NormaliseEmail(email), name, passwordHash, role, now)
	if err != nil {
		return nil, wrapSQLite("store: create user", err)
	}

	return &User{
		ID:           id,
		Email:        NormaliseEmail(email),
		Name:         name,
		PasswordHash: passwordHash,
		Role:         role,
		Status:       UserActive,
		CreatedAt:    now,
		UpdatedAt:    now,
	}, nil
}

func (s *sqliteStore) UserByEmail(ctx context.Context, email string) (*User, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, email, name, password_hash, role, status, last_login_at, created_at, updated_at
		FROM users WHERE email = $1`, NormaliseEmail(email))
	return scanUser(row)
}

func (s *sqliteStore) UserByID(ctx context.Context, id string) (*User, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, email, name, password_hash, role, status, last_login_at, created_at, updated_at
		FROM users WHERE id = $1`, id)
	return scanUser(row)
}

func (s *sqliteStore) ListUsers(ctx context.Context) ([]User, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, email, name, password_hash, role, status, last_login_at, created_at, updated_at
		FROM users ORDER BY created_at ASC`)
	if err != nil {
		return nil, wrapSQLite("store: list users", err)
	}
	defer rows.Close()

	var users []User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		users = append(users, *u)
	}
	return users, rows.Err()
}

func (s *sqliteStore) CountAdmins(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM users WHERE role = 'admin' AND status = 'active'`).Scan(&n)
	return n, wrapSQLite("store: count admins", err)
}

func (s *sqliteStore) UpdateUser(ctx context.Context, id, name string, role auth.Role, status UserStatus) (*User, error) {
	now := time.Now().UTC()
	res, err := s.db.ExecContext(ctx, `
		UPDATE users SET name = $1, role = $2, status = $3, updated_at = $4
		WHERE id = $5`, name, role, status, now, id)
	if err != nil {
		return nil, wrapSQLite("store: update user", err)
	}
	rowsAffected, _ := res.RowsAffected()
	if rowsAffected == 0 {
		return nil, ErrNotFound
	}
	return s.UserByID(ctx, id)
}

func (s *sqliteStore) UpdateUserName(ctx context.Context, id, name string) (*User, error) {
	now := time.Now().UTC()
	res, err := s.db.ExecContext(ctx, `
		UPDATE users SET name = $1, updated_at = $2
		WHERE id = $3`, name, now, id)
	if err != nil {
		return nil, wrapSQLite("store: update user name", err)
	}
	rowsAffected, _ := res.RowsAffected()
	if rowsAffected == 0 {
		return nil, ErrNotFound
	}
	return s.UserByID(ctx, id)
}

func (s *sqliteStore) UpdateUserPassword(ctx context.Context, id, passwordHash, keepSessionID string) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		now := time.Now().UTC()
		res, err := tx.ExecContext(ctx, `
			UPDATE users SET password_hash = $1, updated_at = $2
			WHERE id = $3`, passwordHash, now, id)
		if err != nil {
			return wrapSQLite("store: update password", err)
		}
		rowsAffected, _ := res.RowsAffected()
		if rowsAffected == 0 {
			return ErrNotFound
		}

		if keepSessionID != "" {
			_, err = tx.ExecContext(ctx, `DELETE FROM sessions WHERE user_id = $1 AND id <> $2`, id, keepSessionID)
		} else {
			_, err = tx.ExecContext(ctx, `DELETE FROM sessions WHERE user_id = $1`, id)
		}
		return wrapSQLite("store: revoke sessions on password change", err)
	})
}

func (s *sqliteStore) TouchUserLogin(ctx context.Context, id string) error {
	now := time.Now().UTC()
	_, err := s.db.ExecContext(ctx, `UPDATE users SET last_login_at = $1 WHERE id = $2`, now, id)
	return wrapSQLite("store: touch user login", err)
}

func (s *sqliteStore) DeleteUser(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM users WHERE id = $1`, id)
	if err != nil {
		return wrapSQLite("store: delete user", err)
	}
	rowsAffected, _ := res.RowsAffected()
	if rowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func scanUser(r interface{ Scan(...any) error }) (*User, error) {
	var u User
	err := r.Scan(
		&u.ID, &u.Email, &u.Name, &u.PasswordHash, &u.Role, &u.Status,
		&u.LastLoginAt, &u.CreatedAt, &u.UpdatedAt,
	)
	if err != nil {
		return nil, wrapSQLite("store: scan user", err)
	}
	return &u, nil
}

// --- Sessions ---

func (s *sqliteStore) CreateSession(ctx context.Context, userID string, tokenHash []byte, expiresAt time.Time, ip, userAgent string) (*Session, error) {
	id := newUUID()
	now := time.Now().UTC()
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO sessions (id, user_id, token_hash, expires_at, last_used_at, ip, user_agent, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $5)`,
		id, userID, tokenHash, expiresAt, now, nullable(ip), nullable(truncate(userAgent, 512)))
	if err != nil {
		return nil, wrapSQLite("store: create session", err)
	}

	var ipPtr, uaPtr *string
	if ip != "" {
		ipPtr = &ip
	}
	if userAgent != "" {
		ua := truncate(userAgent, 512)
		uaPtr = &ua
	}

	return &Session{
		ID:         id,
		UserID:     userID,
		ExpiresAt:  expiresAt,
		LastUsedAt: now,
		IP:         ipPtr,
		UserAgent:  uaPtr,
		CreatedAt:  now,
	}, nil
}

func (s *sqliteStore) SessionByTokenHash(ctx context.Context, tokenHash []byte) (*SessionUser, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT s.id, s.user_id, s.expires_at, s.last_used_at, s.ip, s.user_agent, s.created_at,
		       u.id, u.email, u.name, u.password_hash, u.role, u.status,
		       u.last_login_at, u.created_at, u.updated_at
		FROM sessions s
		JOIN users u ON u.id = s.user_id
		WHERE s.token_hash = $1 AND s.expires_at > datetime('now')`, tokenHash)

	var out SessionUser
	err := row.Scan(
		&out.Session.ID, &out.Session.UserID, &out.Session.ExpiresAt, &out.Session.LastUsedAt,
		&out.Session.IP, &out.Session.UserAgent, &out.Session.CreatedAt,
		&out.User.ID, &out.User.Email, &out.User.Name, &out.User.PasswordHash, &out.User.Role,
		&out.User.Status, &out.User.LastLoginAt, &out.User.CreatedAt, &out.User.UpdatedAt,
	)
	if err != nil {
		return nil, wrapSQLite("store: session by token", err)
	}
	return &out, nil
}

func (s *sqliteStore) TouchSession(ctx context.Context, id string, expiresAt time.Time) error {
	now := time.Now().UTC()
	_, err := s.db.ExecContext(ctx, `
		UPDATE sessions SET last_used_at = $1, expires_at = $2 WHERE id = $3`, now, expiresAt, id)
	return wrapSQLite("store: touch session", err)
}

func (s *sqliteStore) ListSessions(ctx context.Context, userID string) ([]Session, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, user_id, expires_at, last_used_at, ip, user_agent, created_at
		FROM sessions WHERE user_id = $1 AND expires_at > datetime('now')
		ORDER BY last_used_at DESC`, userID)
	if err != nil {
		return nil, wrapSQLite("store: list sessions", err)
	}
	defer rows.Close()

	var sessions []Session
	for rows.Next() {
		var sess Session
		if err := rows.Scan(
			&sess.ID, &sess.UserID, &sess.ExpiresAt, &sess.LastUsedAt,
			&sess.IP, &sess.UserAgent, &sess.CreatedAt,
		); err != nil {
			return nil, wrapSQLite("store: scan session", err)
		}
		sessions = append(sessions, sess)
	}
	return sessions, rows.Err()
}

func (s *sqliteStore) DeleteSession(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE id = $1`, id)
	return wrapSQLite("store: delete session", err)
}

func (s *sqliteStore) DeleteSessionsForUser(ctx context.Context, userID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE user_id = $1`, userID)
	return wrapSQLite("store: delete user sessions", err)
}

func (s *sqliteStore) PurgeExpiredSessions(ctx context.Context) (int64, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at < datetime('now')`)
	if err != nil {
		return 0, wrapSQLite("store: purge sessions", err)
	}
	return res.RowsAffected()
}

// --- Invitations ---

func (s *sqliteStore) CreateInvitation(ctx context.Context, email, name string, role auth.Role, invitedBy string, tokenHash []byte, expiresAt time.Time) (*Invitation, error) {
	var inv *Invitation
	err := s.tx(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `DELETE FROM invitations WHERE email = $1 AND accepted_at IS NULL`, NormaliseEmail(email))
		if err != nil {
			return wrapSQLite("store: clear invitations", err)
		}

		id := newUUID()
		now := time.Now().UTC()
		_, err = tx.ExecContext(ctx, `
			INSERT INTO invitations (id, email, name, role, invited_by, token_hash, expires_at, created_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
			id, NormaliseEmail(email), name, role, nullable(invitedBy), tokenHash, expiresAt, now)
		if err != nil {
			return wrapSQLite("store: create invitation", err)
		}

		inv = &Invitation{
			ID:        id,
			Email:     NormaliseEmail(email),
			Name:      name,
			Role:      role,
			InvitedBy: nullable(invitedBy),
			ExpiresAt: expiresAt,
			CreatedAt: now,
		}
		return nil
	})
	return inv, err
}

func (s *sqliteStore) InvitationByTokenHash(ctx context.Context, tokenHash []byte) (*Invitation, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, email, name, role, invited_by, expires_at, accepted_at, accepted_user_id, created_at
		FROM invitations
		WHERE token_hash = $1 AND accepted_at IS NULL AND expires_at > datetime('now')`, tokenHash)

	var inv Invitation
	err := row.Scan(
		&inv.ID, &inv.Email, &inv.Name, &inv.Role, &inv.InvitedBy,
		&inv.ExpiresAt, &inv.AcceptedAt, &inv.AcceptedUserID, &inv.CreatedAt,
	)
	if err != nil {
		return nil, wrapSQLite("store: invitation by token", err)
	}
	return &inv, nil
}

func (s *sqliteStore) ListInvitations(ctx context.Context) ([]Invitation, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, email, name, role, invited_by, expires_at, accepted_at, accepted_user_id, created_at
		FROM invitations
		WHERE accepted_at IS NULL AND expires_at > datetime('now')
		ORDER BY created_at DESC`)
	if err != nil {
		return nil, wrapSQLite("store: list invitations", err)
	}
	defer rows.Close()

	var invs []Invitation
	for rows.Next() {
		var inv Invitation
		if err := rows.Scan(
			&inv.ID, &inv.Email, &inv.Name, &inv.Role, &inv.InvitedBy,
			&inv.ExpiresAt, &inv.AcceptedAt, &inv.AcceptedUserID, &inv.CreatedAt,
		); err != nil {
			return nil, wrapSQLite("store: scan invitation", err)
		}
		invs = append(invs, inv)
	}
	return invs, rows.Err()
}

func (s *sqliteStore) AcceptInvitation(ctx context.Context, invitationID, name, passwordHash string) (*User, error) {
	var user *User
	err := s.tx(ctx, func(tx *sql.Tx) error {
		var email string
		var role auth.Role
		err := tx.QueryRowContext(ctx, `
			SELECT email, role FROM invitations
			WHERE id = $1 AND accepted_at IS NULL AND expires_at > datetime('now')`, invitationID).Scan(&email, &role)
		if err != nil {
			return wrapSQLite("store: read invitation", err)
		}

		now := time.Now().UTC()
		_, err = tx.ExecContext(ctx, `
			UPDATE invitations SET accepted_at = $1 WHERE id = $2`, now, invitationID)
		if err != nil {
			return wrapSQLite("store: spend invitation", err)
		}

		userID := newUUID()
		_, err = tx.ExecContext(ctx, `
			INSERT INTO users (id, email, name, password_hash, role, status, created_at, updated_at)
			VALUES ($1, $2, $3, $4, $5, 'active', $6, $6)`,
			userID, email, name, passwordHash, role, now)
		if err != nil {
			return wrapSQLite("store: create invited user", err)
		}

		_, err = tx.ExecContext(ctx, `
			UPDATE invitations SET accepted_user_id = $1 WHERE id = $2`, userID, invitationID)
		if err != nil {
			return wrapSQLite("store: link invitation", err)
		}

		user = &User{
			ID:           userID,
			Email:        email,
			Name:         name,
			PasswordHash: passwordHash,
			Role:         role,
			Status:       UserActive,
			CreatedAt:    now,
			UpdatedAt:    now,
		}
		return nil
	})
	return user, err
}

func (s *sqliteStore) DeleteInvitation(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM invitations WHERE id = $1`, id)
	if err != nil {
		return wrapSQLite("store: delete invitation", err)
	}
	rowsAffected, _ := res.RowsAffected()
	if rowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// --- API Tokens ---

func (s *sqliteStore) CreateAPIToken(ctx context.Context, userID, name string, tokenHash []byte, prefix string, expiresAt *time.Time) (*APIToken, error) {
	id := newUUID()
	now := time.Now().UTC()
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO api_tokens (id, user_id, name, token_hash, prefix, expires_at, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		id, userID, name, tokenHash, prefix, expiresAt, now)
	if err != nil {
		return nil, wrapSQLite("store: create api token", err)
	}

	return &APIToken{
		ID:        id,
		UserID:    userID,
		Name:      name,
		Prefix:    prefix,
		ExpiresAt: expiresAt,
		CreatedAt: now,
	}, nil
}

func (s *sqliteStore) APITokenByHash(ctx context.Context, tokenHash []byte) (*TokenUser, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT t.id, t.user_id, t.name, t.prefix, t.last_used_at, t.expires_at, t.created_at,
		       u.id, u.email, u.name, u.password_hash, u.role, u.status,
		       u.last_login_at, u.created_at, u.updated_at
		FROM api_tokens t
		JOIN users u ON u.id = t.user_id
		WHERE t.token_hash = $1 AND (t.expires_at IS NULL OR t.expires_at > datetime('now'))`, tokenHash)

	var out TokenUser
	err := row.Scan(
		&out.Token.ID, &out.Token.UserID, &out.Token.Name, &out.Token.Prefix,
		&out.Token.LastUsedAt, &out.Token.ExpiresAt, &out.Token.CreatedAt,
		&out.User.ID, &out.User.Email, &out.User.Name, &out.User.PasswordHash, &out.User.Role,
		&out.User.Status, &out.User.LastLoginAt, &out.User.CreatedAt, &out.User.UpdatedAt,
	)
	if err != nil {
		return nil, wrapSQLite("store: api token by hash", err)
	}
	return &out, nil
}

func (s *sqliteStore) TouchAPIToken(ctx context.Context, id string) error {
	now := time.Now().UTC()
	_, err := s.db.ExecContext(ctx, `UPDATE api_tokens SET last_used_at = $1 WHERE id = $2`, now, id)
	return wrapSQLite("store: touch api token", err)
}

func (s *sqliteStore) ListAPITokens(ctx context.Context, userID string) ([]APIToken, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, user_id, name, prefix, last_used_at, expires_at, created_at
		FROM api_tokens WHERE user_id = $1 ORDER BY created_at DESC`, userID)
	if err != nil {
		return nil, wrapSQLite("store: list api tokens", err)
	}
	defer rows.Close()

	var tokens []APIToken
	for rows.Next() {
		var t APIToken
		if err := rows.Scan(
			&t.ID, &t.UserID, &t.Name, &t.Prefix, &t.LastUsedAt, &t.ExpiresAt, &t.CreatedAt,
		); err != nil {
			return nil, wrapSQLite("store: scan api token", err)
		}
		tokens = append(tokens, t)
	}
	return tokens, rows.Err()
}

func (s *sqliteStore) DeleteAPIToken(ctx context.Context, id, userID string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM api_tokens WHERE id = $1 AND user_id = $2`, id, userID)
	if err != nil {
		return wrapSQLite("store: delete api token", err)
	}
	rowsAffected, _ := res.RowsAffected()
	if rowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// --- Audit ---

func (s *sqliteStore) WriteAudit(ctx context.Context, e AuditEntry) error {
	meta := e.Meta
	if len(meta) == 0 {
		meta = json.RawMessage(`{}`)
	}
	now := time.Now().UTC()
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO audit_log (user_id, actor_email, action, resource_type, resource_id, status, ip, meta, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		e.UserID, e.ActorEmail, e.Action, e.ResourceType, e.ResourceID, e.Status, e.IP, string(meta), now)
	return wrapSQLite("store: write audit", err)
}

func (s *sqliteStore) ListAudit(ctx context.Context, f AuditFilter) ([]AuditEntry, error) {
	limit := f.Limit
	if limit <= 0 || limit > 200 {
		limit = 50
	}

	query := `SELECT id, user_id, actor_email, action, resource_type, resource_id, status, ip, meta, created_at FROM audit_log WHERE 1=1`
	var args []any
	argIdx := 1

	if f.UserID != "" {
		query += fmt.Sprintf(` AND user_id = $%d`, argIdx)
		args = append(args, f.UserID)
		argIdx++
	}
	if f.ResourceType != "" {
		query += fmt.Sprintf(` AND resource_type = $%d`, argIdx)
		args = append(args, f.ResourceType)
		argIdx++
	}
	if f.ResourceID != "" {
		query += fmt.Sprintf(` AND resource_id = $%d`, argIdx)
		args = append(args, f.ResourceID)
		argIdx++
	}
	if f.Before != nil {
		query += fmt.Sprintf(` AND created_at < $%d`, argIdx)
		args = append(args, f.Before.UTC())
		argIdx++
	}
	query += fmt.Sprintf(` ORDER BY created_at DESC, id DESC LIMIT $%d`, argIdx)
	args = append(args, limit)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, wrapSQLite("store: list audit", err)
	}
	defer rows.Close()

	var entries []AuditEntry
	for rows.Next() {
		var e AuditEntry
		var metaStr string
		if err := rows.Scan(
			&e.ID, &e.UserID, &e.ActorEmail, &e.Action, &e.ResourceType, &e.ResourceID,
			&e.Status, &e.IP, &metaStr, &e.CreatedAt,
		); err != nil {
			return nil, wrapSQLite("store: scan audit", err)
		}
		e.Meta = json.RawMessage(metaStr)
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

func (s *sqliteStore) PurgeOldAuditLogs(ctx context.Context, olderThan time.Duration) (int64, error) {
	cutoff := time.Now().UTC().Add(-olderThan)
	res, err := s.db.ExecContext(ctx, `DELETE FROM audit_log WHERE created_at < $1`, cutoff)
	if err != nil {
		return 0, wrapSQLite("store: purge audit logs", err)
	}
	return res.RowsAffected()
}

// --- Secrets ---

func (s *sqliteStore) insertSecret(ctx context.Context, tx *sql.Tx, sealer Sealer, kind secrets.Kind, plaintext string) (*string, error) {
	id := newUUID()
	nonce, ciphertext, err := sealer.SealString(plaintext, []byte(id))
	if err != nil {
		return nil, fmt.Errorf("store: seal secret: %w", err)
	}

	now := time.Now().UTC()
	_, err = tx.ExecContext(ctx, `
		INSERT INTO secrets (id, kind, nonce, ciphertext, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $5)`, id, kind, nonce, ciphertext, now)
	if err != nil {
		return nil, wrapSQLite("store: insert secret", err)
	}
	return &id, nil
}

func (s *sqliteStore) openSecret(ctx context.Context, sealer Sealer, id *string) (string, error) {
	if id == nil {
		return "", nil
	}

	var nonce, ciphertext []byte
	err := s.db.QueryRowContext(ctx, `SELECT nonce, ciphertext FROM secrets WHERE id = $1`, *id).Scan(&nonce, &ciphertext)
	if err != nil {
		return "", wrapSQLite("store: read secret", err)
	}

	plaintext, err := sealer.OpenString(nonce, ciphertext, []byte(*id))
	if err != nil {
		return "", fmt.Errorf("store: decrypt secret %s: %w", *id, err)
	}
	return plaintext, nil
}

func (s *sqliteStore) ReplaceSecret(ctx context.Context, sealer Sealer, id, plaintext string) error {
	nonce, ciphertext, err := sealer.SealString(plaintext, []byte(id))
	if err != nil {
		return fmt.Errorf("store: seal secret: %w", err)
	}
	now := time.Now().UTC()
	res, err := s.db.ExecContext(ctx, `
		UPDATE secrets SET nonce = $1, ciphertext = $2, updated_at = $3 WHERE id = $4`, nonce, ciphertext, now, id)
	if err != nil {
		return wrapSQLite("store: replace secret", err)
	}
	rowsAffected, _ := res.RowsAffected()
	if rowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *sqliteStore) deleteWithSecret(ctx context.Context, table, id, secretColumn string) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		var secretID *string
		err := tx.QueryRowContext(ctx, `SELECT `+secretColumn+` FROM `+table+` WHERE id = $1`, id).Scan(&secretID)
		if err != nil {
			return wrapSQLite("store: delete "+table, err)
		}

		res, err := tx.ExecContext(ctx, `DELETE FROM `+table+` WHERE id = $1`, id)
		if err != nil {
			return wrapSQLite("store: delete "+table, err)
		}
		rowsAffected, _ := res.RowsAffected()
		if rowsAffected == 0 {
			return ErrNotFound
		}

		if secretID != nil {
			_, err = tx.ExecContext(ctx, `DELETE FROM secrets WHERE id = $1`, *secretID)
			if err != nil {
				return wrapSQLite("store: delete "+table+" secret", err)
			}
		}
		return nil
	})
}

// --- Servers ---

func (s *sqliteStore) CreateServer(ctx context.Context, sealer Sealer, in NewServer) (*Server, error) {
	var server *Server
	err := s.tx(ctx, func(tx *sql.Tx) error {
		var secretID, passphraseID, sudoID *string
		switch in.AuthMethod {
		case sshx.AuthPassword:
			id, err := s.insertSecret(ctx, tx, sealer, KindSSHPassword, in.Password)
			if err != nil {
				return err
			}
			secretID = id
		case sshx.AuthKey:
			id, err := s.insertSecret(ctx, tx, sealer, KindSSHPrivateKey, in.PrivateKey)
			if err != nil {
				return err
			}
			secretID = id
			if in.Passphrase != "" {
				pid, err := s.insertSecret(ctx, tx, sealer, KindSSHPassphrase, in.Passphrase)
				if err != nil {
					return err
				}
				passphraseID = pid
			}
		}

		if in.SudoPassword != "" {
			sid, err := s.insertSecret(ctx, tx, sealer, KindSudoPassword, in.SudoPassword)
			if err != nil {
				return err
			}
			sudoID = sid
		}

		socket := in.DockerSocket
		if socket == "" {
			socket = "/var/run/docker.sock"
		}
		port := in.Port
		if port == 0 {
			port = 22
		}

		id := newUUID()
		now := time.Now().UTC()
		_, err := tx.ExecContext(ctx, `
			INSERT INTO servers (
				id, name, host, port, username, auth_method,
				secret_id, passphrase_secret_id, sudo_password_secret_id,
				host_key_fingerprint, docker_socket, status, status_message, capabilities,
				created_by, created_at, updated_at
			)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, 'unknown', '', '{}', $12, $13, $13)`,
			id, in.Name, in.Host, port, in.Username, in.AuthMethod,
			secretID, passphraseID, sudoID,
			in.HostKeyFingerprint, socket, nullable(in.CreatedBy), now)
		if err != nil {
			return wrapSQLite("store: create server", err)
		}

		server = &Server{
			ID:                   id,
			Name:                 in.Name,
			Host:                 in.Host,
			Port:                 port,
			Username:             in.Username,
			AuthMethod:           in.AuthMethod,
			SecretID:             secretID,
			PassphraseSecretID:   passphraseID,
			SudoPasswordSecretID: sudoID,
			HostKeyFingerprint:   in.HostKeyFingerprint,
			DockerSocket:         socket,
			Status:               ServerUnknown,
			Capabilities:         json.RawMessage("{}"),
			CreatedBy:            nullable(in.CreatedBy),
			CreatedAt:            now,
			UpdatedAt:            now,
		}
		return nil
	})
	return server, err
}

func (s *sqliteStore) ListServers(ctx context.Context, userID string, all bool) ([]Server, error) {
	var query string
	var args []any
	if all {
		query = `SELECT ` + serverColumns + ` FROM servers ORDER BY name`
	} else {
		query = `SELECT ` + serverColumns + ` FROM servers WHERE id IN (SELECT server_id FROM server_members WHERE user_id = $1) ORDER BY name`
		args = append(args, userID)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, wrapSQLite("store: list servers", err)
	}
	defer rows.Close()

	var servers []Server
	for rows.Next() {
		srv, err := scanServer(rows)
		if err != nil {
			return nil, err
		}
		servers = append(servers, *srv)
	}
	return servers, rows.Err()
}

func (s *sqliteStore) ServerByID(ctx context.Context, id string) (*Server, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+serverColumns+` FROM servers WHERE id = $1`, id)
	return scanServer(row)
}

func (s *sqliteStore) CanAccessServer(ctx context.Context, serverID, userID string, isAdmin bool) (bool, error) {
	if isAdmin {
		return true, nil
	}
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM server_members WHERE server_id = $1 AND user_id = $2`, serverID, userID).Scan(&count)
	if err != nil {
		return false, wrapSQLite("store: access check", err)
	}
	return count > 0, nil
}

func (s *sqliteStore) ServerCredential(ctx context.Context, sealer Sealer, server *Server) (*ServerCredential, error) {
	out := &ServerCredential{
		Target: sshx.Target{
			Host:        server.Host,
			Port:        server.Port,
			User:        server.Username,
			Fingerprint: server.HostKeyFingerprint,
		},
		Credential: sshx.Credential{Method: server.AuthMethod},
	}

	secret, err := s.openSecret(ctx, sealer, server.SecretID)
	if err != nil {
		return nil, err
	}
	switch server.AuthMethod {
	case sshx.AuthPassword:
		out.Credential.Password = secret
	case sshx.AuthKey:
		out.Credential.PrivateKey = []byte(secret)
		if out.Credential.Passphrase, err = s.openSecret(ctx, sealer, server.PassphraseSecretID); err != nil {
			return nil, err
		}
	}

	if out.SudoPassword, err = s.openSecret(ctx, sealer, server.SudoPasswordSecretID); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *sqliteStore) UpdateServerStatus(ctx context.Context, id string, status ServerStatus, message string, caps *sshx.Capabilities) error {
	var capsJSON string
	if caps != nil {
		b, err := json.Marshal(caps)
		if err != nil {
			return fmt.Errorf("store: encode capabilities: %w", err)
		}
		capsJSON = string(b)
	}

	now := time.Now().UTC()
	var err error
	if caps != nil {
		if status == ServerOnline {
			_, err = s.db.ExecContext(ctx, `
				UPDATE servers SET status = $1, status_message = $2, capabilities = $3, last_seen_at = $4, updated_at = $4
				WHERE id = $5`, status, message, capsJSON, now, id)
		} else {
			_, err = s.db.ExecContext(ctx, `
				UPDATE servers SET status = $1, status_message = $2, capabilities = $3, updated_at = $4
				WHERE id = $5`, status, message, capsJSON, now, id)
		}
	} else {
		if status == ServerOnline {
			_, err = s.db.ExecContext(ctx, `
				UPDATE servers SET status = $1, status_message = $2, last_seen_at = $3, updated_at = $3
				WHERE id = $4`, status, message, now, id)
		} else {
			_, err = s.db.ExecContext(ctx, `
				UPDATE servers SET status = $1, status_message = $2, updated_at = $3
				WHERE id = $4`, status, message, now, id)
		}
	}
	return wrapSQLite("store: update server status", err)
}

func (s *sqliteStore) UpdateServer(ctx context.Context, id, name, dockerSocket string) (*Server, error) {
	now := time.Now().UTC()
	res, err := s.db.ExecContext(ctx, `
		UPDATE servers SET name = $1, docker_socket = $2, updated_at = $3 WHERE id = $4`, name, dockerSocket, now, id)
	if err != nil {
		return nil, wrapSQLite("store: update server", err)
	}
	rowsAffected, _ := res.RowsAffected()
	if rowsAffected == 0 {
		return nil, ErrNotFound
	}
	return s.ServerByID(ctx, id)
}

func (s *sqliteStore) DeleteServer(ctx context.Context, id string) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		var secID, passID, sudoID *string
		err := tx.QueryRowContext(ctx, `
			SELECT secret_id, passphrase_secret_id, sudo_password_secret_id FROM servers WHERE id = $1`, id).
			Scan(&secID, &passID, &sudoID)
		if err != nil {
			return wrapSQLite("store: delete server", err)
		}

		_, err = tx.ExecContext(ctx, `DELETE FROM servers WHERE id = $1`, id)
		if err != nil {
			return wrapSQLite("store: delete server", err)
		}

		for _, sid := range []*string{secID, passID, sudoID} {
			if sid != nil {
				_, _ = tx.ExecContext(ctx, `DELETE FROM secrets WHERE id = $1`, *sid)
			}
		}
		return nil
	})
}

func (s *sqliteStore) ListServerMembers(ctx context.Context, serverID string) ([]ServerMember, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT m.server_id, m.user_id, u.email, u.name, m.permission, m.created_at
		FROM server_members m
		JOIN users u ON u.id = m.user_id
		WHERE m.server_id = $1
		ORDER BY u.email`, serverID)
	if err != nil {
		return nil, wrapSQLite("store: list server members", err)
	}
	defer rows.Close()

	var members []ServerMember
	for rows.Next() {
		var m ServerMember
		if err := rows.Scan(&m.ServerID, &m.UserID, &m.Email, &m.Name, &m.Permission, &m.CreatedAt); err != nil {
			return nil, wrapSQLite("store: scan server member", err)
		}
		members = append(members, m)
	}
	return members, rows.Err()
}

func (s *sqliteStore) GrantServerAccess(ctx context.Context, serverID, userID string, permission ServerPermission) error {
	now := time.Now().UTC()
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO server_members (server_id, user_id, permission, created_at)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (server_id, user_id) DO UPDATE SET permission = excluded.permission`,
		serverID, userID, permission, now)
	return wrapSQLite("store: grant server access", err)
}

func (s *sqliteStore) RevokeServerAccess(ctx context.Context, serverID, userID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM server_members WHERE server_id = $1 AND user_id = $2`, serverID, userID)
	return wrapSQLite("store: revoke server access", err)
}

func scanServer(r interface{ Scan(...any) error }) (*Server, error) {
	var s Server
	var capsJSON string
	err := r.Scan(
		&s.ID, &s.Name, &s.Host, &s.Port, &s.Username, &s.AuthMethod,
		&s.SecretID, &s.PassphraseSecretID, &s.SudoPasswordSecretID,
		&s.HostKeyFingerprint, &s.DockerSocket, &s.Status, &s.StatusMessage,
		&capsJSON, &s.LastSeenAt, &s.CreatedBy, &s.CreatedAt, &s.UpdatedAt,
	)
	if err != nil {
		return nil, wrapSQLite("store: scan server", err)
	}
	if capsJSON != "" {
		_ = json.Unmarshal([]byte(capsJSON), &s.Capabilities)
	}
	return &s, nil
}

// --- Credentials & Registries ---

func (s *sqliteStore) CreateRegistry(ctx context.Context, sealer Sealer, name, url, username, password, createdBy string) (*Registry, error) {
	var reg *Registry
	err := s.tx(ctx, func(tx *sql.Tx) error {
		secretID, err := s.insertSecret(ctx, tx, sealer, KindRegistryToken, password)
		if err != nil {
			return err
		}

		id := newUUID()
		now := time.Now().UTC()
		_, err = tx.ExecContext(ctx, `
			INSERT INTO registries (id, name, url, username, secret_id, created_by, created_at, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $7)`,
			id, name, url, username, secretID, nullable(createdBy), now)
		if err != nil {
			return wrapSQLite("store: create registry", err)
		}

		reg = &Registry{
			ID:        id,
			Name:      name,
			URL:       url,
			Username:  username,
			SecretID:  secretID,
			CreatedBy: nullable(createdBy),
			CreatedAt: now,
			UpdatedAt: now,
		}
		return nil
	})
	return reg, err
}

func (s *sqliteStore) ListRegistries(ctx context.Context) ([]Registry, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+registryColumns+` FROM registries ORDER BY name`)
	if err != nil {
		return nil, wrapSQLite("store: list registries", err)
	}
	defer rows.Close()

	var registries []Registry
	for rows.Next() {
		var reg Registry
		if err := rows.Scan(
			&reg.ID, &reg.Name, &reg.URL, &reg.Username, &reg.SecretID,
			&reg.CreatedBy, &reg.CreatedAt, &reg.UpdatedAt,
		); err != nil {
			return nil, wrapSQLite("store: scan registry", err)
		}
		registries = append(registries, reg)
	}
	return registries, rows.Err()
}

func (s *sqliteStore) RegistryByID(ctx context.Context, id string) (*Registry, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+registryColumns+` FROM registries WHERE id = $1`, id)
	var reg Registry
	err := row.Scan(
		&reg.ID, &reg.Name, &reg.URL, &reg.Username, &reg.SecretID,
		&reg.CreatedBy, &reg.CreatedAt, &reg.UpdatedAt,
	)
	if err != nil {
		return nil, wrapSQLite("store: registry by id", err)
	}
	return &reg, nil
}

func (s *sqliteStore) RegistryPassword(ctx context.Context, sealer Sealer, registry *Registry) (string, error) {
	return s.openSecret(ctx, sealer, registry.SecretID)
}

func (s *sqliteStore) DeleteRegistry(ctx context.Context, id string) error {
	return s.deleteWithSecret(ctx, "registries", id, "secret_id")
}

func (s *sqliteStore) CreateGitCredential(ctx context.Context, sealer Sealer, name string, kind gitx.Kind, username, secret, createdBy string) (*GitCredential, error) {
	var cred *GitCredential
	err := s.tx(ctx, func(tx *sql.Tx) error {
		secretID, err := s.insertSecret(ctx, tx, sealer, KindGitToken, secret)
		if err != nil {
			return err
		}

		id := newUUID()
		now := time.Now().UTC()
		_, err = tx.ExecContext(ctx, `
			INSERT INTO git_credentials (id, name, provider, kind, username, secret_id, created_by, created_at, updated_at)
			VALUES ($1, $2, 'generic', $3, $4, $5, $6, $7, $7)`,
			id, name, kind, username, secretID, nullable(createdBy), now)
		if err != nil {
			return wrapSQLite("store: create git credential", err)
		}

		cred = &GitCredential{
			ID:        id,
			Name:      name,
			Provider:  "generic",
			Kind:      kind,
			Username:  username,
			SecretID:  secretID,
			CreatedBy: nullable(createdBy),
			CreatedAt: now,
			UpdatedAt: now,
		}
		return nil
	})
	return cred, err
}

func (s *sqliteStore) ListGitCredentials(ctx context.Context) ([]GitCredential, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+gitCredentialColumns+` FROM git_credentials ORDER BY name`)
	if err != nil {
		return nil, wrapSQLite("store: list git credentials", err)
	}
	defer rows.Close()

	var creds []GitCredential
	for rows.Next() {
		var c GitCredential
		if err := rows.Scan(
			&c.ID, &c.Name, &c.Provider, &c.Kind, &c.Username, &c.SecretID,
			&c.CreatedBy, &c.CreatedAt, &c.UpdatedAt,
		); err != nil {
			return nil, wrapSQLite("store: scan git credential", err)
		}
		creds = append(creds, c)
	}
	return creds, rows.Err()
}

func (s *sqliteStore) GitCredentialByID(ctx context.Context, id string) (*GitCredential, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+gitCredentialColumns+` FROM git_credentials WHERE id = $1`, id)
	var c GitCredential
	err := row.Scan(
		&c.ID, &c.Name, &c.Provider, &c.Kind, &c.Username, &c.SecretID,
		&c.CreatedBy, &c.CreatedAt, &c.UpdatedAt,
	)
	if err != nil {
		return nil, wrapSQLite("store: git credential by id", err)
	}
	return &c, nil
}

func (s *sqliteStore) ResolveGitCredential(ctx context.Context, sealer Sealer, id *string) (*gitx.Credential, error) {
	if id == nil {
		return nil, nil
	}
	cred, err := s.GitCredentialByID(ctx, *id)
	if err != nil {
		return nil, err
	}
	sec, err := s.openSecret(ctx, sealer, cred.SecretID)
	if err != nil {
		return nil, err
	}
	return &gitx.Credential{
		Kind:     cred.Kind,
		Username: cred.Username,
		Secret:   sec,
	}, nil
}

func (s *sqliteStore) DeleteGitCredential(ctx context.Context, id string) error {
	return s.deleteWithSecret(ctx, "git_credentials", id, "secret_id")
}

// --- Deployments ---

func (s *sqliteStore) CreateDeployment(ctx context.Context, sealer Sealer, in NewDeployment) (*Deployment, error) {
	var d *Deployment
	err := s.tx(ctx, func(tx *sql.Tx) error {
		whSecret, err := s.insertSecret(ctx, tx, sealer, KindWebhookSecret, in.WebhookSecret)
		if err != nil {
			return err
		}

		id := newUUID()
		now := time.Now().UTC()
		_, err = tx.ExecContext(ctx, `
			INSERT INTO deployments (
				id, server_id, name, slug, source_type,
				repo_url, git_ref, git_credential_id,
				dockerfile_path, build_context, compose_path, compose_content, image_ref,
				build_strategy, registry_id, image_name,
				workdir, host_port, container_port, webhook_secret_id, status, created_by, created_at, updated_at
			)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,'never_deployed',$21,$22,$22)`,
			id, in.ServerID, in.Name, in.Slug, in.SourceType,
			in.RepoURL, in.GitRef, in.GitCredentialID,
			in.DockerfilePath, in.BuildContext, in.ComposePath, in.ComposeContent, in.ImageRef,
			in.BuildStrategy, in.RegistryID, in.ImageName,
			in.Workdir, in.HostPort, in.ContainerPort, whSecret, nullable(in.CreatedBy), now)
		if err != nil {
			return wrapSQLite("store: create deployment", err)
		}

		d = &Deployment{
			ID:                id,
			ServerID:          in.ServerID,
			Name:              in.Name,
			Slug:              in.Slug,
			SourceType:        in.SourceType,
			RepoURL:           in.RepoURL,
			GitRef:            in.GitRef,
			GitCredentialID:   in.GitCredentialID,
			DockerfilePath:    in.DockerfilePath,
			BuildContext:      in.BuildContext,
			ComposePath:       in.ComposePath,
			ComposeContent:    in.ComposeContent,
			ImageRef:          in.ImageRef,
			BuildStrategy:     in.BuildStrategy,
			RegistryID:        in.RegistryID,
			ImageName:         in.ImageName,
			Workdir:           in.Workdir,
			HostPort:          in.HostPort,
			ContainerPort:     in.ContainerPort,
			WebhookSecretID:   whSecret,
			Status:            DeploymentNeverDeployed,
			CreatedBy:         nullable(in.CreatedBy),
			CreatedAt:         now,
			UpdatedAt:         now,
		}
		return nil
	})
	return d, err
}

func (s *sqliteStore) ListDeployments(ctx context.Context, userID string, all bool) ([]Deployment, error) {
	var query string
	var args []any
	if all {
		query = `SELECT ` + deploymentColumns + ` FROM deployments ORDER BY name`
	} else {
		query = `SELECT ` + deploymentColumns + ` FROM deployments WHERE server_id IN (SELECT server_id FROM server_members WHERE user_id = $1) ORDER BY name`
		args = append(args, userID)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, wrapSQLite("store: list deployments", err)
	}
	defer rows.Close()

	var deps []Deployment
	for rows.Next() {
		d, err := scanDeployment(rows)
		if err != nil {
			return nil, err
		}
		deps = append(deps, *d)
	}
	return deps, rows.Err()
}

func (s *sqliteStore) DeploymentByID(ctx context.Context, id string) (*Deployment, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+deploymentColumns+` FROM deployments WHERE id = $1`, id)
	return scanDeployment(row)
}

func (s *sqliteStore) UpdateDeploymentStatus(ctx context.Context, id string, status DeploymentStatus, runID *string) error {
	now := time.Now().UTC()
	var err error
	if runID != nil {
		_, err = s.db.ExecContext(ctx, `
			UPDATE deployments SET status = $1, current_run_id = $2, updated_at = $3 WHERE id = $4`,
			status, *runID, now, id)
	} else {
		_, err = s.db.ExecContext(ctx, `
			UPDATE deployments SET status = $1, updated_at = $2 WHERE id = $3`,
			status, now, id)
	}
	return wrapSQLite("store: update deployment status", err)
}

func (s *sqliteStore) SetDeploymentPort(ctx context.Context, id string, port int) error {
	now := time.Now().UTC()
	res, err := s.db.ExecContext(ctx, `
		UPDATE deployments SET host_port = $1, updated_at = $2 WHERE id = $3`, port, now, id)
	if err != nil {
		return wrapSQLite("store: set deployment port", err)
	}
	rowsAffected, _ := res.RowsAffected()
	if rowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *sqliteStore) DeleteDeployment(ctx context.Context, id string) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		var whSecretID *string
		_ = tx.QueryRowContext(ctx, `SELECT webhook_secret_id FROM deployments WHERE id = $1`, id).Scan(&whSecretID)

		var envSecretIDs []string
		rows, err := tx.QueryContext(ctx, `SELECT secret_id FROM deployment_env WHERE deployment_id = $1 AND secret_id IS NOT NULL`, id)
		if err == nil {
			for rows.Next() {
				var sid string
				if err := rows.Scan(&sid); err == nil {
					envSecretIDs = append(envSecretIDs, sid)
				}
			}
			rows.Close()
		}

		res, err := tx.ExecContext(ctx, `DELETE FROM deployments WHERE id = $1`, id)
		if err != nil {
			return wrapSQLite("store: delete deployment", err)
		}
		rowsAffected, _ := res.RowsAffected()
		if rowsAffected == 0 {
			return ErrNotFound
		}

		if whSecretID != nil {
			_, _ = tx.ExecContext(ctx, `DELETE FROM secrets WHERE id = $1`, *whSecretID)
		}
		for _, sid := range envSecretIDs {
			_, _ = tx.ExecContext(ctx, `DELETE FROM secrets WHERE id = $1`, sid)
		}

		return nil
	})
}

func (s *sqliteStore) AllocateHostPort(ctx context.Context, serverID string, min, max int, inUse []int) (int, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT host_port FROM deployments WHERE server_id = $1 AND host_port IS NOT NULL`, serverID)
	if err != nil {
		return 0, wrapSQLite("store: read allocated ports", err)
	}
	defer rows.Close()

	taken := make(map[int]bool, len(inUse)+16)
	for _, p := range inUse {
		taken[p] = true
	}
	for rows.Next() {
		var p int
		if err := rows.Scan(&p); err == nil {
			taken[p] = true
		}
	}

	for port := min; port <= max; port++ {
		if !taken[port] {
			return port, nil
		}
	}
	return 0, fmt.Errorf("store: port range %d-%d exhausted on server %s", min, max, serverID)
}

func (s *sqliteStore) SetDeploymentEnv(ctx context.Context, sealer Sealer, deploymentID string, vars []EnvVar) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		type existingEnv struct {
			secretID *string
			isSecret bool
		}
		existing := make(map[string]existingEnv)
		rows, err := tx.QueryContext(ctx, `
			SELECT key, secret_id, is_secret FROM deployment_env WHERE deployment_id = $1`, deploymentID)
		if err != nil {
			return wrapSQLite("store: list existing env", err)
		}
		for rows.Next() {
			var k string
			var sid *string
			var isSec bool
			if err := rows.Scan(&k, &sid, &isSec); err == nil {
				existing[k] = existingEnv{secretID: sid, isSecret: isSec}
			}
		}
		rows.Close()

		kept := make(map[string]bool, len(vars))
		for _, v := range vars {
			kept[v.Key] = true
			cur, exists := existing[v.Key]

			switch {
			case v.IsSecret && !exists:
				sid, err := s.insertSecret(ctx, tx, sealer, KindDeploymentEnv, v.Value)
				if err != nil {
					return err
				}
				_, err = tx.ExecContext(ctx, `
					INSERT INTO deployment_env (deployment_id, key, value, secret_id, is_secret)
					VALUES ($1, $2, '', $3, 1)`, deploymentID, v.Key, sid)
				if err != nil {
					return wrapSQLite("store: insert secret env", err)
				}

			case v.IsSecret && exists:
				if v.Value != "" {
					if cur.secretID != nil {
						if err := s.ReplaceSecret(ctx, sealer, *cur.secretID, v.Value); err != nil {
							return err
						}
					} else {
						sid, err := s.insertSecret(ctx, tx, sealer, KindDeploymentEnv, v.Value)
						if err != nil {
							return err
						}
						_, err = tx.ExecContext(ctx, `
							UPDATE deployment_env SET value = '', secret_id = $1, is_secret = 1
							WHERE deployment_id = $2 AND key = $3`, sid, deploymentID, v.Key)
						if err != nil {
							return wrapSQLite("store: convert to secret env", err)
						}
					}
				}

			case !v.IsSecret && exists && cur.isSecret:
				if cur.secretID != nil {
					_, _ = tx.ExecContext(ctx, `DELETE FROM secrets WHERE id = $1`, *cur.secretID)
				}
				_, err = tx.ExecContext(ctx, `
					UPDATE deployment_env SET value = $1, secret_id = NULL, is_secret = 0
					WHERE deployment_id = $2 AND key = $3`, v.Value, deploymentID, v.Key)
				if err != nil {
					return wrapSQLite("store: convert from secret env", err)
				}

			default:
				_, err = tx.ExecContext(ctx, `
					INSERT INTO deployment_env (deployment_id, key, value, secret_id, is_secret)
					VALUES ($1, $2, $3, NULL, 0)
					ON CONFLICT (deployment_id, key) DO UPDATE SET value = excluded.value, secret_id = NULL, is_secret = 0`,
					deploymentID, v.Key, v.Value)
				if err != nil {
					return wrapSQLite("store: upsert env", err)
				}
			}
		}

		for k, cur := range existing {
			if !kept[k] {
				_, _ = tx.ExecContext(ctx, `DELETE FROM deployment_env WHERE deployment_id = $1 AND key = $2`, deploymentID, k)
				if cur.secretID != nil {
					_, _ = tx.ExecContext(ctx, `DELETE FROM secrets WHERE id = $1`, *cur.secretID)
				}
			}
		}

		now := time.Now().UTC()
		_, _ = tx.ExecContext(ctx, `UPDATE deployments SET updated_at = $1 WHERE id = $2`, now, deploymentID)
		return nil
	})
}

func (s *sqliteStore) ListDeploymentEnv(ctx context.Context, deploymentID string) ([]EnvVar, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT key, value, is_secret FROM deployment_env WHERE deployment_id = $1 ORDER BY key`, deploymentID)
	if err != nil {
		return nil, wrapSQLite("store: list deployment env", err)
	}
	defer rows.Close()

	var vars []EnvVar
	for rows.Next() {
		var v EnvVar
		if err := rows.Scan(&v.Key, &v.Value, &v.IsSecret); err != nil {
			return nil, wrapSQLite("store: scan deployment env", err)
		}
		vars = append(vars, v)
	}
	return vars, rows.Err()
}

func (s *sqliteStore) ResolveDeploymentEnv(ctx context.Context, sealer Sealer, deploymentID string) (map[string]string, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT key, value, secret_id, is_secret FROM deployment_env WHERE deployment_id = $1`, deploymentID)
	if err != nil {
		return nil, wrapSQLite("store: resolve env", err)
	}
	defer rows.Close()

	env := make(map[string]string)
	for rows.Next() {
		var k, val string
		var sid *string
		var isSec bool
		if err := rows.Scan(&k, &val, &sid, &isSec); err != nil {
			return nil, wrapSQLite("store: scan resolve env", err)
		}
		if isSec && sid != nil {
			dec, err := s.openSecret(ctx, sealer, sid)
			if err != nil {
				return nil, fmt.Errorf("decrypt env %s: %w", k, err)
			}
			env[k] = dec
		} else {
			env[k] = val
		}
	}
	return env, rows.Err()
}

func (s *sqliteStore) SecretEnvValues(ctx context.Context, sealer Sealer, deploymentID string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT secret_id FROM deployment_env WHERE deployment_id = $1 AND is_secret = 1 AND secret_id IS NOT NULL`, deploymentID)
	if err != nil {
		return nil, wrapSQLite("store: read secret env", err)
	}
	defer rows.Close()

	var values []string
	for rows.Next() {
		var sid string
		if err := rows.Scan(&sid); err != nil {
			return nil, wrapSQLite("store: scan secret env", err)
		}
		dec, err := s.openSecret(ctx, sealer, &sid)
		if err != nil {
			continue
		}
		if dec != "" {
			values = append(values, dec)
		}
	}
	return values, rows.Err()
}

func (s *sqliteStore) DeploymentWebhookSecret(ctx context.Context, sealer Sealer, deployment *Deployment) (string, error) {
	return s.openSecret(ctx, sealer, deployment.WebhookSecretID)
}

func scanDeployment(r interface{ Scan(...any) error }) (*Deployment, error) {
	var d Deployment
	err := r.Scan(
		&d.ID, &d.ServerID, &d.Name, &d.Slug, &d.SourceType,
		&d.RepoURL, &d.GitRef, &d.GitCredentialID,
		&d.DockerfilePath, &d.BuildContext, &d.ComposePath, &d.ComposeContent, &d.ImageRef,
		&d.BuildStrategy, &d.RegistryID, &d.ImageName,
		&d.Workdir, &d.HostPort, &d.ContainerPort,
		&d.WebhookSecretID, &d.Status, &d.CurrentRunID, &d.CreatedBy,
		&d.CreatedAt, &d.UpdatedAt,
	)
	if err != nil {
		return nil, wrapSQLite("store: scan deployment", err)
	}
	return &d, nil
}

// --- Runs & Logs ---

func (s *sqliteStore) EnqueueRun(ctx context.Context, deploymentID string, trigger RunTrigger, triggeredBy *string) (*Run, error) {
	return s.EnqueueRunWithParams(ctx, EnqueueRunParams{
		DeploymentID: deploymentID,
		Trigger:      trigger,
		TriggeredBy:  triggeredBy,
	})
}

func (s *sqliteStore) EnqueueRunWithParams(ctx context.Context, params EnqueueRunParams) (*Run, error) {
	var run *Run
	err := s.tx(ctx, func(tx *sql.Tx) error {
		var nextNumber int
		err := tx.QueryRowContext(ctx, `
			SELECT COALESCE(max(number), 0) + 1 FROM deployment_runs WHERE deployment_id = $1`, params.DeploymentID).Scan(&nextNumber)
		if err != nil {
			return wrapSQLite("store: next run number", err)
		}

		id := newUUID()
		now := time.Now().UTC()
		_, err = tx.ExecContext(ctx, `
			INSERT INTO deployment_runs (
				id, deployment_id, number, trigger, status, commit_sha, image_ref, error, triggered_by, queued_at
			)
			VALUES ($1, $2, $3, $4, 'queued', $5, $6, '', $7, $8)`,
			id, params.DeploymentID, nextNumber, params.Trigger, params.CommitSHA, params.ImageRef, nullablePtr(params.TriggeredBy), now)
		if err != nil {
			return wrapSQLite("store: enqueue run", err)
		}

		run = &Run{
			ID:           id,
			DeploymentID: params.DeploymentID,
			Number:       nextNumber,
			Trigger:      params.Trigger,
			Status:       RunQueued,
			CommitSHA:    params.CommitSHA,
			ImageRef:     params.ImageRef,
			TriggeredBy:  params.TriggeredBy,
			QueuedAt:     now,
		}
		return nil
	})
	if err == nil {
		s.pubsub.notify()
	}
	return run, err
}

func (s *sqliteStore) ClaimRun(ctx context.Context, workerID string) (*Run, error) {
	var run *Run
	err := s.tx(ctx, func(tx *sql.Tx) error {
		var id string
		err := tx.QueryRowContext(ctx, `
			SELECT id FROM deployment_runs
			WHERE status = 'queued'
			ORDER BY queued_at ASC
			LIMIT 1`).Scan(&id)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrNoRuns
			}
			return wrapSQLite("store: claim run", err)
		}

		now := time.Now().UTC()
		_, err = tx.ExecContext(ctx, `
			UPDATE deployment_runs
			SET status = 'running', started_at = $1, locked_at = $1, locked_by = $2
			WHERE id = $3`, now, workerID, id)
		if err != nil {
			return wrapSQLite("store: claim run", err)
		}

		row := tx.QueryRowContext(ctx, `SELECT `+runColumns+` FROM deployment_runs WHERE id = $1`, id)
		run, err = scanRun(row)
		return err
	})
	return run, err
}

func (s *sqliteStore) ReleaseStaleRuns(ctx context.Context, olderThan time.Duration) (int64, error) {
	cutoff := time.Now().UTC().Add(-olderThan)
	res, err := s.db.ExecContext(ctx, `
		UPDATE deployment_runs
		SET status = 'failed',
		    error = 'The worker running this deploy stopped unexpectedly.',
		    finished_at = CURRENT_TIMESTAMP
		WHERE status = 'running' AND locked_at < $1`, cutoff)
	if err != nil {
		return 0, wrapSQLite("store: release stale runs", err)
	}
	return res.RowsAffected()
}

func (s *sqliteStore) FinishRun(ctx context.Context, runID string, status RunStatus, imageRef, commitSHA, failure string) error {
	now := time.Now().UTC()
	return s.tx(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `
			UPDATE deployment_runs
			SET status = $1,
			    finished_at = $2,
			    image_ref = CASE WHEN $3 <> '' THEN $3 ELSE image_ref END,
			    commit_sha = CASE WHEN $4 <> '' THEN $4 ELSE commit_sha END,
			    error = $5
			WHERE id = $6`, status, now, imageRef, commitSHA, failure, runID)
		if err != nil {
			return wrapSQLite("store: finish run", err)
		}

		var depStatus DeploymentStatus
		switch status {
		case RunSucceeded:
			depStatus = DeploymentRunning
		case RunFailed, RunCancelled:
			depStatus = DeploymentFailed
		default:
			depStatus = DeploymentRunning
		}

		_, err = tx.ExecContext(ctx, `
			UPDATE deployments
			SET status = $1,
			    current_run_id = $2,
			    image_ref = CASE WHEN $3 <> '' THEN $3 ELSE image_ref END,
			    updated_at = $4
			WHERE id = (SELECT deployment_id FROM deployment_runs WHERE id = $2)`,
			depStatus, runID, imageRef, now)
		return wrapSQLite("store: finish run update deployment", err)
	})
}

func (s *sqliteStore) RunByID(ctx context.Context, id string) (*Run, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+runColumns+` FROM deployment_runs WHERE id = $1`, id)
	return scanRun(row)
}

func (s *sqliteStore) ListRuns(ctx context.Context, deploymentID string, limit int) ([]Run, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT `+runColumns+` FROM deployment_runs
		WHERE deployment_id = $1 ORDER BY number DESC LIMIT $2`, deploymentID, limit)
	if err != nil {
		return nil, wrapSQLite("store: list runs", err)
	}
	defer rows.Close()

	var runs []Run
	for rows.Next() {
		r, err := scanRun(rows)
		if err != nil {
			return nil, err
		}
		runs = append(runs, *r)
	}
	return runs, rows.Err()
}

func (s *sqliteStore) LastSuccessfulRun(ctx context.Context, deploymentID, excludeRunID string) (*Run, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT `+runColumns+` FROM deployment_runs
		WHERE deployment_id = $1 AND status = 'succeeded'
		  AND image_ref <> '' AND ($2 = '' OR id <> $2)
		ORDER BY number DESC LIMIT 1`, deploymentID, excludeRunID)
	return scanRun(row)
}

func (s *sqliteStore) CancelRun(ctx context.Context, runID string) error {
	now := time.Now().UTC()
	res, err := s.db.ExecContext(ctx, `
		UPDATE deployment_runs
		SET status = 'cancelled', finished_at = $1
		WHERE id = $2 AND status = 'queued'`, now, runID)
	if err != nil {
		return wrapSQLite("store: cancel run", err)
	}
	rowsAffected, _ := res.RowsAffected()
	if rowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *sqliteStore) AppendLogs(ctx context.Context, runID string, startSeq int64, entries []LogEntry) error {
	if len(entries) == 0 {
		return nil
	}
	return s.tx(ctx, func(tx *sql.Tx) error {
		stmt, err := tx.PrepareContext(ctx, `
			INSERT INTO deployment_logs (run_id, seq, stream, line, ts)
			VALUES ($1, $2, $3, $4, $5)
			ON CONFLICT (run_id, seq) DO NOTHING`)
		if err != nil {
			return wrapSQLite("store: prepare append logs", err)
		}
		defer stmt.Close()

		for i, entry := range entries {
			ts := entry.TS
			if ts.IsZero() {
				ts = time.Now().UTC()
			}
			if _, err := stmt.ExecContext(ctx, runID, startSeq+int64(i), entry.Stream, entry.Line, ts); err != nil {
				return wrapSQLite("store: append log entry", err)
			}
		}
		return nil
	})
}

func (s *sqliteStore) ListLogs(ctx context.Context, runID string, afterSeq int64, limit int) ([]LogEntry, error) {
	if limit <= 0 || limit > 5000 {
		limit = 2000
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT seq, stream, line, ts FROM deployment_logs
		WHERE run_id = $1 AND seq > $2
		ORDER BY seq ASC LIMIT $3`, runID, afterSeq, limit)
	if err != nil {
		return nil, wrapSQLite("store: list logs", err)
	}
	defer rows.Close()

	var logs []LogEntry
	for rows.Next() {
		var l LogEntry
		if err := rows.Scan(&l.Seq, &l.Stream, &l.Line, &l.TS); err != nil {
			return nil, wrapSQLite("store: scan log", err)
		}
		logs = append(logs, l)
	}
	return logs, rows.Err()
}

func (s *sqliteStore) PruneRunHistory(ctx context.Context, keepPerDeployment int) (int64, error) {
	res, err := s.db.ExecContext(ctx, `
		DELETE FROM deployment_runs
		WHERE status <> 'running'
		  AND id NOT IN (SELECT current_run_id FROM deployments WHERE current_run_id IS NOT NULL)
		  AND number <= (
			SELECT max(number) - $1 FROM deployment_runs dr WHERE dr.deployment_id = deployment_runs.deployment_id
		  )`, keepPerDeployment)
	if err != nil {
		return 0, wrapSQLite("store: prune runs", err)
	}
	return res.RowsAffected()
}

func scanRun(r interface{ Scan(...any) error }) (*Run, error) {
	var run Run
	err := r.Scan(
		&run.ID, &run.DeploymentID, &run.Number, &run.Trigger, &run.Status,
		&run.CommitSHA, &run.ImageRef, &run.Error, &run.TriggeredBy,
		&run.QueuedAt, &run.StartedAt, &run.FinishedAt, &run.LockedAt, &run.LockedBy,
	)
	if err != nil {
		return nil, wrapSQLite("store: scan run", err)
	}
	return &run, nil
}

// --- Domains ---

func (s *sqliteStore) CreateDomain(ctx context.Context, in NewDomain) (*Domain, error) {
	if in.SSLMode == "" {
		in.SSLMode = DomainSSLNone
	}
	if in.Status == "" {
		in.Status = DomainStatusPending
	}
	hostname := strings.ToLower(strings.TrimSpace(in.Hostname))

	id := newUUID()
	now := time.Now().UTC()
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO domains (
			id, deployment_id, server_id, hostname, upstream_port,
			ssl_mode, websocket, config_rendered, status, status_message, created_by, created_at, updated_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $12)`,
		id, in.DeploymentID, in.ServerID, hostname, in.UpstreamPort,
		in.SSLMode, in.WebSocket, in.ConfigRendered, in.Status, in.StatusMessage, nullable(in.CreatedBy), now)
	if err != nil {
		return nil, wrapSQLite("store: create domain", err)
	}

	return &Domain{
		ID:             id,
		DeploymentID:   in.DeploymentID,
		ServerID:       in.ServerID,
		Hostname:       hostname,
		UpstreamPort:   in.UpstreamPort,
		SSLMode:        in.SSLMode,
		WebSocket:      in.WebSocket,
		ConfigRendered: in.ConfigRendered,
		Status:         in.Status,
		StatusMessage:  in.StatusMessage,
		CreatedBy:      nullable(in.CreatedBy),
		CreatedAt:      now,
		UpdatedAt:      now,
	}, nil
}

func (s *sqliteStore) DomainByID(ctx context.Context, id string) (*Domain, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+domainColumns+` FROM domains WHERE id = $1`, id)
	return scanDomain(row)
}

func (s *sqliteStore) DomainByHostname(ctx context.Context, hostname string) (*Domain, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+domainColumns+` FROM domains WHERE hostname = $1`, strings.ToLower(strings.TrimSpace(hostname)))
	return scanDomain(row)
}

func (s *sqliteStore) ListDomains(ctx context.Context, filter DomainFilter) ([]Domain, error) {
	query := `SELECT ` + domainColumns + ` FROM domains WHERE 1=1`
	var args []any
	argIdx := 1

	if filter.ServerID != nil && *filter.ServerID != "" {
		query += fmt.Sprintf(` AND server_id = $%d`, argIdx)
		args = append(args, *filter.ServerID)
		argIdx++
	}
	if filter.DeploymentID != nil && *filter.DeploymentID != "" {
		query += fmt.Sprintf(` AND deployment_id = $%d`, argIdx)
		args = append(args, *filter.DeploymentID)
		argIdx++
	}
	query += ` ORDER BY hostname ASC`

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, wrapSQLite("store: list domains", err)
	}
	defer rows.Close()

	var domains []Domain
	for rows.Next() {
		d, err := scanDomain(rows)
		if err != nil {
			return nil, err
		}
		domains = append(domains, *d)
	}
	return domains, rows.Err()
}

func (s *sqliteStore) ListDomainsByServer(ctx context.Context, serverID string) ([]Domain, error) {
	return s.ListDomains(ctx, DomainFilter{ServerID: &serverID})
}

func (s *sqliteStore) ListDomainsByDeployment(ctx context.Context, deploymentID string) ([]Domain, error) {
	return s.ListDomains(ctx, DomainFilter{DeploymentID: &deploymentID})
}

func (s *sqliteStore) UpdateDomainStatus(ctx context.Context, id string, params UpdateDomainParams) error {
	now := time.Now().UTC()
	query := `UPDATE domains SET status = $1, status_message = $2, updated_at = $3`
	args := []any{params.Status, params.StatusMessage, now}
	idx := 4

	if params.ConfigRendered != nil {
		query += fmt.Sprintf(`, config_rendered = $%d`, idx)
		args = append(args, *params.ConfigRendered)
		idx++
	}
	if params.CertExpiresAt != nil {
		query += fmt.Sprintf(`, cert_expires_at = $%d`, idx)
		args = append(args, params.CertExpiresAt.UTC())
		idx++
	}

	query += fmt.Sprintf(` WHERE id = $%d`, idx)
	args = append(args, id)

	res, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return wrapSQLite("store: update domain status", err)
	}
	rowsAffected, _ := res.RowsAffected()
	if rowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *sqliteStore) DeleteDomain(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM domains WHERE id = $1`, id)
	if err != nil {
		return wrapSQLite("store: delete domain", err)
	}
	rowsAffected, _ := res.RowsAffected()
	if rowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func scanDomain(r interface{ Scan(...any) error }) (*Domain, error) {
	var d Domain
	err := r.Scan(
		&d.ID, &d.DeploymentID, &d.ServerID, &d.Hostname, &d.UpstreamPort,
		&d.SSLMode, &d.WebSocket, &d.ConfigRendered, &d.CertExpiresAt, &d.Status,
		&d.StatusMessage, &d.CreatedBy, &d.CreatedAt, &d.UpdatedAt,
	)
	if err != nil {
		return nil, wrapSQLite("store: scan domain", err)
	}
	return &d, nil
}

func nullablePtr(s *string) *string {
	if s == nil || *s == "" {
		return nil
	}
	return s
}
