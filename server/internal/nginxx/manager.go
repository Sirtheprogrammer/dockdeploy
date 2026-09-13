package nginxx

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/sirtheprogrammer/docker-deployments/server/internal/servers"
	"github.com/sirtheprogrammer/docker-deployments/server/internal/sshx"
	"github.com/sirtheprogrammer/docker-deployments/server/internal/store"
)

// Manager coordinates nginx vhost generation, validation, deployment,
// and certbot certificate issuance across managed servers over SSH.
type Manager struct {
	store   *store.Store
	sealer  store.Sealer
	servers *servers.Manager
	log     *slog.Logger
}

func NewManager(db *store.Store, sealer store.Sealer, servers *servers.Manager, log *slog.Logger) *Manager {
	return &Manager{
		store:   db,
		sealer:  sealer,
		servers: servers,
		log:     log,
	}
}

// Deploy renders the vhost, uploads to a temporary path, validates it,
// backs up any existing configuration, moves the new file into place,
// tests the system nginx configuration, and reloads nginx. If anything fails,
// previous configuration is restored and an error is reported.
func (m *Manager) Deploy(ctx context.Context, server *store.Server, domain *store.Domain, sudoPasswords ...string) (string, error) {
	caps := readCapabilities(server)

	conn, err := m.servers.Connect(ctx, server)
	if err != nil {
		return "", fmt.Errorf("connect to server %s: %w", server.Name, err)
	}
	defer conn.Release()

	cred, err := m.store.ServerCredential(ctx, m.sealer, server)
	if err != nil {
		return "", fmt.Errorf("read server credentials: %w", err)
	}

	sudoPassword := cred.SudoPassword
	if len(sudoPasswords) > 0 && sudoPasswords[0] != "" {
		sudoPassword = sudoPasswords[0]
	}

	if caps.NginxLayout == sshx.NginxNone {
		// Live probe over SSH in case nginx was installed after initial registration
		if d, err := conn.Run(ctx, `test -d /etc/nginx/sites-available && echo yes`); err == nil && d.Output() == "yes" {
			caps.NginxLayout = sshx.NginxDebian
		} else if d, err := conn.Run(ctx, `test -d /etc/nginx/conf.d && echo yes`); err == nil && d.Output() == "yes" {
			caps.NginxLayout = sshx.NginxConfD
		} else if d, err := conn.Run(ctx, `which nginx 2>/dev/null && echo yes`); err == nil && d.Output() == "yes" {
			_, _ = m.runSudo(ctx, conn, caps, sudoPassword, "mkdir -p /etc/nginx/conf.d")
			caps.NginxLayout = sshx.NginxConfD
		} else {
			return "", errors.New("nginx is not installed or its directory layout is unrecognized on this server")
		}
	}

	if caps.SudoMode == sshx.SudoNone && sudoPassword == "" {
		return "", errors.New("sudo privileges required: please provide the sudo password for this server")
	}

	// Ensure webroot directory exists for ACME challenge
	_, _ = m.runSudo(ctx, conn, caps, sudoPassword, "mkdir -p /var/www/certbot && chmod 755 /var/www/certbot")

	// Check whether SSL certificate files exist on the server
	hasSSL := false
	if domain.SSLMode == store.DomainSSLLetsEncrypt {
		checkCmd := fmt.Sprintf("test -f /etc/letsencrypt/live/%s/fullchain.pem && test -f /etc/letsencrypt/live/%s/privkey.pem && echo yes",
			domain.Hostname, domain.Hostname)
		res, err := m.runSudo(ctx, conn, caps, sudoPassword, checkCmd)
		if err == nil && res.Output() == "yes" {
			hasSSL = true
		}
	}

	rendered, err := Render(Config{
		Hostname:     domain.Hostname,
		UpstreamPort: domain.UpstreamPort,
		WebSocket:    domain.WebSocket,
		HasSSL:       hasSSL,
	})
	if err != nil {
		return "", fmt.Errorf("render vhost: %w", err)
	}

	// Step 1: Upload to temp file
	tempPath := fmt.Sprintf("/tmp/dockdeploy-%s.conf", domain.Hostname)
	if err := conn.WriteFile(ctx, tempPath, []byte(rendered), 0o644); err != nil {
		// Fallback write via sudo with password support
		writeCmd := fmt.Sprintf("cat << 'EOF' > %s\n%s\nEOF", tempPath, rendered)
		if _, wErr := m.runSudo(ctx, conn, caps, sudoPassword, writeCmd); wErr != nil {
			return "", fmt.Errorf("upload temp config: %w (fallback error: %v)", err, wErr)
		}
	}
	defer func() {
		_, _ = m.runSudo(ctx, conn, caps, sudoPassword, fmt.Sprintf("rm -f %s", shellQuote(tempPath)))
	}()

	// Determine target path
	var availablePath, enabledPath string
	if caps.NginxLayout == sshx.NginxDebian {
		availablePath = fmt.Sprintf("/etc/nginx/sites-available/%s.conf", domain.Hostname)
		enabledPath = fmt.Sprintf("/etc/nginx/sites-enabled/%s.conf", domain.Hostname)
		_, _ = m.runSudo(ctx, conn, caps, sudoPassword, "mkdir -p /etc/nginx/sites-available /etc/nginx/sites-enabled")
	} else {
		availablePath = fmt.Sprintf("/etc/nginx/conf.d/%s.conf", domain.Hostname)
		_, _ = m.runSudo(ctx, conn, caps, sudoPassword, "mkdir -p /etc/nginx/conf.d")
	}

	// Step 2: Back up existing file if present
	backupPath := availablePath + ".dockdeploy.bak"
	_, _ = m.runSudo(ctx, conn, caps, sudoPassword,
		fmt.Sprintf("if [ -f %s ]; then cp -f %s %s; fi", shellQuote(availablePath), shellQuote(availablePath), shellQuote(backupPath)))

	// Step 3: Move new config into place
	installCmd := fmt.Sprintf("cp -f %s %s && chmod 644 %s",
		shellQuote(tempPath), shellQuote(availablePath), shellQuote(availablePath))
	if enabledPath != "" {
		installCmd += fmt.Sprintf(" && ln -sf %s %s", shellQuote(availablePath), shellQuote(enabledPath))
	}
	if res, err := m.runSudo(ctx, conn, caps, sudoPassword, installCmd); err != nil || !res.Ok() {
		errDetail := ""
		if err != nil {
			errDetail = err.Error()
		} else {
			errDetail = strings.TrimSpace(res.Stderr + " " + res.Stdout)
		}
		return "", fmt.Errorf("failed to install virtual host: %s", errDetail)
	}

	// Step 4: Test full system nginx configuration before reload
	sysTest, err := m.runSudo(ctx, conn, caps, sudoPassword, "nginx -t 2>&1")
	if err != nil || !sysTest.Ok() {
		m.rollback(ctx, conn, caps, sudoPassword, availablePath, enabledPath, backupPath)
		out := sysTest.Stdout + sysTest.Stderr
		return "", fmt.Errorf("system nginx configuration check failed: %s", strings.TrimSpace(out))
	}

	// Step 5: Reload nginx
	reloadCmd := "systemctl reload nginx || service nginx reload || nginx -s reload"
	reloadRes, err := m.runSudo(ctx, conn, caps, sudoPassword, reloadCmd)
	if err != nil || !reloadRes.Ok() {
		m.rollback(ctx, conn, caps, sudoPassword, availablePath, enabledPath, backupPath)
		out := reloadRes.Stdout + reloadRes.Stderr
		return "", fmt.Errorf("nginx reload failed: %s", strings.TrimSpace(out))
	}

	// Cleanup backup on success
	_, _ = m.runSudo(ctx, conn, caps, sudoPassword, fmt.Sprintf("rm -f %s", shellQuote(backupPath)))

	// Update database
	if err := m.store.UpdateDomainStatus(ctx, domain.ID, store.UpdateDomainParams{
		ConfigRendered: &rendered,
		Status:         store.DomainStatusActive,
		StatusMessage:  "",
	}); err != nil {
		m.log.Warn("record domain status", "domain", domain.Hostname, "error", err)
	}

	return rendered, nil
}

// IssueSSL issues a Let's Encrypt TLS certificate for the domain using certbot webroot,
// re-renders the nginx virtual host with SSL enabled, validates, and reloads nginx.
func (m *Manager) IssueSSL(ctx context.Context, server *store.Server, domain *store.Domain, sudoPasswords ...string) error {
	caps := readCapabilities(server)

	conn, err := m.servers.Connect(ctx, server)
	if err != nil {
		return fmt.Errorf("connect to server %s: %w", server.Name, err)
	}
	defer conn.Release()

	cred, err := m.store.ServerCredential(ctx, m.sealer, server)
	if err != nil {
		return fmt.Errorf("read server credentials: %w", err)
	}

	sudoPassword := cred.SudoPassword
	if len(sudoPasswords) > 0 && sudoPasswords[0] != "" {
		sudoPassword = sudoPasswords[0]
	}

	if caps.CertbotVersion == "" {
		res, _ := m.runSudo(ctx, conn, caps, sudoPassword, "certbot --version 2>&1")
		if !res.Ok() {
			msg := "certbot is not installed on this server; install certbot before issuing certificates"
			_ = m.store.UpdateDomainStatus(ctx, domain.ID, store.UpdateDomainParams{
				Status:        store.DomainStatusError,
				StatusMessage: msg,
			})
			return errors.New(msg)
		}
	}

	// Ensure webroot directory exists with correct permissions
	if res, err := m.runSudo(ctx, conn, caps, sudoPassword, "mkdir -p /var/www/certbot && chmod 755 /var/www/certbot"); err != nil || !res.Ok() {
		return fmt.Errorf("create /var/www/certbot: %s", res.Stderr)
	}

	// Ensure HTTP vhost is active so Let's Encrypt can reach /.well-known/acme-challenge/
	if _, err := m.Deploy(ctx, server, domain, sudoPassword); err != nil {
		return fmt.Errorf("deploy HTTP vhost for acme challenge: %w", err)
	}

	// Run certbot certonly with webroot
	certbotCmd := fmt.Sprintf(
		"certbot certonly --webroot -w /var/www/certbot -d %s --non-interactive --agree-tos --register-unsafely-without-email 2>&1",
		shellQuote(domain.Hostname),
	)

	certRes, err := m.runSudo(ctx, conn, caps, sudoPassword, certbotCmd)
	if err != nil || !certRes.Ok() {
		errMsg := strings.TrimSpace(certRes.Stdout + certRes.Stderr)
		_ = m.store.UpdateDomainStatus(ctx, domain.ID, store.UpdateDomainParams{
			Status:        store.DomainStatusError,
			StatusMessage: fmt.Sprintf("Certificate issuance failed: %s", errMsg),
		})
		return fmt.Errorf("certbot issuance failed: %s", errMsg)
	}

	// Inspect expiration date from issued cert
	expCmd := fmt.Sprintf("openssl x509 -enddate -noout -in /etc/letsencrypt/live/%s/cert.pem 2>/dev/null", shellQuote(domain.Hostname))
	expRes, _ := m.runSudo(ctx, conn, caps, sudoPassword, expCmd)
	var expiresAt *time.Time
	if expRes.Ok() && strings.HasPrefix(expRes.Output(), "notAfter=") {
		rawDate := strings.TrimPrefix(expRes.Output(), "notAfter=")
		if parsed, err := time.Parse("Jan _2 15:04:05 2006 GMT", strings.TrimSpace(rawDate)); err == nil {
			expiresAt = &parsed
		}
	}

	// Deploy SSL-enabled configuration
	domain.SSLMode = store.DomainSSLLetsEncrypt
	rendered, err := m.Deploy(ctx, server, domain, sudoPassword)
	if err != nil {
		return fmt.Errorf("activate SSL vhost: %w", err)
	}

	_ = m.store.UpdateDomainStatus(ctx, domain.ID, store.UpdateDomainParams{
		ConfigRendered: &rendered,
		CertExpiresAt:  expiresAt,
		Status:         store.DomainStatusActive,
		StatusMessage:  "",
	})

	return nil
}

// Remove deletes the virtual host configuration and symlink from the server,
// tests the configuration, and reloads nginx.
func (m *Manager) Remove(ctx context.Context, server *store.Server, domain *store.Domain, sudoPasswords ...string) error {
	caps := readCapabilities(server)
	conn, err := m.servers.Connect(ctx, server)
	if err != nil {
		return fmt.Errorf("connect to server %s: %w", server.Name, err)
	}
	defer conn.Release()

	cred, err := m.store.ServerCredential(ctx, m.sealer, server)
	if err != nil {
		return fmt.Errorf("read server credentials: %w", err)
	}

	sudoPassword := cred.SudoPassword
	if len(sudoPasswords) > 0 && sudoPasswords[0] != "" {
		sudoPassword = sudoPasswords[0]
	}

	var removeCmd string
	if caps.NginxLayout == sshx.NginxDebian {
		removeCmd = fmt.Sprintf("rm -f /etc/nginx/sites-enabled/%s.conf /etc/nginx/sites-available/%s.conf /etc/nginx/sites-available/%s.conf.dockdeploy.bak",
			shellQuote(domain.Hostname), shellQuote(domain.Hostname), shellQuote(domain.Hostname))
	} else {
		removeCmd = fmt.Sprintf("rm -f /etc/nginx/conf.d/%s.conf /etc/nginx/conf.d/%s.conf.dockdeploy.bak",
			shellQuote(domain.Hostname), shellQuote(domain.Hostname))
	}

	_, _ = m.runSudo(ctx, conn, caps, sudoPassword, removeCmd)

	// Validate and reload
	_, _ = m.runSudo(ctx, conn, caps, sudoPassword, "nginx -t && (systemctl reload nginx || service nginx reload || nginx -s reload || true)")

	return m.store.DeleteDomain(ctx, domain.ID)
}

func (m *Manager) rollback(ctx context.Context, conn *sshx.Conn, caps sshx.Capabilities, sudoPassword, availablePath, enabledPath, backupPath string) {
	cmd := fmt.Sprintf(
		"if [ -f %s ]; then mv -f %s %s; else rm -f %s %s; fi && (systemctl reload nginx || service nginx reload || nginx -s reload || true)",
		shellQuote(backupPath), shellQuote(backupPath), shellQuote(availablePath),
		shellQuote(availablePath), shellQuote(enabledPath),
	)
	_, _ = m.runSudo(ctx, conn, caps, sudoPassword, cmd)
}

func (m *Manager) runSudo(ctx context.Context, conn *sshx.Conn, caps sshx.Capabilities, sudoPassword, cmd string) (sshx.Result, error) {
	var fullCmd string
	switch {
	case caps.SudoMode == sshx.SudoRoot:
		fullCmd = cmd
	case sudoPassword != "":
		fullCmd = fmt.Sprintf("printf '%%s\\n' %s | sudo -S -p '' sh -c %s", shellQuote(sudoPassword), shellQuote(cmd))
	case caps.SudoMode == sshx.SudoNoPassword:
		fullCmd = "sudo -n sh -c " + shellQuote(cmd)
	default:
		return sshx.Result{}, errors.New("sudo requires a password on this server, but none is saved")
	}

	res, err := conn.Run(ctx, fullCmd)
	if err == nil && !res.Ok() && caps.SudoMode == sshx.SudoNoPassword && sudoPassword != "" {
		fallbackCmd := fmt.Sprintf("printf '%%s\\n' %s | sudo -S -p '' sh -c %s", shellQuote(sudoPassword), shellQuote(cmd))
		if fRes, fErr := conn.Run(ctx, fallbackCmd); fErr == nil && fRes.Ok() {
			return fRes, nil
		}
	}
	return res, err
}

func readCapabilities(server *store.Server) sshx.Capabilities {
	var caps sshx.Capabilities
	if len(server.Capabilities) > 0 {
		_ = json.Unmarshal(server.Capabilities, &caps)
	}
	return caps
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}
