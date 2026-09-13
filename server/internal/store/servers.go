package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/sirtheprogrammer/docker-deployments/server/internal/sshx"
)

// ServerStatus is the last known reachability of a server.
type ServerStatus string

const (
	// ServerUnknown means it has not been contacted since it was added.
	ServerUnknown ServerStatus = "unknown"
	ServerOnline  ServerStatus = "online"
	ServerOffline ServerStatus = "offline"
	// ServerUnauthorized covers rejected credentials and, more importantly, a
	// host key that no longer matches. Both need a human, not a retry.
	ServerUnauthorized ServerStatus = "unauthorized"
)

// ServerPermission is a per-server grant for a member.
type ServerPermission string

const (
	ServerRead    ServerPermission = "read"
	ServerOperate ServerPermission = "operate"
)

// Server is a machine the platform manages. Secret columns are pointers into
// the secrets table and are never part of any API response.
type Server struct {
	ID         string          `db:"id"          json:"id"`
	Name       string          `db:"name"        json:"name"`
	Host       string          `db:"host"        json:"host"`
	Port       int             `db:"port"        json:"port"`
	Username   string          `db:"username"    json:"username"`
	AuthMethod sshx.AuthMethod `db:"auth_method" json:"auth_method"`

	SecretID             *string `db:"secret_id"               json:"-"`
	PassphraseSecretID   *string `db:"passphrase_secret_id"    json:"-"`
	SudoPasswordSecretID *string `db:"sudo_password_secret_id" json:"-"`

	HostKeyFingerprint string `db:"host_key_fingerprint" json:"host_key_fingerprint"`
	DockerSocket       string `db:"docker_socket"        json:"docker_socket"`

	Status        ServerStatus    `db:"status"         json:"status"`
	StatusMessage string          `db:"status_message" json:"status_message"`
	Capabilities  json.RawMessage `db:"capabilities"   json:"capabilities"`
	LastSeenAt    *time.Time      `db:"last_seen_at"   json:"last_seen_at"`

	CreatedBy *string   `db:"created_by" json:"created_by"`
	CreatedAt time.Time `db:"created_at" json:"created_at"`
	UpdatedAt time.Time `db:"updated_at" json:"updated_at"`
}

// HasSudoPassword lets the UI show whether a stored sudo password exists
// without ever revealing it.
func (s *Server) HasSudoPassword() bool { return s.SudoPasswordSecretID != nil }

const serverColumns = `id, name, host, port, username, auth_method,
	secret_id, passphrase_secret_id, sudo_password_secret_id,
	host_key_fingerprint, docker_socket,
	status, status_message, capabilities, last_seen_at,
	created_by, created_at, updated_at`

// NewServer carries everything needed to create a server, with credentials
// still in plaintext. The store seals them before they reach the database.
type NewServer struct {
	Name       string
	Host       string
	Port       int
	Username   string
	AuthMethod sshx.AuthMethod

	// Exactly one of Password or PrivateKey is used, per AuthMethod.
	Password   string
	PrivateKey string
	Passphrase string
	// SudoPassword is optional and only needed when sudo prompts.
	SudoPassword string

	HostKeyFingerprint string
	DockerSocket       string
	CreatedBy          string
}

// CreateServer stores a server and seals its credentials in one transaction.
//
// Sealing happens here rather than in the handler so there is exactly one path
// from plaintext to storage, and no way to add a credential column later that
// quietly skips encryption.
func (s *Store) CreateServer(ctx context.Context, sealer Sealer, in NewServer) (*Server, error) {
	if s.sqlite != nil {
		return s.sqlite.CreateServer(ctx, sealer, in)
	}
	var server *Server

	err := s.tx(ctx, func(tx pgx.Tx) error {
		var secretID, passphraseID, sudoID *string

		switch in.AuthMethod {
		case sshx.AuthPassword:
			id, err := insertSecret(ctx, tx, sealer, KindSSHPassword, in.Password)
			if err != nil {
				return err
			}
			secretID = id

		case sshx.AuthKey:
			id, err := insertSecret(ctx, tx, sealer, KindSSHPrivateKey, in.PrivateKey)
			if err != nil {
				return err
			}
			secretID = id

			if in.Passphrase != "" {
				id, err := insertSecret(ctx, tx, sealer, KindSSHPassphrase, in.Passphrase)
				if err != nil {
					return err
				}
				passphraseID = id
			}
		}

		if in.SudoPassword != "" {
			id, err := insertSecret(ctx, tx, sealer, KindSudoPassword, in.SudoPassword)
			if err != nil {
				return err
			}
			sudoID = id
		}

		socket := in.DockerSocket
		if socket == "" {
			socket = "/var/run/docker.sock"
		}
		port := in.Port
		if port == 0 {
			port = 22
		}

		rows, err := tx.Query(ctx, `
			INSERT INTO servers (name, host, port, username, auth_method,
				secret_id, passphrase_secret_id, sudo_password_secret_id,
				host_key_fingerprint, docker_socket, created_by)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
			RETURNING `+serverColumns,
			in.Name, in.Host, port, in.Username, in.AuthMethod,
			secretID, passphraseID, sudoID,
			in.HostKeyFingerprint, socket, nullable(in.CreatedBy))
		if err != nil {
			return wrap("store: create server", err)
		}
		created, err := pgx.CollectExactlyOneRow(rows, pgx.RowToStructByName[Server])
		if err != nil {
			return wrap("store: create server", err)
		}
		server = &created
		return nil
	})

	return server, err
}

// ListServers returns servers visible to a user.
//
// Admins see everything. Everyone else sees only servers they were granted,
// which is why the visibility filter lives in SQL: a handler that forgets to
// apply it would leak the whole fleet.
func (s *Store) ListServers(ctx context.Context, userID string, all bool) ([]Server, error) {
	if s.sqlite != nil {
		return s.sqlite.ListServers(ctx, userID, all)
	}
	rows, err := s.pool.Query(ctx, `
		SELECT `+serverColumns+` FROM servers
		WHERE $1 OR id IN (SELECT server_id FROM server_members WHERE user_id = $2::uuid)
		ORDER BY name`, all, userID)
	if err != nil {
		return nil, wrap("store: list servers", err)
	}
	servers, err := pgx.CollectRows(rows, pgx.RowToStructByName[Server])
	return servers, wrap("store: list servers", err)
}

func (s *Store) ServerByID(ctx context.Context, id string) (*Server, error) {
	if s.sqlite != nil {
		return s.sqlite.ServerByID(ctx, id)
	}
	rows, err := s.pool.Query(ctx, `SELECT `+serverColumns+` FROM servers WHERE id = $1`, id)
	if err != nil {
		return nil, wrap("store: server by id", err)
	}
	server, err := pgx.CollectExactlyOneRow(rows, pgx.RowToStructByName[Server])
	if err != nil {
		return nil, wrap("store: server by id", err)
	}
	return &server, nil
}

// CanAccessServer reports whether a user may act on a server. Admins always
// can; members need an explicit grant.
func (s *Store) CanAccessServer(ctx context.Context, serverID, userID string, isAdmin bool) (bool, error) {
	if s.sqlite != nil {
		return s.sqlite.CanAccessServer(ctx, serverID, userID, isAdmin)
	}
	if isAdmin {
		return true, nil
	}
	var exists bool
	err := s.pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM server_members WHERE server_id = $1 AND user_id = $2)`,
		serverID, userID).Scan(&exists)
	return exists, wrap("store: check server access", err)
}

// ServerCredential is the decrypted material for one connection. It is built
// on demand and must not be cached or logged.
type ServerCredential struct {
	Target     sshx.Target
	Credential sshx.Credential
	// SudoPassword is empty unless one was stored.
	SudoPassword string
}

// ServerCredential decrypts a server's credentials for immediate use.
func (s *Store) ServerCredential(ctx context.Context, sealer Sealer, server *Server) (*ServerCredential, error) {
	if s.sqlite != nil {
		return s.sqlite.ServerCredential(ctx, sealer, server)
	}
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

// UpdateServerStatus records the outcome of a connection attempt.
//
// A nil caps leaves the stored capabilities untouched. That matters because
// this is called on every ordinary request that opens a connection, not just
// after a probe: writing an empty object there would erase what the last probe
// found and leave the dashboard showing nothing.
func (s *Store) UpdateServerStatus(ctx context.Context, id string, status ServerStatus, message string, caps *sshx.Capabilities) error {
	if s.sqlite != nil {
		return s.sqlite.UpdateServerStatus(ctx, id, status, message, caps)
	}
	var encoded []byte
	if caps != nil {
		data, err := json.Marshal(caps)
		if err != nil {
			return fmt.Errorf("store: encode capabilities: %w", err)
		}
		encoded = data
	}

	// last_seen_at only advances on success, so it stays a truthful record of
	// when the server was last actually reachable.
	_, err := s.pool.Exec(ctx, `
		UPDATE servers
		SET status = $2,
		    status_message = $3,
		    capabilities = COALESCE($4::jsonb, capabilities),
		    last_seen_at = CASE WHEN $2 = 'online' THEN now() ELSE last_seen_at END
		WHERE id = $1`, id, status, message, encoded)
	return wrap("store: update server status", err)
}

// UpdateServer changes the fields a user can edit. Credentials are rotated
// separately so that renaming a server cannot accidentally clear them.
func (s *Store) UpdateServer(ctx context.Context, id, name, dockerSocket string) (*Server, error) {
	if s.sqlite != nil {
		return s.sqlite.UpdateServer(ctx, id, name, dockerSocket)
	}
	rows, err := s.pool.Query(ctx, `
		UPDATE servers SET name = $2, docker_socket = $3
		WHERE id = $1
		RETURNING `+serverColumns, id, name, dockerSocket)
	if err != nil {
		return nil, wrap("store: update server", err)
	}
	server, err := pgx.CollectExactlyOneRow(rows, pgx.RowToStructByName[Server])
	if err != nil {
		return nil, wrap("store: update server", err)
	}
	return &server, nil
}

// SetServerSudoPassword updates or clears the stored sudo password for a server.
func (s *Store) SetServerSudoPassword(ctx context.Context, sealer Sealer, serverID, sudoPassword string) error {
	if s.sqlite != nil {
		return s.sqlite.SetServerSudoPassword(ctx, sealer, serverID, sudoPassword)
	}
	return s.tx(ctx, func(tx pgx.Tx) error {
		var oldSudoID *string
		err := tx.QueryRow(ctx, `SELECT sudo_password_secret_id FROM servers WHERE id = $1`, serverID).Scan(&oldSudoID)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrNotFound
			}
			return wrap("store: find server for sudo password", err)
		}

		var newSudoID *string
		if sudoPassword != "" {
			id, err := insertSecret(ctx, tx, sealer, KindSudoPassword, sudoPassword)
			if err != nil {
				return err
			}
			newSudoID = id
		}

		_, err = tx.Exec(ctx, `UPDATE servers SET sudo_password_secret_id = $1, updated_at = now() WHERE id = $2`, newSudoID, serverID)
		if err != nil {
			return wrap("store: update server sudo password", err)
		}

		if oldSudoID != nil && (newSudoID == nil || *oldSudoID != *newSudoID) {
			_, _ = tx.Exec(ctx, `DELETE FROM secrets WHERE id = $1`, *oldSudoID)
		}
		return nil
	})
}

// DeleteServer removes a server and the secrets that belonged to it.
//
// Order matters: the server row references the secrets, so it goes first.
// Leaving the secrets behind would accumulate undeletable encrypted rows that
// nothing points at.
func (s *Store) DeleteServer(ctx context.Context, id string) error {
	if s.sqlite != nil {
		return s.sqlite.DeleteServer(ctx, id)
	}
	return s.tx(ctx, func(tx pgx.Tx) error {
		var secretIDs []*string
		err := tx.QueryRow(ctx, `
			SELECT ARRAY[secret_id, passphrase_secret_id, sudo_password_secret_id]
			FROM servers WHERE id = $1`, id).Scan(&secretIDs)
		if err != nil {
			return wrap("store: delete server", err)
		}

		if _, err := tx.Exec(ctx, `DELETE FROM servers WHERE id = $1`, id); err != nil {
			return wrap("store: delete server", err)
		}

		for _, secretID := range secretIDs {
			if secretID == nil {
				continue
			}
			if _, err := tx.Exec(ctx, `DELETE FROM secrets WHERE id = $1`, *secretID); err != nil {
				return wrap("store: delete server secret", err)
			}
		}
		return nil
	})
}

// --- per-server membership ----------------------------------------------

type ServerMember struct {
	ServerID   string           `db:"server_id"  json:"server_id"`
	UserID     string           `db:"user_id"    json:"user_id"`
	Email      string           `db:"email"      json:"email"`
	Name       string           `db:"name"       json:"name"`
	Permission ServerPermission `db:"permission" json:"permission"`
	CreatedAt  time.Time        `db:"created_at" json:"created_at"`
}

func (s *Store) ListServerMembers(ctx context.Context, serverID string) ([]ServerMember, error) {
	if s.sqlite != nil {
		return s.sqlite.ListServerMembers(ctx, serverID)
	}
	rows, err := s.pool.Query(ctx, `
		SELECT m.server_id, m.user_id, u.email, u.name, m.permission, m.created_at
		FROM server_members m
		JOIN users u ON u.id = m.user_id
		WHERE m.server_id = $1
		ORDER BY u.email`, serverID)
	if err != nil {
		return nil, wrap("store: list server members", err)
	}
	members, err := pgx.CollectRows(rows, pgx.RowToStructByName[ServerMember])
	return members, wrap("store: list server members", err)
}

func (s *Store) GrantServerAccess(ctx context.Context, serverID, userID string, permission ServerPermission) error {
	if s.sqlite != nil {
		return s.sqlite.GrantServerAccess(ctx, serverID, userID, permission)
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO server_members (server_id, user_id, permission)
		VALUES ($1, $2, $3)
		ON CONFLICT (server_id, user_id) DO UPDATE SET permission = EXCLUDED.permission`,
		serverID, userID, permission)
	return wrap("store: grant server access", err)
}

func (s *Store) RevokeServerAccess(ctx context.Context, serverID, userID string) error {
	if s.sqlite != nil {
		return s.sqlite.RevokeServerAccess(ctx, serverID, userID)
	}
	_, err := s.pool.Exec(ctx,
		`DELETE FROM server_members WHERE server_id = $1 AND user_id = $2`, serverID, userID)
	return wrap("store: revoke server access", err)
}
