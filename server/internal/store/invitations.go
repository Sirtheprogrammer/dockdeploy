package store

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/sirtheprogrammer/docker-deployments/server/internal/auth"
)

type Invitation struct {
	ID             string     `db:"id"          json:"id"`
	Email          string     `db:"email"       json:"email"`
	Name           string     `db:"name"        json:"name"`
	Role           auth.Role  `db:"role"        json:"role"`
	InvitedBy      *string    `db:"invited_by"  json:"invited_by"`
	ExpiresAt      time.Time  `db:"expires_at"  json:"expires_at"`
	AcceptedAt     *time.Time `db:"accepted_at" json:"accepted_at"`
	AcceptedUserID *string    `db:"accepted_user_id" json:"accepted_user_id"`
	CreatedAt      time.Time  `db:"created_at"  json:"created_at"`
}

func (i *Invitation) Pending() bool {
	return i.AcceptedAt == nil && i.ExpiresAt.After(time.Now())
}

const invitationColumns = `id, email, name, role, invited_by, expires_at, accepted_at, accepted_user_id, created_at`

// CreateInvitation issues a fresh invite, replacing any outstanding one for the
// same address. Without the replace, re-inviting someone would leave two valid
// links and no way to tell which was revoked.
func (s *Store) CreateInvitation(ctx context.Context, email, name string, role auth.Role, invitedBy string, tokenHash []byte, expiresAt time.Time) (*Invitation, error) {
	if s.sqlite != nil {
		return s.sqlite.CreateInvitation(ctx, email, name, role, invitedBy, tokenHash, expiresAt)
	}
	var invitation *Invitation
	err := s.tx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx,
			`DELETE FROM invitations WHERE email = $1 AND accepted_at IS NULL`,
			NormaliseEmail(email)); err != nil {
			return wrap("store: clear invitations", err)
		}

		rows, err := tx.Query(ctx, `
			INSERT INTO invitations (email, name, role, invited_by, token_hash, expires_at)
			VALUES ($1, $2, $3, $4, $5, $6)
			RETURNING `+invitationColumns,
			NormaliseEmail(email), name, role, invitedBy, tokenHash, expiresAt)
		if err != nil {
			return wrap("store: create invitation", err)
		}
		created, err := pgx.CollectExactlyOneRow(rows, pgx.RowToStructByName[Invitation])
		if err != nil {
			return wrap("store: create invitation", err)
		}
		invitation = &created
		return nil
	})
	return invitation, err
}

// InvitationByTokenHash returns a still-usable invitation, filtering spent and
// expired ones in SQL.
func (s *Store) InvitationByTokenHash(ctx context.Context, tokenHash []byte) (*Invitation, error) {
	if s.sqlite != nil {
		return s.sqlite.InvitationByTokenHash(ctx, tokenHash)
	}
	rows, err := s.pool.Query(ctx, `
		SELECT `+invitationColumns+` FROM invitations
		WHERE token_hash = $1 AND accepted_at IS NULL AND expires_at > now()`, tokenHash)
	if err != nil {
		return nil, wrap("store: invitation by token", err)
	}
	invitation, err := pgx.CollectExactlyOneRow(rows, pgx.RowToStructByName[Invitation])
	if err != nil {
		return nil, wrap("store: invitation by token", err)
	}
	return &invitation, nil
}

func (s *Store) ListInvitations(ctx context.Context) ([]Invitation, error) {
	if s.sqlite != nil {
		return s.sqlite.ListInvitations(ctx)
	}
	rows, err := s.pool.Query(ctx, `
		SELECT `+invitationColumns+` FROM invitations
		WHERE accepted_at IS NULL AND expires_at > now()
		ORDER BY created_at DESC`)
	if err != nil {
		return nil, wrap("store: list invitations", err)
	}
	invitations, err := pgx.CollectRows(rows, pgx.RowToStructByName[Invitation])
	return invitations, wrap("store: list invitations", err)
}

// AcceptInvitation creates the account and spends the invitation atomically, so
// a link can never be redeemed twice.
func (s *Store) AcceptInvitation(ctx context.Context, invitationID, name, passwordHash string) (*User, error) {
	if s.sqlite != nil {
		return s.sqlite.AcceptInvitation(ctx, invitationID, name, passwordHash)
	}
	var user *User
	err := s.tx(ctx, func(tx pgx.Tx) error {
		var email string
		var role auth.Role
		err := tx.QueryRow(ctx, `
			UPDATE invitations SET accepted_at = now()
			WHERE id = $1 AND accepted_at IS NULL AND expires_at > now()
			RETURNING email, role`, invitationID).Scan(&email, &role)
		if err != nil {
			return wrap("store: spend invitation", err)
		}

		rows, err := tx.Query(ctx, `
			INSERT INTO users (email, name, password_hash, role)
			VALUES ($1, $2, $3, $4)
			RETURNING `+userColumns, email, name, passwordHash, role)
		if err != nil {
			return wrap("store: create invited user", err)
		}
		created, err := pgx.CollectExactlyOneRow(rows, pgx.RowToStructByName[User])
		if err != nil {
			return wrap("store: create invited user", err)
		}

		if _, err := tx.Exec(ctx,
			`UPDATE invitations SET accepted_user_id = $2 WHERE id = $1`,
			invitationID, created.ID); err != nil {
			return wrap("store: link invitation", err)
		}
		user = &created
		return nil
	})
	return user, err
}

func (s *Store) DeleteInvitation(ctx context.Context, id string) error {
	if s.sqlite != nil {
		return s.sqlite.DeleteInvitation(ctx, id)
	}
	tag, err := s.pool.Exec(ctx, `DELETE FROM invitations WHERE id = $1`, id)
	if err != nil {
		return wrap("store: delete invitation", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
