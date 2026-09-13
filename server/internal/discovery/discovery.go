package discovery

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/url"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/sirtheprogrammer/docker-deployments/server/internal/dockerx"
	"github.com/sirtheprogrammer/docker-deployments/server/internal/gitx"
	"github.com/sirtheprogrammer/docker-deployments/server/internal/servers"
	"github.com/sirtheprogrammer/docker-deployments/server/internal/sshx"
	"github.com/sirtheprogrammer/docker-deployments/server/internal/store"
)

// Report contains the results of an auto-detection run on a server.
type Report struct {
	ServerID    string                  `json:"server_id"`
	ServerName  string                  `json:"server_name"`
	Domains     []DiscoveredDomain      `json:"domains"`
	Deployments []DiscoveredDeployment  `json:"deployments"`
	Credentials []DiscoveredCredential  `json:"credentials"`
	Errors      []string                `json:"errors,omitempty"`
}

type DiscoveredDomain struct {
	ID           string              `json:"id"`
	Hostname     string              `json:"hostname"`
	UpstreamPort int                 `json:"upstream_port"`
	SSLMode      store.DomainSSLMode `json:"ssl_mode"`
	Action       string              `json:"action"` // "created", "updated", "existing"
}

type DiscoveredDeployment struct {
	ID         string                 `json:"id"`
	Name       string                 `json:"name"`
	Slug       string                 `json:"slug"`
	SourceType store.SourceType       `json:"source_type"`
	Status     store.DeploymentStatus `json:"status"`
	Action     string                 `json:"action"` // "created", "updated", "existing"
}

type DiscoveredCredential struct {
	ID       string    `json:"id"`
	Name     string    `json:"name"`
	Kind     gitx.Kind `json:"kind"`
	Username string    `json:"username"`
	Action   string    `json:"action"` // "saved", "existing"
}

// Service coordinates discovering domains, deployments, and git credentials
// on managed servers and saving them into dockdeploy.
type Service struct {
	store   *store.Store
	sealer  store.Sealer
	servers *servers.Manager
	log     *slog.Logger
}

func NewService(st *store.Store, sealer store.Sealer, mgr *servers.Manager, log *slog.Logger) *Service {
	return &Service{
		store:   st,
		sealer:  sealer,
		servers: mgr,
		log:     log,
	}
}

// DiscoverAll runs discovery across all provided servers concurrently.
func (s *Service) DiscoverAll(ctx context.Context, serverList []store.Server, actorUserID string) ([]Report, error) {
	if len(serverList) == 0 {
		return []Report{}, nil
	}

	reports := make([]Report, len(serverList))
	var wg sync.WaitGroup
	sem := make(chan struct{}, 5) // max 5 concurrent server discoveries

	for i := range serverList {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			srv := &serverList[idx]
			// Give each server discovery a bounded timeout
			serverCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
			defer cancel()

			report, err := s.DiscoverServer(serverCtx, srv, actorUserID)
			if err != nil {
				reports[idx] = Report{
					ServerID:   srv.ID,
					ServerName: srv.Name,
					Errors:     []string{err.Error()},
				}
			} else {
				reports[idx] = *report
			}
		}(i)
	}

	wg.Wait()
	return reports, nil
}

// DiscoverServer discovers domains, deployments, and git credentials on a server.
func (s *Service) DiscoverServer(ctx context.Context, server *store.Server, actorUserID string) (*Report, error) {
	report := &Report{
		ServerID:    server.ID,
		ServerName:  server.Name,
		Domains:     make([]DiscoveredDomain, 0),
		Deployments: make([]DiscoveredDeployment, 0),
		Credentials: make([]DiscoveredCredential, 0),
		Errors:      make([]string, 0),
	}

	// Read credentials for sudo access if needed
	cred, err := s.store.ServerCredential(ctx, s.sealer, server)
	if err != nil {
		report.Errors = append(report.Errors, fmt.Sprintf("read server credentials: %v", err))
		return report, fmt.Errorf("read server credentials: %w", err)
	}

	// Try opening a session (which provides both SSH connection and Docker client)
	session, sessionErr := s.servers.Session(ctx, server)
	var conn *sshx.Conn
	var dockerClient *dockerx.Client

	if sessionErr == nil {
		defer session.Close()
		conn = session.Conn
		dockerClient = session.Docker
	} else {
		// If Docker client failed, fall back to pure SSH connection so we can still
		// discover Nginx domains and Git credentials.
		c, connErr := s.servers.Connect(ctx, server)
		if connErr != nil {
			report.Errors = append(report.Errors, fmt.Sprintf("connect to server: %v", connErr))
			return report, fmt.Errorf("connect to server: %w", connErr)
		}
		defer c.Release()
		conn = c
		report.Errors = append(report.Errors, fmt.Sprintf("docker unavailable: %v", sessionErr))
	}

	// 1. Discover and autosave Git credentials from the server
	if conn != nil {
		s.discoverGitCredentials(ctx, conn, server, cred.SudoPassword, actorUserID, report)
	}

	// 2. Discover Nginx domains from virtual hosts on the server
	if conn != nil {
		s.discoverDomains(ctx, conn, server, cred.SudoPassword, actorUserID, report)
	}

	// 3. Discover Docker deployments from containers and Compose stacks
	if dockerClient != nil && conn != nil {
		s.discoverDeployments(ctx, dockerClient, conn, server, actorUserID, report)
	}

	// 4. Link domains to deployments on this server by upstream port matching
	s.linkDomainsAndDeployments(ctx, server.ID)

	return report, nil
}

// ---------------------------------------------------------------------------
// 1. Git Credentials Discovery & Autosave
// ---------------------------------------------------------------------------

type rawGitCred struct {
	Name     string
	Kind     gitx.Kind
	Username string
	Secret   string
}

func (s *Service) discoverGitCredentials(
	ctx context.Context,
	conn *sshx.Conn,
	server *store.Server,
	sudoPassword string,
	actorUserID string,
	report *Report,
) {
	// Script to inspect ~/.git-credentials, ~/.config/git/credentials, ~/.netrc,
	// and ~/.ssh private keys. Also checks /root if sudo is available.
	probeScript := `
sh -c '
# 1. ~/.git-credentials
if [ -f "$HOME/.git-credentials" ]; then
    echo "===DOCKDEPLOY_CRED:GIT_CREDENTIALS==="
    cat "$HOME/.git-credentials"
    echo "===DOCKDEPLOY_CRED_END==="
fi

# 2. ~/.config/git/credentials
if [ -f "$HOME/.config/git/credentials" ]; then
    echo "===DOCKDEPLOY_CRED:GIT_CREDENTIALS==="
    cat "$HOME/.config/git/credentials"
    echo "===DOCKDEPLOY_CRED_END==="
fi

# 3. ~/.netrc
if [ -f "$HOME/.netrc" ]; then
    echo "===DOCKDEPLOY_CRED:NETRC==="
    cat "$HOME/.netrc"
    echo "===DOCKDEPLOY_CRED_END==="
fi

# 4. Standard SSH private keys
for k in "$HOME/.ssh/id_rsa" "$HOME/.ssh/id_ed25519" "$HOME/.ssh/id_ecdsa"; do
    if [ -f "$k" ]; then
        echo "===DOCKDEPLOY_CRED:SSH_KEY:$(basename "$k")==="
        cat "$k"
        echo "===DOCKDEPLOY_CRED_END==="
    fi
done

# 5. Keys referenced in ~/.ssh/config
if [ -f "$HOME/.ssh/config" ]; then
    grep -i "^[[:space:]]*IdentityFile" "$HOME/.ssh/config" 2>/dev/null | awk "{print \$2}" | while read -r kp; do
        kp=$(eval echo "$kp")
        if [ -f "$kp" ]; then
            case "$kp" in
                *id_rsa|*id_ed25519|*id_ecdsa) ;;
                *)
                    echo "===DOCKDEPLOY_CRED:SSH_KEY:$(basename "$kp")==="
                    cat "$kp"
                    echo "===DOCKDEPLOY_CRED_END==="
                    ;;
            esac
        fi
    done
fi
'
`

	result, err := conn.Run(ctx, probeScript)
	if err != nil || !result.Ok() {
		// Non-fatal: user may not have git credentials
		s.log.Debug("git credentials probe finished", "server", server.Name, "exit", result.ExitCode)
		return
	}

	foundCreds := parseGitCredentialsOutput(result.Stdout, server.Name)

	// Fetch existing stored credentials so we don't recreate them
	existingCreds, err := s.store.ListGitCredentials(ctx)
	if err != nil {
		s.log.Warn("list existing git credentials", "error", err)
		return
	}
	existingMap := make(map[string]store.GitCredential, len(existingCreds))
	for _, c := range existingCreds {
		existingMap[c.Name] = c
	}

	for _, cred := range foundCreds {
		if cred.Secret == "" {
			continue
		}

		if existing, ok := existingMap[cred.Name]; ok {
			report.Credentials = append(report.Credentials, DiscoveredCredential{
				ID:       existing.ID,
				Name:     existing.Name,
				Kind:     existing.Kind,
				Username: existing.Username,
				Action:   "existing",
			})
			continue
		}

		created, err := s.store.CreateGitCredential(
			ctx,
			s.sealer,
			cred.Name,
			cred.Kind,
			cred.Username,
			cred.Secret,
			actorUserID,
		)
		if err != nil {
			s.log.Warn("autosave git credential failed", "name", cred.Name, "error", err)
			report.Errors = append(report.Errors, fmt.Sprintf("autosave credential %s: %v", cred.Name, err))
			continue
		}

		existingMap[created.Name] = *created
		report.Credentials = append(report.Credentials, DiscoveredCredential{
			ID:       created.ID,
			Name:     created.Name,
			Kind:     created.Kind,
			Username: created.Username,
			Action:   "saved",
		})
		s.log.Info("autosaved git credential from server", "server", server.Name, "name", created.Name)
	}
}

func parseGitCredentialsOutput(output, serverName string) []rawGitCred {
	var out []rawGitCred
	seenNames := make(map[string]bool)

	sections := strings.Split(output, "===DOCKDEPLOY_CRED:")
	for _, sec := range sections {
		if sec == "" {
			continue
		}
		endIdx := strings.Index(sec, "===DOCKDEPLOY_CRED_END===")
		if endIdx == -1 {
			continue
		}
		block := sec[:endIdx]
		tagEnd := strings.Index(block, "===")
		if tagEnd == -1 {
			continue
		}
		tag := block[:tagEnd]
		content := strings.TrimSpace(block[tagEnd+3:])

		switch {
		case tag == "GIT_CREDENTIALS":
			lines := strings.Split(content, "\n")
			for _, line := range lines {
				line = strings.TrimSpace(line)
				if line == "" || strings.HasPrefix(line, "#") {
					continue
				}
				u, err := url.Parse(line)
				if err != nil || u.User == nil {
					continue
				}
				username := u.User.Username()
				secret, _ := u.User.Password()
				host := u.Host
				if secret == "" && username != "" && (strings.HasPrefix(username, "ghp_") || strings.HasPrefix(username, "glpat-") || len(username) > 20) {
					secret = username
					username = "oauth2"
				}
				if secret == "" {
					continue
				}

				name := fmt.Sprintf("Server %s: %s (%s)", serverName, host, username)
				if len(name) > 90 {
					name = name[:90]
				}
				if seenNames[name] {
					continue
				}
				seenNames[name] = true

				out = append(out, rawGitCred{
					Name:     name,
					Kind:     gitx.KindToken,
					Username: username,
					Secret:   secret,
				})
			}

		case tag == "NETRC":
			tokens := strings.Fields(content)
			var curMachine, curLogin, curPass string
			for i := 0; i < len(tokens); i++ {
				switch tokens[i] {
				case "machine", "default":
					if curMachine != "" && curPass != "" {
						name := fmt.Sprintf("Server %s: netrc (%s)", serverName, curMachine)
						if len(name) > 90 {
							name = name[:90]
						}
						if !seenNames[name] {
							seenNames[name] = true
							out = append(out, rawGitCred{
								Name:     name,
								Kind:     gitx.KindToken,
								Username: curLogin,
								Secret:   curPass,
							})
						}
						curLogin = ""
						curPass = ""
					}
					if i+1 < len(tokens) {
						curMachine = tokens[i+1]
						i++
					}
				case "login":
					if i+1 < len(tokens) {
						curLogin = tokens[i+1]
						i++
					}
				case "password":
					if i+1 < len(tokens) {
						curPass = tokens[i+1]
						i++
					}
				}
			}
			if curMachine != "" && curPass != "" {
				name := fmt.Sprintf("Server %s: netrc (%s)", serverName, curMachine)
				if len(name) > 90 {
					name = name[:90]
				}
				if !seenNames[name] {
					seenNames[name] = true
					out = append(out, rawGitCred{
						Name:     name,
						Kind:     gitx.KindToken,
						Username: curLogin,
						Secret:   curPass,
					})
				}
			}

		case strings.HasPrefix(tag, "SSH_KEY:"):
			keyName := strings.TrimPrefix(tag, "SSH_KEY:")
			if strings.Contains(content, "-----BEGIN ") && strings.Contains(content, "PRIVATE KEY-----") {
				name := fmt.Sprintf("Server %s: SSH Key (%s)", serverName, keyName)
				if len(name) > 90 {
					name = name[:90]
				}
				if !seenNames[name] {
					seenNames[name] = true
					out = append(out, rawGitCred{
						Name:     name,
						Kind:     gitx.KindSSHKey,
						Username: "git",
						Secret:   content,
					})
				}
			}
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// 2. Nginx Domains Discovery
// ---------------------------------------------------------------------------

type parsedVHost struct {
	Hostname     string
	UpstreamPort int
	SSLMode      store.DomainSSLMode
	WebSocket    bool
	Config       string
	SourceFile   string
}

func (s *Service) discoverDomains(
	ctx context.Context,
	conn *sshx.Conn,
	server *store.Server,
	sudoPassword string,
	actorUserID string,
	report *Report,
) {
	// Read Nginx configuration files from sites-enabled, conf.d, and sites-available
	probeScript := `
sh -c '
for d in /etc/nginx/sites-enabled /etc/nginx/conf.d; do
    if [ -d "$d" ]; then
        for f in "$d"/*; do
            if [ -f "$f" ]; then
                echo "===DOCKDEPLOY_NGINX_FILE:$f==="
                cat "$f" 2>/dev/null
                echo "===DOCKDEPLOY_NGINX_END==="
            fi
        done
    fi
done

# If nothing in sites-enabled or conf.d, also inspect sites-available
if [ -d "/etc/nginx/sites-available" ]; then
    for f in /etc/nginx/sites-available/*; do
        if [ -f "$f" ]; then
            echo "===DOCKDEPLOY_NGINX_FILE:$f==="
            cat "$f" 2>/dev/null
            echo "===DOCKDEPLOY_NGINX_END==="
        fi
    done
fi
'
`
	// Try standard run first
	result, err := conn.Run(ctx, probeScript)
	outText := ""
	if err == nil && result.Ok() && strings.Contains(result.Stdout, "===DOCKDEPLOY_NGINX_FILE:") {
		outText = result.Stdout
	} else if sudoPassword != "" {
		// Run with sudo if standard read was permission-denied
		sudoCmd := fmt.Sprintf("printf '%%s\\n' %s | sudo -S -p '' sh -c %s",
			shellQuote(sudoPassword), shellQuote(probeScript))
		sRes, sErr := conn.Run(ctx, sudoCmd)
		if sErr == nil && sRes.Ok() {
			outText = sRes.Stdout
		}
	}

	if outText == "" {
		return
	}

	vhosts := parseNginxConfigs(outText)
	if len(vhosts) == 0 {
		return
	}

	// Fetch existing domains for this server
	existingDomains, err := s.store.ListDomainsByServer(ctx, server.ID)
	if err != nil {
		s.log.Warn("list domains by server", "server", server.Name, "error", err)
		return
	}
	existingMap := make(map[string]store.Domain, len(existingDomains))
	for _, d := range existingDomains {
		existingMap[strings.ToLower(d.Hostname)] = d
	}

	for _, vh := range vhosts {
		hostname := strings.ToLower(vh.Hostname)
		existing, ok := existingMap[hostname]

		if ok {
			// If domain exists and has empty config or error status, update it
			if existing.Status != store.DomainStatusActive || existing.ConfigRendered == "" {
				cfg := vh.Config
				_ = s.store.UpdateDomainStatus(ctx, existing.ID, store.UpdateDomainParams{
					ConfigRendered: &cfg,
					Status:         store.DomainStatusActive,
					StatusMessage:  "Auto-detected from " + filepath.Base(vh.SourceFile),
				})
				report.Domains = append(report.Domains, DiscoveredDomain{
					ID:           existing.ID,
					Hostname:     existing.Hostname,
					UpstreamPort: existing.UpstreamPort,
					SSLMode:      existing.SSLMode,
					Action:       "updated",
				})
			} else {
				report.Domains = append(report.Domains, DiscoveredDomain{
					ID:           existing.ID,
					Hostname:     existing.Hostname,
					UpstreamPort: existing.UpstreamPort,
					SSLMode:      existing.SSLMode,
					Action:       "existing",
				})
			}
			continue
		}

		// Create domain if not in store
		created, err := s.store.CreateDomain(ctx, store.NewDomain{
			ServerID:       server.ID,
			Hostname:       hostname,
			UpstreamPort:   vh.UpstreamPort,
			SSLMode:        vh.SSLMode,
			WebSocket:      vh.WebSocket,
			ConfigRendered: vh.Config,
			Status:         store.DomainStatusActive,
			StatusMessage:  "Auto-detected from " + filepath.Base(vh.SourceFile),
			CreatedBy:      actorUserID,
		})
		if err != nil {
			s.log.Warn("create auto-detected domain", "hostname", hostname, "error", err)
			report.Errors = append(report.Errors, fmt.Sprintf("import domain %s: %v", hostname, err))
			continue
		}

		existingMap[hostname] = *created
		report.Domains = append(report.Domains, DiscoveredDomain{
			ID:           created.ID,
			Hostname:     created.Hostname,
			UpstreamPort: created.UpstreamPort,
			SSLMode:      created.SSLMode,
			Action:       "created",
		})
		s.log.Info("auto-detected domain", "server", server.Name, "hostname", hostname, "port", vh.UpstreamPort)
	}
}

func parseNginxConfigs(output string) []parsedVHost {
	type fileEntry struct {
		path    string
		content string
	}

	var files []fileEntry
	sections := strings.Split(output, "===DOCKDEPLOY_NGINX_FILE:")
	for _, sec := range sections {
		if sec == "" {
			continue
		}
		endIdx := strings.Index(sec, "===DOCKDEPLOY_NGINX_END===")
		if endIdx == -1 {
			continue
		}
		block := sec[:endIdx]
		tagEnd := strings.Index(block, "===")
		if tagEnd == -1 {
			continue
		}
		filePath := block[:tagEnd]
		content := strings.TrimSpace(block[tagEnd+3:])
		files = append(files, fileEntry{path: filePath, content: content})
	}

	// Map of hostname -> combined vhost info
	domainMap := make(map[string]*parsedVHost)

	for _, fe := range files {
		blocks := extractServerBlocks(fe.content)
		if len(blocks) == 0 {
			// If no server blocks found, treat entire file as one if it contains directives
			if strings.Contains(fe.content, "server_name") || strings.Contains(fe.content, "proxy_pass") {
				blocks = []string{fe.content}
			}
		}

		for _, blk := range blocks {
			names := extractServerNames(blk)
			if len(names) == 0 {
				continue
			}

			port := extractUpstreamPort(blk, fe.content)
			_, isLE := checkSSLDirectives(blk)
			ws := checkWebSocket(blk)

			for _, name := range names {
				name = strings.ToLower(name)
				existing, exists := domainMap[name]
				if !exists {
					sslMode := store.DomainSSLNone
					if isLE {
						sslMode = store.DomainSSLLetsEncrypt
					}
					domainMap[name] = &parsedVHost{
						Hostname:     name,
						UpstreamPort: port,
						SSLMode:      sslMode,
						WebSocket:    ws,
						Config:       fe.content,
						SourceFile:   fe.path,
					}
				} else {
					// Merge: if second block has proxy_pass port, use it
					if existing.UpstreamPort == 80 && port != 80 {
						existing.UpstreamPort = port
					}
					if isLE {
						existing.SSLMode = store.DomainSSLLetsEncrypt
					}
					if ws {
						existing.WebSocket = true
					}
				}
			}
		}
	}

	var results []parsedVHost
	for _, vh := range domainMap {
		results = append(results, *vh)
	}
	return results
}

func extractServerBlocks(content string) []string {
	var blocks []string
	idx := 0
	for {
		serverIdx := strings.Index(content[idx:], "server")
		if serverIdx == -1 {
			break
		}
		absServerIdx := idx + serverIdx
		rest := content[absServerIdx+6:]
		trimmed := strings.TrimLeft(rest, " \t\r\n")
		if !strings.HasPrefix(trimmed, "{") {
			idx = absServerIdx + 6
			continue
		}

		braceStart := absServerIdx + 6 + (len(rest) - len(trimmed))
		depth := 0
		end := -1
		for i := braceStart; i < len(content); i++ {
			if content[i] == '{' {
				depth++
			} else if content[i] == '}' {
				depth--
				if depth == 0 {
					end = i + 1
					break
				}
			}
		}
		if end != -1 {
			blocks = append(blocks, content[absServerIdx:end])
			idx = end
		} else {
			break
		}
	}
	return blocks
}

var serverNameRegex = regexp.MustCompile(`(?i)(?:^|;|\s)server_name\s+([^;]+);`)
var domainRegex = regexp.MustCompile(`^[a-z0-9]([a-z0-9-.]*[a-z0-9])?$`)

func extractServerNames(block string) []string {
	matches := serverNameRegex.FindAllStringSubmatch(block, -1)
	var names []string
	for _, m := range matches {
		if len(m) < 2 {
			continue
		}
		tokens := strings.Fields(m[1])
		for _, tok := range tokens {
			tok = strings.ToLower(strings.TrimSpace(tok))
			if tok == "" || tok == "_" || tok == "localhost" || tok == "127.0.0.1" || tok == "default_server" {
				continue
			}
			// Must look like a domain name
			if strings.Contains(tok, ".") && domainRegex.MatchString(tok) {
				names = append(names, tok)
			}
		}
	}
	return names
}

var proxyPassRegex = regexp.MustCompile(`(?i)proxy_pass\s+([^;]+);`)
var portRegex = regexp.MustCompile(`:(?P<port>[0-9]{2,5})`)

func extractUpstreamPort(block, fullFile string) int {
	matches := proxyPassRegex.FindAllStringSubmatch(block, -1)
	for _, m := range matches {
		if len(m) < 2 {
			continue
		}
		val := strings.TrimSpace(m[1])
		portMatch := portRegex.FindStringSubmatch(val)
		if len(portMatch) > 1 {
			p, err := strconv.Atoi(portMatch[1])
			if err == nil && p >= 1 && p <= 65535 {
				return p
			}
		}

		// Check if proxy_pass references an upstream block in full file
		upstreamName := strings.TrimPrefix(val, "http://")
		upstreamName = strings.TrimPrefix(upstreamName, "https://")
		upstreamName = strings.TrimRight(upstreamName, "/")
		if !strings.Contains(upstreamName, ".") && !strings.Contains(upstreamName, ":") {
			upRegex := regexp.MustCompile(fmt.Sprintf(`(?i)upstream\s+%s\s*\{[^}]*server\s+[^:]+:([0-9]+)`, regexp.QuoteMeta(upstreamName)))
			upMatch := upRegex.FindStringSubmatch(fullFile)
			if len(upMatch) > 1 {
				p, err := strconv.Atoi(upMatch[1])
				if err == nil && p >= 1 && p <= 65535 {
					return p
				}
			}
		}

		if strings.HasPrefix(val, "https://") {
			return 443
		}
	}
	return 80
}

func checkSSLDirectives(block string) (hasSSL bool, isLetsEncrypt bool) {
	lower := strings.ToLower(block)
	if strings.Contains(lower, "ssl_certificate") {
		hasSSL = true
		if strings.Contains(lower, "letsencrypt") {
			isLetsEncrypt = true
		}
	}
	if strings.Contains(lower, "listen") && strings.Contains(lower, "443") {
		hasSSL = true
	}
	return hasSSL, isLetsEncrypt
}

func checkWebSocket(block string) bool {
	lower := strings.ToLower(block)
	return strings.Contains(lower, "$http_upgrade") || strings.Contains(lower, "upgrade")
}

// ---------------------------------------------------------------------------
// 3. Docker Deployments Discovery
// ---------------------------------------------------------------------------

func (s *Service) discoverDeployments(
	ctx context.Context,
	docker *dockerx.Client,
	conn *sshx.Conn,
	server *store.Server,
	actorUserID string,
	report *Report,
) {
	containers, err := docker.ListContainers(ctx)
	if err != nil {
		s.log.Warn("list containers for discovery", "server", server.Name, "error", err)
		report.Errors = append(report.Errors, fmt.Sprintf("list docker containers: %v", err))
		return
	}

	// Fetch existing deployments on this server
	existingDeployments, err := s.store.ListDeployments(ctx, "", true)
	if err != nil {
		s.log.Warn("list existing deployments", "error", err)
		return
	}

	serverDeployments := make(map[string]store.Deployment)
	allSlugs := make(map[string]bool)
	for _, d := range existingDeployments {
		allSlugs[d.Slug] = true
		if d.ServerID == server.ID {
			serverDeployments[d.Slug] = d
			serverDeployments[d.Name] = d
			if d.ID != "" {
				serverDeployments[d.ID] = d
			}
		}
	}

	// Group containers into Compose projects vs standalone
	composeProjects := make(map[string][]dockerx.Container)
	var standalone []dockerx.Container

	for _, c := range containers {
		// Ignore control plane container
		name := strings.TrimPrefix(c.Name, "/")
		if name == "dockdeploy" || strings.Contains(c.Image, "dockdeploy") {
			continue
		}

		if c.ComposeProject != "" {
			composeProjects[c.ComposeProject] = append(composeProjects[c.ComposeProject], c)
		} else {
			standalone = append(standalone, c)
		}
	}

	// Process Compose projects
	for projName, cList := range composeProjects {
		slug := slugify(projName)
		isRunning := false
		var hostPort *int
		containerPort := 80

		for _, c := range cList {
			if c.State == "running" {
				isRunning = true
			}
			for _, p := range c.Ports {
				if p.HostPort > 0 && hostPort == nil {
					hp := int(p.HostPort)
					hostPort = &hp
					containerPort = int(p.Container)
				}
			}
		}

		status := store.DeploymentStopped
		if isRunning {
			status = store.DeploymentRunning
		}

		existing, exists := serverDeployments[slug]
		if !exists {
			existing, exists = serverDeployments[projName]
		}

		if exists {
			if existing.Status != status {
				_ = s.store.UpdateDeploymentStatus(ctx, existing.ID, status, nil)
			}
			if existing.HostPort == nil && hostPort != nil {
				_ = s.store.SetDeploymentPort(ctx, existing.ID, *hostPort)
			}
			report.Deployments = append(report.Deployments, DiscoveredDeployment{
				ID:         existing.ID,
				Name:       existing.Name,
				Slug:       existing.Slug,
				SourceType: existing.SourceType,
				Status:     status,
				Action:     "existing",
			})
			continue
		}

		// Try reading compose file content from host if labels exist
		composeContent := ""
		workdir := ""
		composePath := ""

		// Inspect the first container to find compose labels
		if details, err := docker.InspectContainer(ctx, cList[0].ID); err == nil {
			workdir = details.Config.Labels["com.docker.compose.project.working_dir"]
			composePath = details.Config.Labels["com.docker.compose.project.config_files"]
			if composePath != "" && conn != nil {
				res, err := conn.Run(ctx, fmt.Sprintf("cat %s 2>/dev/null", shellQuote(composePath)))
				if err == nil && res.Ok() && len(res.Stdout) > 0 {
					composeContent = res.Stdout
				}
			}
		}

		// Ensure slug uniqueness
		finalSlug := slug
		counter := 1
		for allSlugs[finalSlug] {
			finalSlug = fmt.Sprintf("%s-%d", slug, counter)
			counter++
		}
		allSlugs[finalSlug] = true

		created, err := s.store.CreateDeployment(ctx, s.sealer, store.NewDeployment{
			ServerID:       server.ID,
			Name:           projName,
			Slug:           finalSlug,
			SourceType:     store.SourceRawCompose,
			ComposeContent: composeContent,
			ComposePath:    composePath,
			Workdir:        workdir,
			HostPort:       hostPort,
			ContainerPort:  containerPort,
			WebhookSecret:  randomSecret(),
			CreatedBy:      actorUserID,
		})
		if err != nil {
			s.log.Warn("create auto-detected compose deployment", "project", projName, "error", err)
			report.Errors = append(report.Errors, fmt.Sprintf("import compose %s: %v", projName, err))
			continue
		}

		if status == store.DeploymentRunning {
			_ = s.store.UpdateDeploymentStatus(ctx, created.ID, store.DeploymentRunning, nil)
		}

		serverDeployments[finalSlug] = *created
		report.Deployments = append(report.Deployments, DiscoveredDeployment{
			ID:         created.ID,
			Name:       created.Name,
			Slug:       created.Slug,
			SourceType: created.SourceType,
			Status:     status,
			Action:     "created",
		})
		s.log.Info("auto-detected compose deployment", "server", server.Name, "name", projName)
	}

	// Process Standalone Containers
	for _, c := range standalone {
		name := strings.TrimPrefix(c.Name, "/")
		slug := slugify(name)
		var hostPort *int
		containerPort := 80

		for _, p := range c.Ports {
			if p.HostPort > 0 && hostPort == nil {
				hp := int(p.HostPort)
				hostPort = &hp
				containerPort = int(p.Container)
			}
		}

		status := store.DeploymentStopped
		if c.State == "running" {
			status = store.DeploymentRunning
		}

		existing, exists := serverDeployments[slug]
		if !exists {
			existing, exists = serverDeployments[name]
		}
		if !exists && c.DeploymentID != "" {
			existing, exists = serverDeployments[c.DeploymentID]
		}

		if exists {
			if existing.Status != status {
				_ = s.store.UpdateDeploymentStatus(ctx, existing.ID, status, nil)
			}
			if existing.HostPort == nil && hostPort != nil {
				_ = s.store.SetDeploymentPort(ctx, existing.ID, *hostPort)
			}
			report.Deployments = append(report.Deployments, DiscoveredDeployment{
				ID:         existing.ID,
				Name:       existing.Name,
				Slug:       existing.Slug,
				SourceType: existing.SourceType,
				Status:     status,
				Action:     "existing",
			})
			continue
		}

		// Ensure slug uniqueness
		finalSlug := slug
		counter := 1
		for allSlugs[finalSlug] {
			finalSlug = fmt.Sprintf("%s-%d", slug, counter)
			counter++
		}
		allSlugs[finalSlug] = true

		created, err := s.store.CreateDeployment(ctx, s.sealer, store.NewDeployment{
			ServerID:      server.ID,
			Name:          name,
			Slug:          finalSlug,
			SourceType:    store.SourceImage,
			ImageRef:      c.Image,
			HostPort:      hostPort,
			ContainerPort: containerPort,
			WebhookSecret: randomSecret(),
			CreatedBy:     actorUserID,
		})
		if err != nil {
			s.log.Warn("create auto-detected image deployment", "name", name, "error", err)
			report.Errors = append(report.Errors, fmt.Sprintf("import container %s: %v", name, err))
			continue
		}

		if status == store.DeploymentRunning {
			_ = s.store.UpdateDeploymentStatus(ctx, created.ID, store.DeploymentRunning, nil)
		}

		serverDeployments[finalSlug] = *created
		report.Deployments = append(report.Deployments, DiscoveredDeployment{
			ID:         created.ID,
			Name:       created.Name,
			Slug:       created.Slug,
			SourceType: created.SourceType,
			Status:     status,
			Action:     "created",
		})
		s.log.Info("auto-detected container deployment", "server", server.Name, "name", name)
	}
}

// ---------------------------------------------------------------------------
// 4. Link Domains to Deployments
// ---------------------------------------------------------------------------

func (s *Service) linkDomainsAndDeployments(ctx context.Context, serverID string) {
	domains, err := s.store.ListDomainsByServer(ctx, serverID)
	if err != nil {
		return
	}
	deployments, err := s.store.ListDeployments(ctx, "", true)
	if err != nil {
		return
	}

	// Build port to deployment ID map for this server
	portToDep := make(map[int]string)
	for _, d := range deployments {
		if d.ServerID == serverID && d.HostPort != nil {
			portToDep[*d.HostPort] = d.ID
		}
	}

	for _, dom := range domains {
		if dom.DeploymentID == nil && dom.UpstreamPort > 0 {
			if depID, ok := portToDep[dom.UpstreamPort]; ok {
				depIDStr := depID
				_ = s.store.UpdateDomainStatus(ctx, dom.ID, store.UpdateDomainParams{
					DeploymentID:  &depIDStr,
					Status:        dom.Status,
					StatusMessage: dom.StatusMessage,
				})
				s.log.Info("linked domain to deployment", "domain", dom.Hostname, "deployment", depID)
			}
		}
	}
}

func slugify(name string) string {
	lowered := strings.ToLower(strings.TrimSpace(name))
	var builder strings.Builder
	lastDash := false
	for _, r := range lowered {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			builder.WriteRune(r)
			lastDash = false
		case !lastDash && builder.Len() > 0:
			builder.WriteByte('-')
			lastDash = true
		}
	}
	slug := strings.Trim(builder.String(), "-")
	if len(slug) > 50 {
		slug = strings.Trim(slug[:50], "-")
	}
	if slug == "" {
		slug = "app"
	}
	return slug
}

func randomSecret() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
