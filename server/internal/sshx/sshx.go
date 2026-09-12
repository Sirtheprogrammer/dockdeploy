// Package sshx is the single transport to every managed server.
//
// The platform installs no agent on the machines it manages: it opens one SSH
// connection and does everything through it -- tunnelling to the remote Docker
// socket, running commands, and copying files. That constraint is what makes
// onboarding a server "paste credentials" rather than "run this installer", and
// it is why this package sits underneath dockerx, deploy and nginxx rather than
// beside them.
package sshx

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

// Failure modes callers need to tell apart. Every one of these reaches a user
// as a specific instruction, because "connection failed" is useless when the
// fix is "add your user to the docker group".
var (
	// ErrUnreachable means the TCP connection never established.
	ErrUnreachable = errors.New("sshx: server unreachable")
	// ErrAuthFailed means the credentials were rejected.
	ErrAuthFailed = errors.New("sshx: authentication failed")
	// ErrHostKeyMismatch means the server presented a different host key than
	// the one recorded. This is either a rebuilt server or an interception;
	// the platform refuses to connect either way and makes the user re-verify.
	ErrHostKeyMismatch = errors.New("sshx: host key does not match the recorded fingerprint")
	// ErrBadPrivateKey means the key could not be parsed or needs a passphrase.
	ErrBadPrivateKey = errors.New("sshx: private key could not be read")
)

// AuthMethod is how the platform authenticates to a server.
type AuthMethod string

const (
	AuthPassword AuthMethod = "password"
	AuthKey      AuthMethod = "key"
)

func (a AuthMethod) Valid() bool { return a == AuthPassword || a == AuthKey }

// Credential carries the secret material for one connection. It is built from
// decrypted values at the moment of use and never stored in this form.
type Credential struct {
	Method     AuthMethod
	Password   string
	PrivateKey []byte
	Passphrase string
}

// Target identifies a server and the host key expected from it.
type Target struct {
	Host string
	Port int
	User string

	// Fingerprint is the SHA256 host key fingerprint the user confirmed when
	// the server was added. Connections verify against it; an empty value is
	// rejected rather than treated as "trust anything".
	Fingerprint string
}

func (t Target) Address() string {
	port := t.Port
	if port == 0 {
		port = 22
	}
	return net.JoinHostPort(t.Host, strconv.Itoa(port))
}

const (
	dialTimeout      = 15 * time.Second
	keepaliveEvery   = 30 * time.Second
	keepaliveTimeout = 15 * time.Second
)

// Fingerprint retrieves a server's host key without authenticating.
//
// The key exchange completes before authentication, so the fingerprint can be
// shown to the user for confirmation before any credential is put on the wire.
// That ordering is the whole point: the user approves the identity first, and
// only then does the platform send a password or key to it.
func Fingerprint(ctx context.Context, host string, port int) (fingerprint, keyType string, err error) {
	target := Target{Host: host, Port: port}

	var captured ssh.PublicKey
	config := &ssh.ClientConfig{
		User: "dockdeploy-probe",
		HostKeyCallback: func(_ string, _ net.Addr, key ssh.PublicKey) error {
			captured = key
			// Stop here. There is no reason to attempt authentication, and
			// refusing keeps this call incapable of leaking a credential.
			return errHostKeyCaptured
		},
		Timeout: dialTimeout,
	}

	conn, err := dial(ctx, target.Address(), config)
	if conn != nil {
		_ = conn.Close()
	}

	if captured == nil {
		return "", "", err
	}
	return ssh.FingerprintSHA256(captured), captured.Type(), nil
}

// errHostKeyCaptured aborts the handshake once the host key is in hand.
var errHostKeyCaptured = errors.New("sshx: host key captured")

// Connect opens an authenticated SSH connection, verifying the host key
// against the recorded fingerprint.
func Connect(ctx context.Context, target Target, cred Credential) (*ssh.Client, error) {
	if target.Fingerprint == "" {
		return nil, fmt.Errorf("%w: no fingerprint recorded for this server", ErrHostKeyMismatch)
	}

	authMethod, err := authMethod(cred)
	if err != nil {
		return nil, err
	}

	config := &ssh.ClientConfig{
		User:            target.User,
		Auth:            []ssh.AuthMethod{authMethod},
		HostKeyCallback: pinnedHostKey(target.Fingerprint),
		Timeout:         dialTimeout,
	}

	client, err := dial(ctx, target.Address(), config)
	if err != nil {
		return nil, err
	}

	go keepalive(client)
	return client, nil
}

// pinnedHostKey verifies the presented key against a recorded SHA256
// fingerprint. Nothing in this package ever calls ssh.InsecureIgnoreHostKey.
func pinnedHostKey(want string) ssh.HostKeyCallback {
	return func(_ string, _ net.Addr, key ssh.PublicKey) error {
		got := ssh.FingerprintSHA256(key)
		if got != want {
			return fmt.Errorf("%w: expected %s, got %s", ErrHostKeyMismatch, want, got)
		}
		return nil
	}
}

func authMethod(cred Credential) (ssh.AuthMethod, error) {
	switch cred.Method {
	case AuthPassword:
		return ssh.Password(cred.Password), nil

	case AuthKey:
		var signer ssh.Signer
		var err error
		if cred.Passphrase != "" {
			signer, err = ssh.ParsePrivateKeyWithPassphrase(cred.PrivateKey, []byte(cred.Passphrase))
		} else {
			signer, err = ssh.ParsePrivateKey(cred.PrivateKey)
		}
		if err != nil {
			var missing *ssh.PassphraseMissingError
			if errors.As(err, &missing) {
				return nil, fmt.Errorf("%w: this key is encrypted and needs a passphrase", ErrBadPrivateKey)
			}
			return nil, fmt.Errorf("%w: %v", ErrBadPrivateKey, err)
		}
		return ssh.PublicKeys(signer), nil

	default:
		return nil, fmt.Errorf("sshx: unsupported auth method %q", cred.Method)
	}
}

// dial honours context cancellation, which ssh.Dial on its own does not.
func dial(ctx context.Context, address string, config *ssh.ClientConfig) (*ssh.Client, error) {
	dialer := net.Dialer{Timeout: config.Timeout}
	conn, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnreachable, err)
	}

	// The handshake can hang on a host that accepts TCP but never speaks SSH.
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	} else {
		_ = conn.SetDeadline(time.Now().Add(config.Timeout))
	}

	sshConn, chans, reqs, err := ssh.NewClientConn(conn, address, config)
	if err != nil {
		_ = conn.Close()
		return nil, classifyHandshakeError(err)
	}
	_ = conn.SetDeadline(time.Time{})

	return ssh.NewClient(sshConn, chans, reqs), nil
}

func classifyHandshakeError(err error) error {
	if errors.Is(err, errHostKeyCaptured) {
		// Expected during Fingerprint; the caller reads the captured key.
		return err
	}
	if errors.Is(err, ErrHostKeyMismatch) {
		return err
	}

	// x/crypto/ssh reports authentication failures as opaque strings, so the
	// message is the only signal available.
	text := err.Error()
	switch {
	case strings.Contains(text, "unable to authenticate"),
		strings.Contains(text, "no supported methods remain"),
		strings.Contains(text, "permission denied"):
		return fmt.Errorf("%w: %v", ErrAuthFailed, err)
	case strings.Contains(text, "host key mismatch"):
		return fmt.Errorf("%w: %v", ErrHostKeyMismatch, err)
	default:
		return fmt.Errorf("%w: %v", ErrUnreachable, err)
	}
}

// keepalive holds the connection open through NAT and idle timeouts, and
// notices a dead peer so the pool can drop it instead of handing out a
// connection that will fail on first use.
func keepalive(client *ssh.Client) {
	ticker := time.NewTicker(keepaliveEvery)
	defer ticker.Stop()

	for range ticker.C {
		done := make(chan error, 1)
		go func() {
			_, _, err := client.SendRequest("keepalive@dockdeploy", true, nil)
			done <- err
		}()

		select {
		case err := <-done:
			if err != nil {
				_ = client.Close()
				return
			}
		case <-time.After(keepaliveTimeout):
			_ = client.Close()
			return
		}
	}
}
