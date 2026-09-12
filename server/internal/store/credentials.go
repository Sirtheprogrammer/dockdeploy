package store

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/sirtheprogrammer/docker-deployments/server/internal/gitx"
)

// Registries and git credentials follow the same shape as servers: the row
// holds a pointer into the secrets table, sealing happens inside the store so
// there is one path from plaintext to storage, and the secret is deleted with
// its owner in the correct order.

type Registry struct {
	ID       string  `db:"id"        json:"id"`
	Name     string  `db:"name"      json:"name"`
	URL      string  `db:"url"       json:"url"`
	Username string  `db:"username"  json:"username"`
	SecretID *string `db:"secret_id" json:"-"`

	CreatedBy *string   `db:"created_by" json:"created_by"`
	CreatedAt time.Time `db:"created_at" json:"created_at"`
	UpdatedAt time.Time `db:"updated_at" json:"updated_at"`
}

const registryColumns = `id, name, url, username, secret_id, created_by, created_at, updated_at`

func (s *Store) CreateRegistry(ctx context.Context, sealer Sealer, name, url, username, password, createdBy string) (*Registry, error) {
	if s.sqlite != nil {
		return s.sqlite.CreateRegistry(ctx, sealer, name, url, username, password, createdBy)
	}
	var registry *Registry

	err := s.tx(ctx, func(tx pgx.Tx) error {
		secretID, err := insertSecret(ctx, tx, sealer, KindRegistryToken, password)
		if err != nil {
			return err
		}

		rows, err := tx.Query(ctx, `
			INSERT INTO registries (name, url, username, secret_id, created_by)
			VALUES ($1, $2, $3, $4, $5)
			RETURNING `+registryColumns, name, url, username, secretID, nullable(createdBy))
		if err != nil {
			return wrap("store: create registry", err)
		}
		created, err := pgx.CollectExactlyOneRow(rows, pgx.RowToStructByName[Registry])
		if err != nil {
			return wrap("store: create registry", err)
		}
		registry = &created
		return nil
	})

	return registry, err
}

func (s *Store) ListRegistries(ctx context.Context) ([]Registry, error) {
	if s.sqlite != nil {
		return s.sqlite.ListRegistries(ctx)
	}
	rows, err := s.pool.Query(ctx, `SELECT `+registryColumns+` FROM registries ORDER BY name`)
	if err != nil {
		return nil, wrap("store: list registries", err)
	}
	registries, err := pgx.CollectRows(rows, pgx.RowToStructByName[Registry])
	return registries, wrap("store: list registries", err)
}

func (s *Store) RegistryByID(ctx context.Context, id string) (*Registry, error) {
	if s.sqlite != nil {
		return s.sqlite.RegistryByID(ctx, id)
	}
	rows, err := s.pool.Query(ctx, `SELECT `+registryColumns+` FROM registries WHERE id = $1`, id)
	if err != nil {
		return nil, wrap("store: registry by id", err)
	}
	registry, err := pgx.CollectExactlyOneRow(rows, pgx.RowToStructByName[Registry])
	if err != nil {
		return nil, wrap("store: registry by id", err)
	}
	return &registry, nil
}

// RegistryPassword decrypts a registry password for immediate use. The result
// is a credential: it goes into a Docker auth header or a stdin-fed
// `docker login --password-stdin`, never onto a command line.
func (s *Store) RegistryPassword(ctx context.Context, sealer Sealer, registry *Registry) (string, error) {
	if s.sqlite != nil {
		return s.sqlite.RegistryPassword(ctx, sealer, registry)
	}
	return s.openSecret(ctx, sealer, registry.SecretID)
}

func (s *Store) DeleteRegistry(ctx context.Context, id string) error {
	if s.sqlite != nil {
		return s.sqlite.DeleteRegistry(ctx, id)
	}
	return s.deleteWithSecret(ctx, "registries", id, "secret_id")
}

// --- git credentials -----------------------------------------------------

type GitCredential struct {
	ID       string    `db:"id"        json:"id"`
	Name     string    `db:"name"      json:"name"`
	Provider string    `db:"provider"  json:"provider"`
	Kind     gitx.Kind `db:"kind"      json:"kind"`
	Username string    `db:"username"  json:"username"`
	SecretID *string   `db:"secret_id" json:"-"`

	CreatedBy *string   `db:"created_by" json:"created_by"`
	CreatedAt time.Time `db:"created_at" json:"created_at"`
	UpdatedAt time.Time `db:"updated_at" json:"updated_at"`
}

const gitCredentialColumns = `id, name, provider, kind, username, secret_id, created_by, created_at, updated_at`

func (s *Store) CreateGitCredential(ctx context.Context, sealer Sealer, name string, kind gitx.Kind, username, secret, createdBy string) (*GitCredential, error) {
	if s.sqlite != nil {
		return s.sqlite.CreateGitCredential(ctx, sealer, name, kind, username, secret, createdBy)
	}
	var credential *GitCredential

	err := s.tx(ctx, func(tx pgx.Tx) error {
		secretID, err := insertSecret(ctx, tx, sealer, KindGitToken, secret)
		if err != nil {
			return err
		}

		rows, err := tx.Query(ctx, `
			INSERT INTO git_credentials (name, kind, username, secret_id, created_by)
			VALUES ($1, $2, $3, $4, $5)
			RETURNING `+gitCredentialColumns, name, kind, username, secretID, nullable(createdBy))
		if err != nil {
			return wrap("store: create git credential", err)
		}
		created, err := pgx.CollectExactlyOneRow(rows, pgx.RowToStructByName[GitCredential])
		if err != nil {
			return wrap("store: create git credential", err)
		}
		credential = &created
		return nil
	})

	return credential, err
}

func (s *Store) ListGitCredentials(ctx context.Context) ([]GitCredential, error) {
	if s.sqlite != nil {
		return s.sqlite.ListGitCredentials(ctx)
	}
	rows, err := s.pool.Query(ctx, `SELECT `+gitCredentialColumns+` FROM git_credentials ORDER BY name`)
	if err != nil {
		return nil, wrap("store: list git credentials", err)
	}
	credentials, err := pgx.CollectRows(rows, pgx.RowToStructByName[GitCredential])
	return credentials, wrap("store: list git credentials", err)
}

func (s *Store) GitCredentialByID(ctx context.Context, id string) (*GitCredential, error) {
	if s.sqlite != nil {
		return s.sqlite.GitCredentialByID(ctx, id)
	}
	rows, err := s.pool.Query(ctx, `SELECT `+gitCredentialColumns+` FROM git_credentials WHERE id = $1`, id)
	if err != nil {
		return nil, wrap("store: git credential by id", err)
	}
	credential, err := pgx.CollectExactlyOneRow(rows, pgx.RowToStructByName[GitCredential])
	if err != nil {
		return nil, wrap("store: git credential by id", err)
	}
	return &credential, nil
}

// ResolveGitCredential decrypts a credential for one clone. A nil id yields
// nil, which is the normal case for a public repository.
func (s *Store) ResolveGitCredential(ctx context.Context, sealer Sealer, id *string) (*gitx.Credential, error) {
	if s.sqlite != nil {
		return s.sqlite.ResolveGitCredential(ctx, sealer, id)
	}
	if id == nil {
		return nil, nil
	}

	credential, err := s.GitCredentialByID(ctx, *id)
	if err != nil {
		return nil, err
	}
	secret, err := s.openSecret(ctx, sealer, credential.SecretID)
	if err != nil {
		return nil, err
	}
	return &gitx.Credential{
		Kind:     credential.Kind,
		Username: credential.Username,
		Secret:   secret,
	}, nil
}

func (s *Store) DeleteGitCredential(ctx context.Context, id string) error {
	if s.sqlite != nil {
		return s.sqlite.DeleteGitCredential(ctx, id)
	}
	return s.deleteWithSecret(ctx, "git_credentials", id, "secret_id")
}

// deleteWithSecret removes a row and the secret it owned.
//
// The row references the secret, so it has to go first; doing both in one
// transaction is what stops a failure halfway from leaving an orphaned
// encrypted row that nothing points at and nothing can delete.
func (s *Store) deleteWithSecret(ctx context.Context, table, id, secretColumn string) error {
	return s.tx(ctx, func(tx pgx.Tx) error {
		var secretID *string
		// table and secretColumn are package-internal constants, never user
		// input, so interpolating them here cannot be influenced by a request.
		err := tx.QueryRow(ctx,
			`SELECT `+secretColumn+` FROM `+table+` WHERE id = $1`, id).Scan(&secretID)
		if err != nil {
			return wrap("store: delete "+table, err)
		}

		tag, err := tx.Exec(ctx, `DELETE FROM `+table+` WHERE id = $1`, id)
		if err != nil {
			return wrap("store: delete "+table, err)
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}

		if secretID != nil {
			if _, err := tx.Exec(ctx, `DELETE FROM secrets WHERE id = $1`, *secretID); err != nil {
				return wrap("store: delete "+table+" secret", err)
			}
		}
		return nil
	})
}
