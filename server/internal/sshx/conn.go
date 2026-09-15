package sshx

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"strings"
	"sync"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

// Conn is a live connection to one server, shared by every caller that needs
// it. Release must be called when done so the pool can retire it.
type Conn struct {
	client  *ssh.Client
	pool    *Pool
	key     string
	isLocal bool

	releaseOnce sync.Once
}

// Client exposes the underlying SSH client for callers that need a raw channel.
func (c *Conn) Client() *ssh.Client { return c.client }

// IsLocal reports whether this connection runs directly against the local host.
func (c *Conn) IsLocal() bool { return c.isLocal }

// Release returns the connection to the pool.
func (c *Conn) Release() {
	if c.isLocal {
		return
	}
	c.releaseOnce.Do(func() {
		if c.pool != nil {
			c.pool.release(c.key)
		}
	})
}

// DialSocket opens a channel straight to a Unix socket on the remote host.
//
// This is the mechanism the whole platform rests on: pointed at
// /var/run/docker.sock it yields a net.Conn carrying the Docker Engine API, so
// dockerx gets the full API -- inspect, logs, stats, exec -- with nothing
// installed on the far end. It uses the direct-streamlocal@openssh.com channel
// type, which OpenSSH has supported for years.
// DefaultSocket is the standard Linux Docker daemon Unix socket path.
const DefaultSocket = "/var/run/docker.sock"

func (c *Conn) DialSocket(ctx context.Context, path string) (net.Conn, error) {
	if c.isLocal {
		if path == "" {
			path = DefaultSocket
		}
		var d net.Dialer
		return d.DialContext(ctx, "unix", path)
	}

	type result struct {
		conn net.Conn
		err  error
	}
	done := make(chan result, 1)

	go func() {
		conn, err := c.client.Dial("unix", path)
		done <- result{conn, err}
	}()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case r := <-done:
		if r.err != nil {
			return nil, fmt.Errorf("sshx: dial %s: %w", path, r.err)
		}
		return r.conn, nil
	}
}

// Result is the outcome of a remote command.
type Result struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

// Ok reports whether the command exited zero.
func (r Result) Ok() bool { return r.ExitCode == 0 }

// Output returns stdout trimmed, which is what capability probes want.
func (r Result) Output() string { return strings.TrimSpace(r.Stdout) }

// Run executes a command and collects its output.
//
// A non-zero exit is returned in Result rather than as an error: probing for
// optional software means running commands that are *expected* to fail, and
// forcing every caller to unwrap an ExitError for that would be noise. A
// returned error means the command could not be run at all.
func (c *Conn) Run(ctx context.Context, cmd string) (Result, error) {
	if c.isLocal {
		cmdObj := exec.CommandContext(ctx, "sh", "-c", cmd)
		var stdout, stderr bytes.Buffer
		cmdObj.Stdout = &stdout
		cmdObj.Stderr = &stderr
		err := cmdObj.Run()
		res := Result{Stdout: stdout.String(), Stderr: stderr.String()}
		if err == nil {
			return res, nil
		}
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			res.ExitCode = exitErr.ExitCode()
			return res, nil
		}
		return res, fmt.Errorf("local: run %q: %w", cmd, err)
	}

	session, err := c.client.NewSession()
	if err != nil {
		return Result{}, fmt.Errorf("sshx: open session: %w", err)
	}
	defer func() { _ = session.Close() }()

	var stdout, stderr bytes.Buffer
	session.Stdout = &stdout
	session.Stderr = &stderr

	done := make(chan error, 1)
	go func() { done <- session.Run(cmd) }()

	select {
	case <-ctx.Done():
		// Closing the session unblocks Run; without this a hung command would
		// pin the goroutine for the life of the process.
		_ = session.Signal(ssh.SIGKILL)
		_ = session.Close()
		return Result{}, ctx.Err()

	case err := <-done:
		result := Result{Stdout: stdout.String(), Stderr: stderr.String()}
		if err == nil {
			return result, nil
		}
		var exitErr *ssh.ExitError
		if errors.As(err, &exitErr) {
			result.ExitCode = exitErr.ExitStatus()
			return result, nil
		}
		return result, fmt.Errorf("sshx: run %q: %w", cmd, err)
	}
}

// Stream runs a command and copies its output as it arrives, for long jobs
// like builds where waiting for completion would leave the user staring at
// nothing.
func (c *Conn) Stream(ctx context.Context, cmd string, stdout, stderr io.Writer) (int, error) {
	if c.isLocal {
		cmdObj := exec.CommandContext(ctx, "sh", "-c", cmd)
		cmdObj.Stdout = stdout
		cmdObj.Stderr = stderr
		err := cmdObj.Run()
		if err == nil {
			return 0, nil
		}
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return exitErr.ExitCode(), nil
		}
		return 0, fmt.Errorf("local: run %q: %w", cmd, err)
	}

	session, err := c.client.NewSession()
	if err != nil {
		return 0, fmt.Errorf("sshx: open session: %w", err)
	}
	defer func() { _ = session.Close() }()

	session.Stdout = stdout
	session.Stderr = stderr

	done := make(chan error, 1)
	go func() { done <- session.Run(cmd) }()

	select {
	case <-ctx.Done():
		_ = session.Signal(ssh.SIGKILL)
		_ = session.Close()
		return 0, ctx.Err()

	case err := <-done:
		if err == nil {
			return 0, nil
		}
		var exitErr *ssh.ExitError
		if errors.As(err, &exitErr) {
			return exitErr.ExitStatus(), nil
		}
		return 0, fmt.Errorf("sshx: run %q: %w", cmd, err)
	}
}

// WriteFile uploads content over SFTP with explicit permissions.
//
// Deployment .env files land here, so mode matters: they are written 0600 and
// must never be readable by other users on a shared box.
func (c *Conn) WriteFile(ctx context.Context, path string, content []byte, mode uint32) error {
	if c.isLocal {
		if dir := parentDir(path); dir != "" {
			if err := os.MkdirAll(dir, 0755); err != nil {
				return fmt.Errorf("local: mkdir %s: %w", dir, err)
			}
		}
		if err := os.WriteFile(path, content, fsMode(mode)); err != nil {
			return fmt.Errorf("local: write %s: %w", path, err)
		}
		return nil
	}

	client, err := sftp.NewClient(c.client)
	if err != nil {
		return fmt.Errorf("sshx: open sftp: %w", err)
	}
	defer func() { _ = client.Close() }()

	if dir := parentDir(path); dir != "" {
		if err := client.MkdirAll(dir); err != nil {
			return fmt.Errorf("sshx: mkdir %s: %w", dir, err)
		}
	}

	file, err := client.Create(path)
	if err != nil {
		return fmt.Errorf("sshx: create %s: %w", path, err)
	}
	defer func() { _ = file.Close() }()

	// Tighten permissions before writing, so the content is never briefly
	// visible at the default mode.
	if err := file.Chmod(fsMode(mode)); err != nil {
		return fmt.Errorf("sshx: chmod %s: %w", path, err)
	}
	if _, err := file.Write(content); err != nil {
		return fmt.Errorf("sshx: write %s: %w", path, err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return nil
}

// ReadFile downloads a remote file.
func (c *Conn) ReadFile(ctx context.Context, path string) ([]byte, error) {
	if c.isLocal {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("local: read %s: %w", path, err)
		}
		return data, nil
	}

	client, err := sftp.NewClient(c.client)
	if err != nil {
		return nil, fmt.Errorf("sshx: open sftp: %w", err)
	}
	defer func() { _ = client.Close() }()

	file, err := client.Open(path)
	if err != nil {
		return nil, fmt.Errorf("sshx: open %s: %w", path, err)
	}
	defer func() { _ = file.Close() }()

	content, err := io.ReadAll(file)
	if err != nil {
		return nil, fmt.Errorf("sshx: read %s: %w", path, err)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return content, nil
}

func parentDir(path string) string {
	if i := strings.LastIndex(path, "/"); i > 0 {
		return path[:i]
	}
	return ""
}
