// Package dockerx speaks the Docker Engine API to a managed server.
//
// It never shells out to the docker CLI. An sshx connection is tunnelled
// straight to the remote /var/run/docker.sock and the official Engine client
// runs over that, which is what makes streaming logs, live stats and
// interactive exec possible at all -- none of those work well through
// `ssh host docker logs -f`.
package dockerx

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/docker/docker/client"

	"github.com/sirtheprogrammer/docker-deployments/server/internal/sshx"
)

// DefaultSocket is where the daemon listens on essentially every Linux host.
const DefaultSocket = "/var/run/docker.sock"

var (
	// ErrSocketDenied means the SSH user cannot read the Docker socket. It has
	// a specific fix, so it gets a specific error.
	ErrSocketDenied = errors.New("dockerx: permission denied on the Docker socket")
	// ErrDaemonUnreachable means the socket is missing or nothing is listening.
	ErrDaemonUnreachable = errors.New("dockerx: cannot reach the Docker daemon")
)

// Client wraps the Engine API client together with the connection it rides on.
type Client struct {
	api  *client.Client
	conn *sshx.Conn
}

// API exposes the Engine client.
func (c *Client) API() *client.Client { return c.api }

// Conn exposes the underlying SSH connection, for callers that also need to
// run commands (compose, git) on the same server.
func (c *Client) Conn() *sshx.Conn { return c.conn }

// Close releases the Engine client. The SSH connection belongs to the pool and
// is released separately by whoever borrowed it.
func (c *Client) Close() error { return c.api.Close() }

// New builds an Engine API client that reaches the daemon through an SSH
// connection.
//
// Every HTTP request the client makes opens a fresh channel to the remote
// socket over the one SSH connection. The host in the URL is a placeholder --
// the dialler ignores it entirely -- but the client requires a syntactically
// valid one.
func New(ctx context.Context, conn *sshx.Conn, socketPath string) (*Client, error) {
	if socketPath == "" {
		socketPath = DefaultSocket
	}

	dialSocket := func(ctx context.Context, _, _ string) (net.Conn, error) {
		return conn.DialSocket(ctx, socketPath)
	}

	httpClient := &http.Client{
		Transport: &http.Transport{
			DialContext: dialSocket,
			// Long-lived streams (logs, stats, exec) each hold a channel open,
			// so idle pooling gains little and risks stale channels.
			DisableKeepAlives:   true,
			TLSHandshakeTimeout: 10 * time.Second,
		},
		// No global timeout: following logs and attaching a terminal are
		// requests that are supposed to stay open indefinitely.
	}

	api, err := client.NewClientWithOpts(
		client.WithHTTPClient(httpClient),
		// The host is a placeholder. Our dialler ignores the address entirely
		// and goes to the remote socket, but the SDK needs a parseable URL.
		client.WithHost("tcp://docker.invalid:2375"),
		// Order matters: WithHost calls sockets.ConfigureTransport, which
		// overwrites DialContext with a plain TCP dialler. Re-applying ours
		// afterwards is the only way the tunnel survives. Setting it on the
		// http.Transport above is not enough.
		client.WithDialContext(dialSocket),
		// The daemon on the far end may be older or newer than the SDK this
		// binary was built against; negotiate rather than guess.
		client.WithAPIVersionNegotiation(),
	)
	if err != nil {
		return nil, fmt.Errorf("dockerx: build client: %w", err)
	}

	// Prove the tunnel works now, so a broken server surfaces here rather than
	// as a confusing failure inside an unrelated request later.
	if _, err := api.Ping(ctx); err != nil {
		_ = api.Close()
		return nil, classify(err)
	}

	return &Client{api: api, conn: conn}, nil
}

// classify turns an opaque transport failure into something actionable.
func classify(err error) error {
	text := err.Error()
	switch {
	case strings.Contains(text, "permission denied"):
		return fmt.Errorf("%w: add the SSH user to the docker group with "+
			"'sudo usermod -aG docker <user>', then reconnect (%v)", ErrSocketDenied, err)
	case strings.Contains(text, "administratively prohibited"),
		strings.Contains(text, "open failed"):
		// sshd refused the channel rather than failing to reach the socket.
		// The usual cause is AllowTcpForwarding no in sshd_config: despite the
		// name it also gates direct-streamlocal, which is how this tunnel
		// reaches the Docker socket. Note that OpenSSH honours the first
		// occurrence of a keyword, so an override appended to the end of the
		// file has no effect.
		return fmt.Errorf("%w: the SSH server refused to forward to the Docker "+
			"socket. Set 'AllowTcpForwarding yes' in /etc/ssh/sshd_config "+
			"(edit the existing line, do not append) and reload sshd (%v)",
			ErrDaemonUnreachable, err)
	case strings.Contains(text, "no such file"), strings.Contains(text, "connect failed"):
		return fmt.Errorf("%w: no Docker socket at that path on the server (%v)",
			ErrDaemonUnreachable, err)
	default:
		return fmt.Errorf("%w: %v", ErrDaemonUnreachable, err)
	}
}
