package dockerx_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/sirtheprogrammer/docker-deployments/server/internal/dockerx"
	"github.com/sirtheprogrammer/docker-deployments/server/internal/sshx"
)

// These tests exercise the mechanism the whole platform rests on: an SSH
// connection tunnelled to a remote Docker socket, carrying the Engine API.
//
// They need a real sshd with the Docker socket mounted. testdata/fixture.sh
// starts one. Without it they skip, so `go test ./...` stays runnable
// anywhere.
//
//	sh internal/dockerx/testdata/fixture.sh up
//	DOCKDEPLOY_SSH_HOST=127.0.0.1 DOCKDEPLOY_SSH_PORT=2222 \
//	DOCKDEPLOY_SSH_USER=root DOCKDEPLOY_SSH_KEY=/tmp/sshfix/id_ed25519 \
//	go test ./internal/dockerx/ -run Tunnel -v

type fixture struct {
	host string
	port int
	user string
	key  []byte
}

func loadFixture(t *testing.T) fixture {
	t.Helper()

	host := os.Getenv("DOCKDEPLOY_SSH_HOST")
	keyPath := os.Getenv("DOCKDEPLOY_SSH_KEY")
	if host == "" || keyPath == "" {
		t.Skip("set DOCKDEPLOY_SSH_HOST and DOCKDEPLOY_SSH_KEY to run tunnel tests")
	}

	key, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatalf("read private key: %v", err)
	}

	port := 22
	if raw := os.Getenv("DOCKDEPLOY_SSH_PORT"); raw != "" {
		if _, err := fmtSscan(raw, &port); err != nil {
			t.Fatalf("bad DOCKDEPLOY_SSH_PORT %q: %v", raw, err)
		}
	}

	user := os.Getenv("DOCKDEPLOY_SSH_USER")
	if user == "" {
		user = "root"
	}
	return fixture{host: host, port: port, user: user, key: key}
}

func (f fixture) credential() sshx.Credential {
	return sshx.Credential{Method: sshx.AuthKey, PrivateKey: f.key}
}

// connect performs the real add-a-server sequence: read the host key, pin it,
// then authenticate against it.
func (f fixture) connect(t *testing.T, ctx context.Context) *sshx.Conn {
	t.Helper()

	fingerprint, keyType, err := sshx.Fingerprint(ctx, f.host, f.port)
	if err != nil {
		t.Fatalf("Fingerprint: %v", err)
	}
	if !strings.HasPrefix(fingerprint, "SHA256:") {
		t.Fatalf("fingerprint = %q, want a SHA256: prefix", fingerprint)
	}
	t.Logf("host key %s %s", keyType, fingerprint)

	pool := sshx.NewPool(slog.New(slog.DiscardHandler))
	t.Cleanup(pool.Close)

	target := sshx.Target{Host: f.host, Port: f.port, User: f.user, Fingerprint: fingerprint}
	conn, err := pool.Get(ctx, "test-server", target, f.credential())
	if err != nil {
		t.Fatalf("pool.Get: %v", err)
	}
	t.Cleanup(conn.Release)
	return conn
}

func TestTunnelListsContainers(t *testing.T) {
	f := loadFixture(t)
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()

	conn := f.connect(t, ctx)

	client, err := dockerx.New(ctx, conn, dockerx.DefaultSocket)
	if err != nil {
		t.Fatalf("dockerx.New: %v", err)
	}
	defer func() { _ = client.Close() }()

	containers, err := client.ListContainers(ctx)
	if err != nil {
		t.Fatalf("ListContainers: %v", err)
	}
	if len(containers) == 0 {
		t.Fatal("no containers returned; the fixture host should be running at least itself")
	}
	for _, c := range containers {
		if c.ID == "" || c.Name == "" {
			t.Errorf("container with empty id or name: %+v", c)
		}
	}
	t.Logf("discovered %d containers, first %q (%s)", len(containers), containers[0].Name, containers[0].State)

	info, err := client.Info(ctx)
	if err != nil {
		t.Fatalf("Info: %v", err)
	}
	if info.ServerVersion == "" {
		t.Error("daemon reported no version")
	}
	t.Logf("daemon %s on %s/%s, %d cpus", info.ServerVersion, info.OperatingSystem, info.Architecture, info.CPUs)
}

// Non-TTY containers multiplex stdout and stderr with an 8-byte frame header.
// If that framing is not stripped, the header bytes appear in the output as
// binary noise, which is the classic Engine API mistake.
func TestTunnelDemultiplexesLogs(t *testing.T) {
	f := loadFixture(t)
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()

	conn := f.connect(t, ctx)
	client, err := dockerx.New(ctx, conn, dockerx.DefaultSocket)
	if err != nil {
		t.Fatalf("dockerx.New: %v", err)
	}
	defer func() { _ = client.Close() }()

	// Run a container that writes to both streams, without a TTY so the
	// output really is framed.
	const marker = "dockdeploy-log-marker"
	run, err := conn.Run(ctx,
		`docker run -d --rm alpine:3.21 sh -c `+
			`'echo `+marker+`-stdout; echo `+marker+`-stderr >&2; sleep 30'`)
	if err != nil {
		t.Fatalf("start log producer: %v", err)
	}
	if !run.Ok() {
		t.Fatalf("start log producer: exit %d: %s", run.ExitCode, run.Stderr)
	}
	id := run.Output()
	t.Cleanup(func() {
		_, _ = conn.Run(context.WithoutCancel(ctx), "docker rm -f "+id)
	})

	// Give the container a moment to emit before reading.
	time.Sleep(2 * time.Second)

	var stdout, stderr bytes.Buffer
	if err := client.Logs(ctx, id, dockerx.LogOptions{Tail: "50"}, &stdout, &stderr); err != nil {
		t.Fatalf("Logs: %v", err)
	}

	if got := stdout.String(); !strings.Contains(got, marker+"-stdout") {
		t.Errorf("stdout = %q, want it to contain %q", got, marker+"-stdout")
	}
	if got := stderr.String(); !strings.Contains(got, marker+"-stderr") {
		t.Errorf("stderr = %q, want it to contain %q", got, marker+"-stderr")
	}

	// The frame header starts with a stream byte (1 or 2) followed by three
	// zero bytes. Finding a NUL in demultiplexed text output means the header
	// survived.
	if bytes.ContainsRune(stdout.Bytes(), 0) {
		t.Error("stdout contains NUL bytes; the log frame header was not stripped")
	}
}

func TestTunnelProbesCapabilities(t *testing.T) {
	f := loadFixture(t)
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()

	caps, err := sshx.Probe(ctx, f.connect(t, ctx))
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}

	if !caps.Ready() {
		t.Errorf("server reported not ready; warnings: %v", caps.Warnings)
	}
	if caps.DockerVersion == "" {
		t.Error("no docker version detected")
	}
	if caps.ComposeCommand == "" {
		t.Error("no compose command detected")
	}
	if caps.GitVersion == "" {
		t.Error("no git detected")
	}
	t.Logf("os=%q docker=%s compose=%q(%s) sudo=%s nginx=%q layout=%q warnings=%v",
		caps.OS, caps.DockerVersion, caps.ComposeCommand, caps.ComposeVersion,
		caps.SudoMode, caps.NginxVersion, caps.NginxLayout, caps.Warnings)
}

// A recorded fingerprint that no longer matches must stop the connection.
// This is the check that makes trust-on-first-use meaningful rather than
// decorative.
func TestTunnelRejectsWrongHostKey(t *testing.T) {
	f := loadFixture(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	target := sshx.Target{
		Host: f.host, Port: f.port, User: f.user,
		Fingerprint: "SHA256:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
	}
	if _, err := sshx.Connect(ctx, target, f.credential()); !errors.Is(err, sshx.ErrHostKeyMismatch) {
		t.Fatalf("Connect with wrong fingerprint = %v, want ErrHostKeyMismatch", err)
	}

	// An empty fingerprint must be refused too, rather than treated as
	// "accept anything".
	target.Fingerprint = ""
	if _, err := sshx.Connect(ctx, target, f.credential()); !errors.Is(err, sshx.ErrHostKeyMismatch) {
		t.Fatalf("Connect with no fingerprint = %v, want ErrHostKeyMismatch", err)
	}
}

func TestTunnelRejectsBadCredentials(t *testing.T) {
	f := loadFixture(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	fingerprint, _, err := sshx.Fingerprint(ctx, f.host, f.port)
	if err != nil {
		t.Fatalf("Fingerprint: %v", err)
	}
	target := sshx.Target{Host: f.host, Port: f.port, User: f.user, Fingerprint: fingerprint}

	_, err = sshx.Connect(ctx, target, sshx.Credential{
		Method: sshx.AuthPassword, Password: "definitely-not-the-password",
	})
	if !errors.Is(err, sshx.ErrAuthFailed) {
		t.Errorf("Connect with wrong password = %v, want ErrAuthFailed", err)
	}

	_, err = sshx.Connect(ctx, target, sshx.Credential{
		Method: sshx.AuthKey, PrivateKey: []byte("not a private key"),
	})
	if !errors.Is(err, sshx.ErrBadPrivateKey) {
		t.Errorf("Connect with malformed key = %v, want ErrBadPrivateKey", err)
	}
}

// The pool must hand out one connection per server rather than reconnecting,
// since a single dashboard page makes several Engine calls.
func TestPoolReusesOneConnection(t *testing.T) {
	f := loadFixture(t)
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()

	fingerprint, _, err := sshx.Fingerprint(ctx, f.host, f.port)
	if err != nil {
		t.Fatalf("Fingerprint: %v", err)
	}

	pool := sshx.NewPool(slog.New(slog.DiscardHandler))
	defer pool.Close()

	target := sshx.Target{Host: f.host, Port: f.port, User: f.user, Fingerprint: fingerprint}

	first, err := pool.Get(ctx, "server-a", target, f.credential())
	if err != nil {
		t.Fatalf("first Get: %v", err)
	}
	defer first.Release()

	for i := 0; i < 5; i++ {
		conn, err := pool.Get(ctx, "server-a", target, f.credential())
		if err != nil {
			t.Fatalf("Get %d: %v", i, err)
		}
		if conn.Client() != first.Client() {
			t.Error("pool dialled a second connection for the same server")
		}
		conn.Release()
	}

	if got := pool.Len(); got != 1 {
		t.Errorf("pool holds %d connections, want 1", got)
	}

	// Changing credentials or address must not reuse the old connection.
	pool.Evict("server-a")
	if got := pool.Len(); got != 0 {
		t.Errorf("pool holds %d connections after evict, want 0", got)
	}
}

// fmtSscan keeps the import list small for one numeric parse.
func fmtSscan(s string, out *int) (int, error) {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, errors.New("not a number")
		}
		n = n*10 + int(c-'0')
	}
	*out = n
	return 1, nil
}
