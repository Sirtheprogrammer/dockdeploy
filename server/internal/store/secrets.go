package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/sirtheprogrammer/docker-deployments/server/internal/secrets"
)

// Sealer is the encryption dependency the store needs. It is an interface so
// tests can substitute one without an encryption key, and so this package does
// not reach for a global.
type Sealer interface {
	SealString(plaintext string, aad []byte) (nonce, ciphertext []byte, err error)
	OpenString(nonce, ciphertext, aad []byte) (string, error)
}

// Re-exported so callers do not need to import the secrets package just to
// name a credential kind.
const (
	KindSSHPassword   = secrets.KindSSHPassword
	KindSSHPrivateKey = secrets.KindSSHPrivateKey
	KindSSHPassphrase = secrets.KindSSHPassphrase
	KindSudoPassword  = secrets.KindSudoPassword
	KindGitToken      = secrets.KindGitToken
	KindRegistryToken = secrets.KindRegistryToken
	KindDeploymentEnv = secrets.KindDeploymentEnv
	KindWebhookSecret = secrets.KindWebhookSecret
	KindTOTPSecret    = secrets.KindTOTPSecret
	KindTOTPRecovery  = secrets.KindTOTPRecovery
	KindAIToken       = secrets.KindAIToken
)

// insertSecret seals a value and stores it, returning the new row id.
//
// The id is generated first and used as the GCM associated data, so the
// ciphertext is cryptographically bound to its row: moving it to a different
// secret makes it fail to decrypt rather than silently authenticating as some
// other credential.
func insertSecret(ctx context.Context, tx pgx.Tx, sealer Sealer, kind secrets.Kind, plaintext string) (*string, error) {
	var id string
	if err := tx.QueryRow(ctx, `SELECT gen_random_uuid()`).Scan(&id); err != nil {
		return nil, wrap("store: allocate secret id", err)
	}

	nonce, ciphertext, err := sealer.SealString(plaintext, []byte(id))
	if err != nil {
		return nil, fmt.Errorf("store: seal secret: %w", err)
	}

	if _, err := tx.Exec(ctx,
		`INSERT INTO secrets (id, kind, nonce, ciphertext) VALUES ($1, $2, $3, $4)`,
		id, kind, nonce, ciphertext); err != nil {
		return nil, wrap("store: insert secret", err)
	}
	return &id, nil
}

// openSecret decrypts a secret by id. A nil id yields an empty string, which
// is the normal case for optional credentials like a sudo password.
func (s *Store) openSecret(ctx context.Context, sealer Sealer, id *string) (string, error) {
	if s.sqlite != nil {
		return s.sqlite.openSecret(ctx, sealer, id)
	}
	if id == nil {
		return "", nil
	}

	var nonce, ciphertext []byte
	err := s.pool.QueryRow(ctx,
		`SELECT nonce, ciphertext FROM secrets WHERE id = $1`, *id).Scan(&nonce, &ciphertext)
	if err != nil {
		return "", wrap("store: read secret", err)
	}

	plaintext, err := sealer.OpenString(nonce, ciphertext, []byte(*id))
	if err != nil {
		// Almost always a wrong or rotated APP_ENCRYPTION_KEY. Say so, because
		// the alternative reading -- a tampered database -- needs different
		// action and the operator has to be able to tell them apart.
		return "", fmt.Errorf("store: decrypt secret %s: %w "+
			"(this usually means APP_ENCRYPTION_KEY changed since the credential was saved)", *id, err)
	}
	return plaintext, nil
}

// ReplaceSecret rotates the value behind an existing secret id, keeping the id
// stable so referencing rows need no update.
func (s *Store) ReplaceSecret(ctx context.Context, sealer Sealer, id, plaintext string) error {
	if s.sqlite != nil {
		return s.sqlite.ReplaceSecret(ctx, sealer, id, plaintext)
	}
	nonce, ciphertext, err := sealer.SealString(plaintext, []byte(id))
	if err != nil {
		return fmt.Errorf("store: seal secret: %w", err)
	}
	tag, err := s.pool.Exec(ctx,
		`UPDATE secrets SET nonce = $2, ciphertext = $3 WHERE id = $1`, id, nonce, ciphertext)
	if err != nil {
		return wrap("store: replace secret", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
