// Package servers turns a stored server row into a live connection.
//
// It is the seam between persistence and transport: handlers ask for a Docker
// client or a shell on a server by id, and this package decrypts the
// credentials, borrows a pooled SSH connection, and records what happened to
// the server's status along the way.
package servers

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/sirtheprogrammer/docker-deployments/server/internal/dockerx"
	"github.com/sirtheprogrammer/docker-deployments/server/internal/sshx"
	"github.com/sirtheprogrammer/docker-deployments/server/internal/store"
)

type Manager struct {
	store  *store.Store
	sealer store.Sealer
	pool   *sshx.Pool
	log    *slog.Logger
}

func NewManager(db *store.Store, sealer store.Sealer, pool *sshx.Pool, log *slog.Logger) *Manager {
	return &Manager{store: db, sealer: sealer, pool: pool, log: log}
}

// Pool exposes the connection pool so shutdown can close it.
func (m *Manager) Pool() *sshx.Pool { return m.pool }

// Session is a borrowed connection to a server. Close must be called.
type Session struct {
	Conn   *sshx.Conn
	Docker *dockerx.Client
}

// Close releases the Docker client and returns the connection to the pool.
func (s *Session) Close() {
	if s.Docker != nil {
		_ = s.Docker.Close()
	}
	if s.Conn != nil {
		s.Conn.Release()
	}
}

// Connect borrows an SSH connection to a server.
//
// Every failure updates the stored status, so the dashboard reflects reality
// without a separate polling loop: the act of using a server is what keeps its
// status honest.
func (m *Manager) Connect(ctx context.Context, server *store.Server) (*sshx.Conn, error) {
	cred, err := m.store.ServerCredential(ctx, m.sealer, server)
	if err != nil {
		m.setStatus(ctx, server, store.ServerOffline, "Stored credentials could not be read.")
		return nil, err
	}

	conn, err := m.pool.Get(ctx, server.ID, cred.Target, cred.Credential)
	if err != nil {
		m.setStatus(ctx, server, statusFor(err), message(err))
		return nil, err
	}
	return conn, nil
}

// Session opens a connection and a Docker client on it.
func (m *Manager) Session(ctx context.Context, server *store.Server) (*Session, error) {
	conn, err := m.Connect(ctx, server)
	if err != nil {
		return nil, err
	}

	client, err := dockerx.New(ctx, conn, server.DockerSocket)
	if err != nil {
		conn.Release()
		// SSH worked, so the server is reachable; it is Docker that is not.
		// Recording this as offline would be wrong and misleading.
		m.setStatus(ctx, server, store.ServerOnline, message(err))
		return nil, err
	}

	m.setStatus(ctx, server, store.ServerOnline, "")
	return &Session{Conn: conn, Docker: client}, nil
}

// Probe connects and inspects what the server can do, storing the result.
//
// Called when a server is added and whenever a user asks to re-check, rather
// than on a timer: probing runs a dozen commands and doing that continuously
// across a fleet is noise on someone else's machine.
func (m *Manager) Probe(ctx context.Context, server *store.Server) (*sshx.Capabilities, error) {
	conn, err := m.Connect(ctx, server)
	if err != nil {
		return nil, err
	}
	defer conn.Release()

	caps, err := sshx.Probe(ctx, conn)
	if err != nil {
		m.setStatus(ctx, server, store.ServerOffline, message(err))
		return nil, err
	}

	// A server that cannot run containers is still online; it is just not
	// usable yet, and the warnings explain why.
	status := store.ServerOnline
	note := ""
	if !caps.Ready() && len(caps.Warnings) > 0 {
		note = caps.Warnings[0]
	}

	if err := m.store.UpdateServerStatus(ctx, server.ID, status, note, caps); err != nil {
		m.log.Warn("record probe result", "server", server.ID, "error", err)
	}
	return caps, nil
}

// Evict drops a server's pooled connection. Call after changing its address,
// credentials or pinned host key so the next request reconnects with the new
// details rather than reusing a connection made under the old ones.
func (m *Manager) Evict(serverID string) { m.pool.Evict(serverID) }

func (m *Manager) setStatus(ctx context.Context, server *store.Server, status store.ServerStatus, note string) {
	// Use a detached context: the request may already be cancelled, and the
	// status is worth recording precisely when things went wrong.
	if err := m.store.UpdateServerStatus(context.WithoutCancel(ctx), server.ID, status, note, nil); err != nil {
		m.log.Warn("record server status", "server", server.ID, "status", status, "error", err)
	}
}

// statusFor maps a connection failure to a stored status. The distinction
// matters: offline is worth retrying, unauthorized needs a person.
func statusFor(err error) store.ServerStatus {
	switch {
	case errors.Is(err, sshx.ErrAuthFailed),
		errors.Is(err, sshx.ErrHostKeyMismatch),
		errors.Is(err, sshx.ErrBadPrivateKey):
		return store.ServerUnauthorized
	default:
		return store.ServerOffline
	}
}

// message produces the user-facing explanation for a connection failure.
// These are the errors people actually hit while setting a server up, so each
// one names the fix rather than restating the symptom.
func message(err error) string {
	switch {
	case errors.Is(err, sshx.ErrHostKeyMismatch):
		return "The host key does not match the one recorded when this server was added. " +
			"If the server was rebuilt, remove it and add it again. If not, do not connect."
	case errors.Is(err, sshx.ErrAuthFailed):
		return "The server rejected the credentials."
	case errors.Is(err, sshx.ErrBadPrivateKey):
		return "The stored private key could not be read. It may need a passphrase."
	case errors.Is(err, sshx.ErrUnreachable):
		return "Could not reach the server over SSH."
	case errors.Is(err, dockerx.ErrSocketDenied):
		return "Connected, but this user cannot read the Docker socket. " +
			"Add the user to the docker group and reconnect."
	case errors.Is(err, dockerx.ErrDaemonUnreachable):
		return "Connected over SSH, but the Docker daemon did not respond."
	default:
		return fmt.Sprintf("Connection failed: %v", err)
	}
}
