package sshx

import (
	"context"
	"regexp"
	"strings"
)

// SudoMode records what escalation is available to the SSH user.
//
// It is probed at connect time rather than discovered when someone first tries
// to add a domain, because "nginx needs root and your user cannot get it" is a
// setup problem, and finding out halfway through issuing a certificate is a
// bad time to learn it.
type SudoMode string

const (
	// SudoNone means the user cannot escalate. nginx and certbot are off the
	// table; Docker still works if the socket is readable.
	SudoNone SudoMode = "none"
	// SudoNoPassword means passwordless sudo. This is the recommended setup.
	SudoNoPassword SudoMode = "nopasswd"
	// SudoPassword means sudo works but prompts, so a password must be stored.
	SudoPassword SudoMode = "password"
	// SudoRoot means the SSH user is already root.
	SudoRoot SudoMode = "root"
)

// NginxLayout distinguishes the two directory conventions. Writing a vhost
// into the wrong one produces a file nginx silently never reads.
type NginxLayout string

const (
	NginxNone NginxLayout = ""
	// NginxDebian uses sites-available with a symlink into sites-enabled.
	NginxDebian NginxLayout = "debian"
	// NginxConfD drops files straight into conf.d (RHEL, Alpine, and others).
	NginxConfD NginxLayout = "confd"
)

// Capabilities is what the platform found on a server.
type Capabilities struct {
	OS     string `json:"os"`
	Kernel string `json:"kernel"`

	DockerVersion string `json:"docker_version"`
	// DockerSocketOK is the one that actually gates everything. Docker being
	// installed is irrelevant if this user cannot talk to the daemon.
	DockerSocketOK bool `json:"docker_socket_ok"`

	// ComposeCommand is "docker compose", "docker-compose", or empty. The two
	// spellings are not interchangeable and old hosts still ship v1.
	ComposeCommand string `json:"compose_command"`
	ComposeVersion string `json:"compose_version"`

	SudoMode SudoMode `json:"sudo_mode"`

	NginxVersion string      `json:"nginx_version"`
	NginxLayout  NginxLayout `json:"nginx_layout"`

	CertbotVersion string `json:"certbot_version"`
	GitVersion     string `json:"git_version"`
	// SSHClient reports whether an ssh binary exists. Cloning over SSH with a
	// deploy key needs one, and its absence is easy to miss: a server running
	// sshd does not necessarily have the client installed.
	SSHClient bool `json:"ssh_client"`

	// Warnings are user-facing notes about what will not work and why.
	Warnings []string `json:"warnings"`
}

// Ready reports whether the server can do the core job: run containers.
func (c *Capabilities) Ready() bool { return c.DockerSocketOK }

// CanManageNginx reports whether domain management is possible here.
func (c *Capabilities) CanManageNginx() bool {
	return c.NginxLayout != NginxNone && c.SudoMode != SudoNone
}

var versionPattern = regexp.MustCompile(`\d+\.\d+(\.\d+)?`)

// Probe inspects a server and reports what the platform can do with it.
//
// Every check is independent and non-fatal: a machine with Docker but no nginx
// is perfectly usable for deployments, so a missing tool becomes a warning
// rather than a failed connection.
func Probe(ctx context.Context, conn *Conn) (*Capabilities, error) {
	caps := &Capabilities{}

	// Identify the host first; the error from this call is the only fatal one,
	// since it means commands do not run at all.
	if result, err := conn.Run(ctx, `uname -sr`); err != nil {
		return nil, err
	} else if result.Ok() {
		caps.Kernel = result.Output()
	}

	if result, err := conn.Run(ctx,
		`. /etc/os-release 2>/dev/null && echo "$PRETTY_NAME" || uname -s`); err == nil && result.Ok() {
		caps.OS = result.Output()
	}

	probeDocker(ctx, conn, caps)
	probeSudo(ctx, conn, caps)
	probeNginx(ctx, conn, caps)
	probeTooling(ctx, conn, caps)

	return caps, nil
}

func probeDocker(ctx context.Context, conn *Conn, caps *Capabilities) {
	result, err := conn.Run(ctx, `docker version --format '{{.Server.Version}}'`)
	if err != nil {
		return
	}

	if result.Ok() && result.Output() != "" {
		caps.DockerVersion = result.Output()
		caps.DockerSocketOK = true
	} else {
		// Distinguish "no docker" from "no permission". The second is the
		// common case and has a one-line fix, so it deserves a precise message
		// instead of a generic failure.
		combined := result.Stdout + result.Stderr
		switch {
		case strings.Contains(combined, "permission denied"):
			caps.Warnings = append(caps.Warnings,
				"The Docker socket is not readable by this user. Run: sudo usermod -aG docker $USER (then reconnect).")
		case strings.Contains(combined, "not found"), strings.Contains(combined, "command not found"):
			caps.Warnings = append(caps.Warnings, "Docker is not installed on this server.")
		case strings.Contains(combined, "Cannot connect to the Docker daemon"):
			caps.Warnings = append(caps.Warnings, "Docker is installed but the daemon is not running.")
		default:
			caps.Warnings = append(caps.Warnings, "Could not reach the Docker daemon on this server.")
		}
		return
	}

	// Compose v2 is a CLI plugin; v1 is a separate binary. Prefer v2.
	if r, err := conn.Run(ctx, `docker compose version --short`); err == nil && r.Ok() && r.Output() != "" {
		caps.ComposeCommand = "docker compose"
		caps.ComposeVersion = r.Output()
		return
	}
	if r, err := conn.Run(ctx, `docker-compose --version`); err == nil && r.Ok() {
		caps.ComposeCommand = "docker-compose"
		caps.ComposeVersion = versionPattern.FindString(r.Output())
		return
	}
	caps.Warnings = append(caps.Warnings,
		"Docker Compose is not available, so compose-based deployments will not run here.")
}

func probeSudo(ctx context.Context, conn *Conn, caps *Capabilities) {
	if r, err := conn.Run(ctx, `id -u`); err == nil && r.Output() == "0" {
		caps.SudoMode = SudoRoot
		return
	}

	// -n makes sudo fail rather than prompt, so this cannot hang on a password
	// prompt waiting for a terminal that does not exist.
	if r, err := conn.Run(ctx, `sudo -n true 2>&1`); err == nil && r.Ok() {
		caps.SudoMode = SudoNoPassword
		return
	}

	if r, err := conn.Run(ctx, `sudo -n -v 2>&1`); err == nil &&
		strings.Contains(r.Stdout+r.Stderr, "password") {
		caps.SudoMode = SudoPassword
		caps.Warnings = append(caps.Warnings,
			"sudo requires a password here. Add a NOPASSWD rule for nginx and certbot, or store the sudo password, before managing domains.")
		return
	}

	caps.SudoMode = SudoNone
}

func probeNginx(ctx context.Context, conn *Conn, caps *Capabilities) {
	r, err := conn.Run(ctx, `nginx -v 2>&1`)
	if err != nil || !r.Ok() {
		return
	}
	caps.NginxVersion = versionPattern.FindString(r.Stdout + r.Stderr)

	// Debian keeps vhosts in sites-available and symlinks the active ones;
	// everyone else uses conf.d. Detect rather than assume.
	if d, err := conn.Run(ctx, `test -d /etc/nginx/sites-available && echo yes`); err == nil && d.Output() == "yes" {
		caps.NginxLayout = NginxDebian
	} else if d, err := conn.Run(ctx, `test -d /etc/nginx/conf.d && echo yes`); err == nil && d.Output() == "yes" {
		caps.NginxLayout = NginxConfD
	} else {
		caps.Warnings = append(caps.Warnings,
			"nginx is installed but neither sites-available nor conf.d exists, so its layout is unrecognised.")
	}
}

func probeTooling(ctx context.Context, conn *Conn, caps *Capabilities) {
	if r, err := conn.Run(ctx, `certbot --version 2>&1`); err == nil && r.Ok() {
		caps.CertbotVersion = versionPattern.FindString(r.Stdout + r.Stderr)
	} else if caps.NginxVersion != "" {
		caps.Warnings = append(caps.Warnings,
			"certbot is not installed, so certificates cannot be issued for domains on this server.")
	}

	if r, err := conn.Run(ctx, `git --version`); err == nil && r.Ok() {
		caps.GitVersion = versionPattern.FindString(r.Output())
	} else {
		caps.Warnings = append(caps.Warnings,
			"git is not installed, so building from a repository on this server will not work. Deployments built here and pulled from a registry are unaffected.")
	}

	// A machine running sshd does not necessarily have the ssh client, and
	// without it a git+SSH deploy key fails with a confusing "could not read
	// from remote repository" rather than anything that names the cause.
	if r, err := conn.Run(ctx, `command -v ssh`); err == nil && r.Ok() {
		caps.SSHClient = true
	} else if caps.GitVersion != "" {
		caps.Warnings = append(caps.Warnings,
			"No ssh client is installed, so repositories cloned over SSH (git@host:path) will not work. "+
				"Install openssh-client, or use an https repository URL with a token instead.")
	}
}
