package servers

import (
	"bufio"
	"context"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/sirtheprogrammer/docker-deployments/server/internal/dockerx"
	"github.com/sirtheprogrammer/docker-deployments/server/internal/sshx"
	"github.com/sirtheprogrammer/docker-deployments/server/internal/store"
)

type SecuritySeverity string

const (
	SeverityCritical SecuritySeverity = "critical"
	SeverityHigh     SecuritySeverity = "high"
	SeverityMedium   SecuritySeverity = "medium"
	SeverityLow      SecuritySeverity = "low"
	SeverityInfo     SecuritySeverity = "info"
)

type SecurityFinding struct {
	ID          string           `json:"id"`
	Title       string           `json:"title"`
	Category    string           `json:"category"` // "SSH & Authentication", "Network & Ports", "Docker & Containers", "Web & Reverse Proxy", "System Integrity"
	Severity    SecuritySeverity `json:"severity"`
	Description string           `json:"description"`
	Evidence    string           `json:"evidence"`
	Remediation string           `json:"remediation"`
}

type IPCount struct {
	IP    string `json:"ip"`
	Count int    `json:"count"`
}

type SSHAttackAnalysis struct {
	FailedAttemptsCount int       `json:"failed_attempts_count"`
	TopAttackerIPs      []IPCount `json:"top_attacker_ips"`
	InvalidUserAttempts int       `json:"invalid_user_attempts"`
	RootLoginAttempts   int       `json:"root_login_attempts"`
	RecentAttacks       []string  `json:"recent_attacks"`
}

type ExposedPort struct {
	Protocol string `json:"protocol"`
	Port     int    `json:"port"`
	BindAddr string `json:"bind_addr"`
	Process  string `json:"process"`
	IsPublic bool   `json:"is_public"`
	IsRisky  bool   `json:"is_risky"`
	RiskNote string `json:"risk_note"`
}

type ContainerSecurityRisk struct {
	ContainerID   string   `json:"container_id"`
	Name          string   `json:"name"`
	Image         string   `json:"image"`
	IsPrivileged  bool     `json:"is_privileged"`
	HostNetwork   bool     `json:"host_network"`
	HostPID       bool     `json:"host_pid"`
	RootUser      bool     `json:"root_user"`
	SocketMounted bool     `json:"socket_mounted"`
	Risks         []string `json:"risks"`
}

type WebAttackSummary struct {
	ScansDetected            int      `json:"scans_detected"`
	SQLiAttempts             int      `json:"sqli_attempts"`
	PathTraversalAttempts    int      `json:"path_traversal_attempts"`
	CommandInjectionAttempts int      `json:"command_injection_attempts"`
	RecentWebAttacks         []string `json:"recent_web_attacks"`
}

type SystemIntegritySummary struct {
	FirewallStatus      string   `json:"firewall_status"`
	SuspiciousProcesses []string `json:"suspicious_processes"`
	WorldWritableFiles  []string `json:"world_writable_files"`
	SUIDBinariesCount   int      `json:"suid_binaries_count"`
}

type ServerSecurityAudit struct {
	ServerID        string                   `json:"server_id"`
	ServerName      string                   `json:"server_name"`
	ScannedAt       time.Time                `json:"scanned_at"`
	Score           int                      `json:"score"` // 0-100
	Grade           string                   `json:"grade"` // "A", "B", "C", "D", "F"
	Status          string                   `json:"status"` // "secure", "warning", "critical"
	Summary         string                   `json:"summary"`
	CriticalCount   int                      `json:"critical_count"`
	HighCount       int                      `json:"high_count"`
	MediumCount     int                      `json:"medium_count"`
	LowCount        int                      `json:"low_count"`
	InfoCount       int                      `json:"info_count"`
	Findings        []SecurityFinding        `json:"findings"`
	SSHAnalysis     SSHAttackAnalysis        `json:"ssh_analysis"`
	ExposedPorts    []ExposedPort            `json:"exposed_ports"`
	ContainerRisks  []ContainerSecurityRisk  `json:"container_risks"`
	WebAttacks      WebAttackSummary         `json:"web_attacks"`
	SystemIntegrity SystemIntegritySummary   `json:"system_integrity"`
}

const securityAuditScript = `echo "===SECTION:SSHLOGS==="
(grep -iE "Failed password|Invalid user|authentication failure" /var/log/auth.log /var/log/secure 2>/dev/null || journalctl -u ssh -u sshd -n 300 --no-pager 2>/dev/null | grep -iE "Failed password|Invalid user|authentication failure" || true) | tail -n 150

echo "===SECTION:PORTS==="
(ss -Htlpn 2>/dev/null || ss -tlpn 2>/dev/null || netstat -tlpn 2>/dev/null) || true

echo "===SECTION:FIREWALL==="
(ufw status 2>/dev/null || iptables -L -n 2>/dev/null | head -n 15 || nft list ruleset 2>/dev/null | head -n 15 || echo "disabled")

echo "===SECTION:TMP_EXECUTABLES==="
find /tmp /var/tmp /dev/shm -maxdepth 3 -type f -perm /111 2>/dev/null | head -n 20 || true

echo "===SECTION:SUSPICIOUS_PROCS==="
ps -eo user,pid,%cpu,%mem,args 2>/dev/null | grep -iE 'miner|xmrig|stratum|minerd|masscan|zmap|netcat|nc -l|ncat -l' | grep -v grep | head -n 15 || true

echo "===SECTION:NGINX_LOGS==="
(tail -n 300 /var/log/nginx/access.log /var/log/nginx/error.log 2>/dev/null || docker logs --tail 300 nginx 2>/dev/null || true) | grep -iE 'union.*select|\.\./|\.\.%2f|/etc/passwd|wp-login|xmlrpc|phpmyadmin|\.env|\.git/config|jndi:|/bin/sh' | tail -n 50 || true

echo "===SECTION:SUID==="
find /usr/bin /usr/sbin /bin /sbin -perm -4000 2>/dev/null | wc -l || true
`

// SecurityAudit inspects a managed server for active attack patterns, dangerous port bindings,
// Docker container privilege escalations, and system integrity gaps.
func (m *Manager) SecurityAudit(ctx context.Context, server *store.Server) (*ServerSecurityAudit, error) {
	conn, err := m.Connect(ctx, server)
	if err != nil {
		return nil, err
	}
	defer conn.Release()

	cmdRes, err := conn.Run(ctx, securityAuditScript)
	if err != nil {
		return nil, fmt.Errorf("execute security audit script: %w", err)
	}

	audit := parseSecurityAuditOutput(cmdRes.Stdout)
	audit.ServerID = server.ID
	audit.ServerName = server.Name
	audit.ScannedAt = time.Now().UTC()

	// Inspect Docker container configurations for security risks
	m.auditDockerContainers(ctx, conn, server, audit)

	// Calculate overall security score, grade, and status
	audit.calculateScoreAndFindings()

	return audit, nil
}

func (m *Manager) auditDockerContainers(ctx context.Context, conn *sshx.Conn, server *store.Server, audit *ServerSecurityAudit) {
	client, err := dockerx.New(ctx, conn, server.DockerSocket)
	if err != nil {
		return
	}
	defer client.Close()

	containers, err := client.ListContainers(ctx)
	if err != nil {
		return
	}

	for _, c := range containers {
		inspect, err := client.InspectContainer(ctx, c.ID)
		if err != nil {
			continue
		}

		risk := ContainerSecurityRisk{
			ContainerID: c.ID,
			Name:        c.Name,
			Image:       c.Image,
			Risks:       []string{},
		}

		if inspect.HostConfig != nil {
			if inspect.HostConfig.Privileged {
				risk.IsPrivileged = true
				risk.Risks = append(risk.Risks, "Running in privileged mode (--privileged), bypassing host Linux kernel isolation")
			}
			if strings.EqualFold(string(inspect.HostConfig.NetworkMode), "host") {
				risk.HostNetwork = true
				risk.Risks = append(risk.Risks, "Using host network stack (--net=host), exposing internal services directly")
			}
			if strings.EqualFold(string(inspect.HostConfig.PidMode), "host") {
				risk.HostPID = true
				risk.Risks = append(risk.Risks, "Sharing host PID namespace (--pid=host), able to signal host processes")
			}
		}

		for _, mount := range inspect.Mounts {
			if mount.Source == "/var/run/docker.sock" {
				risk.SocketMounted = true
				risk.Risks = append(risk.Risks, "Host Docker socket (/var/run/docker.sock) mounted into container (container escape risk)")
			}
			if mount.Source == "/" || mount.Source == "/etc" {
				if mount.RW {
					risk.Risks = append(risk.Risks, fmt.Sprintf("Root host filesystem %s mounted with read-write access", mount.Source))
				}
			}
		}

		user := ""
		if inspect.Config != nil {
			user = inspect.Config.User
		}
		if user == "" || user == "0" || strings.EqualFold(user, "root") {
			risk.RootUser = true
			risk.Risks = append(risk.Risks, "Running as root user inside container")
		}

		if len(risk.Risks) > 0 {
			audit.ContainerRisks = append(audit.ContainerRisks, risk)
		}
	}
}

func parseSecurityAuditOutput(raw string) *ServerSecurityAudit {
	audit := &ServerSecurityAudit{
		Findings:        []SecurityFinding{},
		ExposedPorts:    []ExposedPort{},
		ContainerRisks:  []ContainerSecurityRisk{},
		SSHAnalysis:     SSHAttackAnalysis{TopAttackerIPs: []IPCount{}, RecentAttacks: []string{}},
		WebAttacks:      WebAttackSummary{RecentWebAttacks: []string{}},
		SystemIntegrity: SystemIntegritySummary{SuspiciousProcesses: []string{}, WorldWritableFiles: []string{}},
	}

	sections := splitSections(raw)

	// 1. Parse SSH Logs
	if sshRaw, ok := sections["SSHLOGS"]; ok {
		parseSSHLogs(sshRaw, &audit.SSHAnalysis)
	}

	// 2. Parse Listening Ports
	if portsRaw, ok := sections["PORTS"]; ok {
		audit.ExposedPorts = parseExposedPorts(portsRaw)
	}

	// 3. Parse Firewall
	if fwRaw, ok := sections["FIREWALL"]; ok {
		fwLower := strings.ToLower(fwRaw)
		if strings.Contains(fwLower, "status: active") {
			audit.SystemIntegrity.FirewallStatus = "active (ufw)"
		} else if strings.Contains(fwLower, "chain input") && !strings.Contains(fwLower, "0 references") {
			audit.SystemIntegrity.FirewallStatus = "active (iptables)"
		} else if strings.Contains(fwLower, "table inet") || strings.Contains(fwLower, "table ip") {
			audit.SystemIntegrity.FirewallStatus = "active (nftables)"
		} else {
			audit.SystemIntegrity.FirewallStatus = "disabled"
		}
	}

	// 4. Parse Temp Executables
	if tmpRaw, ok := sections["TMP_EXECUTABLES"]; ok {
		scanner := bufio.NewScanner(strings.NewReader(tmpRaw))
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line != "" {
				audit.SystemIntegrity.WorldWritableFiles = append(audit.SystemIntegrity.WorldWritableFiles, line)
			}
		}
	}

	// 5. Parse Suspicious Processes
	if procRaw, ok := sections["SUSPICIOUS_PROCS"]; ok {
		scanner := bufio.NewScanner(strings.NewReader(procRaw))
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line != "" {
				audit.SystemIntegrity.SuspiciousProcesses = append(audit.SystemIntegrity.SuspiciousProcesses, line)
			}
		}
	}

	// 6. Parse Web & Nginx Attacks
	if webRaw, ok := sections["NGINX_LOGS"]; ok {
		parseWebAttacks(webRaw, &audit.WebAttacks)
	}

	// 7. Parse SUID count
	if suidRaw, ok := sections["SUID"]; ok {
		val, _ := strconv.Atoi(strings.TrimSpace(suidRaw))
		audit.SystemIntegrity.SUIDBinariesCount = val
	}

	return audit
}

func parseSSHLogs(raw string, ssh *SSHAttackAnalysis) {
	ipMap := make(map[string]int)
	ipRegex := regexp.MustCompile(`(?:from\s+)([0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3})`)

	scanner := bufio.NewScanner(strings.NewReader(raw))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		lineLower := strings.ToLower(line)
		if strings.Contains(lineLower, "failed password") || strings.Contains(lineLower, "authentication failure") {
			ssh.FailedAttemptsCount++
			if strings.Contains(lineLower, "root") {
				ssh.RootLoginAttempts++
			}
		}
		if strings.Contains(lineLower, "invalid user") {
			ssh.InvalidUserAttempts++
		}

		matches := ipRegex.FindStringSubmatch(line)
		if len(matches) > 1 {
			ip := matches[1]
			ipMap[ip]++
		}

		if len(ssh.RecentAttacks) < 15 {
			ssh.RecentAttacks = append(ssh.RecentAttacks, line)
		}
	}

	for ip, count := range ipMap {
		ssh.TopAttackerIPs = append(ssh.TopAttackerIPs, IPCount{IP: ip, Count: count})
	}
	sort.Slice(ssh.TopAttackerIPs, func(i, j int) bool {
		return ssh.TopAttackerIPs[i].Count > ssh.TopAttackerIPs[j].Count
	})
	if len(ssh.TopAttackerIPs) > 10 {
		ssh.TopAttackerIPs = ssh.TopAttackerIPs[:10]
	}
}

func parseExposedPorts(raw string) []ExposedPort {
	var list []ExposedPort
	scanner := bufio.NewScanner(strings.NewReader(raw))

	// Risky ports that should rarely be bound to 0.0.0.0 publicly
	riskyPorts := map[int]string{
		2375:  "Docker Engine unauthenticated remote API exposed publicly without TLS",
		2376:  "Docker Engine TLS port exposed publicly",
		3306:  "MySQL database service exposed to public network",
		5432:  "PostgreSQL database service exposed to public network",
		6379:  "Redis in-memory store exposed without network isolation (critical cryptominer vector)",
		27017: "MongoDB database service exposed to public network",
		9200:  "Elasticsearch REST interface exposed to public network",
		11211: "Memcached service exposed publicly (DDoS amplification risk)",
	}

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "State") || strings.HasPrefix(line, "Proto") {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}

		proto := "tcp"
		addrField := ""
		procField := ""

		if strings.HasPrefix(fields[0], "tcp") || strings.HasPrefix(fields[0], "udp") || fields[0] == "LISTEN" {
			for _, f := range fields {
				if strings.Contains(f, ":") && !strings.Contains(f, "users:") && addrField == "" {
					addrField = f
				} else if strings.HasPrefix(f, "users:") || strings.Contains(f, "/") {
					procField = f
				}
			}
		}

		if addrField == "" {
			continue
		}

		lastColon := strings.LastIndex(addrField, ":")
		if lastColon == -1 {
			continue
		}

		bindAddr := addrField[:lastColon]
		portStr := addrField[lastColon+1:]
		port, err := strconv.Atoi(portStr)
		if err != nil || port <= 0 {
			continue
		}

		isPublic := bindAddr == "*" || bindAddr == "0.0.0.0" || bindAddr == "::" || bindAddr == "[::]" || strings.HasPrefix(bindAddr, "%")
		riskNote, isRisky := riskyPorts[port]

		list = append(list, ExposedPort{
			Protocol: proto,
			Port:     port,
			BindAddr: bindAddr,
			Process:  cleanProcField(procField),
			IsPublic: isPublic,
			IsRisky:  isPublic && isRisky,
			RiskNote: riskNote,
		})
	}

	return list
}

func cleanProcField(raw string) string {
	if raw == "" {
		return "-"
	}
	re := regexp.MustCompile(`"([^"]+)"`)
	m := re.FindStringSubmatch(raw)
	if len(m) > 1 {
		return m[1]
	}
	return raw
}

func parseWebAttacks(raw string, web *WebAttackSummary) {
	scanner := bufio.NewScanner(strings.NewReader(raw))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		lineLower := strings.ToLower(line)
		if strings.Contains(lineLower, "union") && strings.Contains(lineLower, "select") {
			web.SQLiAttempts++
		}
		if strings.Contains(lineLower, "../") || strings.Contains(lineLower, "..%2f") || strings.Contains(lineLower, "/etc/passwd") {
			web.PathTraversalAttempts++
		}
		if strings.Contains(lineLower, "/bin/sh") || strings.Contains(lineLower, "jndi:") {
			web.CommandInjectionAttempts++
		}
		if strings.Contains(lineLower, "wp-login") || strings.Contains(lineLower, "xmlrpc") || strings.Contains(lineLower, "phpmyadmin") || strings.Contains(lineLower, ".env") {
			web.ScansDetected++
		}

		if len(web.RecentWebAttacks) < 15 {
			web.RecentWebAttacks = append(web.RecentWebAttacks, line)
		}
	}
}

func (a *ServerSecurityAudit) calculateScoreAndFindings() {
	score := 100

	// 1. Docker risks
	for _, c := range a.ContainerRisks {
		if c.IsPrivileged {
			a.Findings = append(a.Findings, SecurityFinding{
				ID:          fmt.Sprintf("docker-privileged-%s", c.ContainerID[:min(12, len(c.ContainerID))]),
				Title:       fmt.Sprintf("Container '%s' runs in Privileged Mode", c.Name),
				Category:    "Docker & Containers",
				Severity:    SeverityCritical,
				Description: "Privileged containers have full host device access and Linux capabilities, nullifying Docker container isolation.",
				Evidence:    fmt.Sprintf("Container %s (%s) has HostConfig.Privileged=true", c.Name, c.Image),
				Remediation: "Remove --privileged from the container run configuration and only grant specific Linux capabilities (e.g. --cap-add=NET_ADMIN).",
			})
			score -= 25
		}

		if c.SocketMounted {
			a.Findings = append(a.Findings, SecurityFinding{
				ID:          fmt.Sprintf("docker-socket-%s", c.ContainerID[:min(12, len(c.ContainerID))]),
				Title:       fmt.Sprintf("Docker Socket Mounted in Container '%s'", c.Name),
				Category:    "Docker & Containers",
				Severity:    SeverityHigh,
				Description: "Mounting /var/run/docker.sock grants root-equivalent control over the host Docker daemon, allowing container escapes.",
				Evidence:    fmt.Sprintf("Volume mount /var/run/docker.sock found on container %s", c.Name),
				Remediation: "Avoid mounting docker.sock into web-facing or untrusted containers. Use a read-only socket proxy if Docker control is required.",
			})
			score -= 15
		}

		if c.HostNetwork {
			a.Findings = append(a.Findings, SecurityFinding{
				ID:          fmt.Sprintf("docker-hostnet-%s", c.ContainerID[:min(12, len(c.ContainerID))]),
				Title:       fmt.Sprintf("Container '%s' Uses Host Network Stack", c.Name),
				Category:    "Docker & Containers",
				Severity:    SeverityMedium,
				Description: "Using host network mode bypasses Docker port mapping and exposes container ports directly on all host network interfaces.",
				Evidence:    fmt.Sprintf("Container %s has NetworkMode='host'", c.Name),
				Remediation: "Use standard bridge networks with explicit port mappings instead of --net=host.",
			})
			score -= 8
		}
	}

	// 2. Open Risky Ports
	for _, p := range a.ExposedPorts {
		if p.IsRisky {
			sev := SeverityHigh
			deduct := 15
			if p.Port == 2375 {
				sev = SeverityCritical
				deduct = 25
			}
			a.Findings = append(a.Findings, SecurityFinding{
				ID:          fmt.Sprintf("port-exposed-%d", p.Port),
				Title:       fmt.Sprintf("Risky Port %d/TCP Exposed to Public Network", p.Port),
				Category:    "Network & Ports",
				Severity:    sev,
				Description: p.RiskNote,
				Evidence:    fmt.Sprintf("Listening on %s:%d (Process: %s)", p.BindAddr, p.Port, p.Process),
				Remediation: fmt.Sprintf("Bind the service exclusively to localhost (127.0.0.1:%d) or block port %d using a firewall (ufw deny %d).", p.Port, p.Port, p.Port),
			})
			score -= deduct
		}
	}

	// 3. SSH Brute Force Attacks
	if a.SSHAnalysis.FailedAttemptsCount > 200 {
		a.Findings = append(a.Findings, SecurityFinding{
			ID:          "ssh-bruteforce-severe",
			Title:       fmt.Sprintf("Heavy SSH Brute-Force Activity Detected (%d failed logins)", a.SSHAnalysis.FailedAttemptsCount),
			Category:    "SSH & Authentication",
			Severity:    SeverityHigh,
			Description: "Automated botnets are actively attempting to guess SSH credentials on this server.",
			Evidence:    fmt.Sprintf("%d failed attempts from %d distinct IP addresses", a.SSHAnalysis.FailedAttemptsCount, len(a.SSHAnalysis.TopAttackerIPs)),
			Remediation: "Install and enable Fail2ban, disable password authentication in /etc/ssh/sshd_config (PasswordAuthentication no), and change the default SSH port.",
		})
		score -= 15
	} else if a.SSHAnalysis.FailedAttemptsCount > 20 {
		a.Findings = append(a.Findings, SecurityFinding{
			ID:          "ssh-bruteforce-moderate",
			Title:       fmt.Sprintf("SSH Authentication Probing Detected (%d failed logins)", a.SSHAnalysis.FailedAttemptsCount),
			Category:    "SSH & Authentication",
			Severity:    SeverityMedium,
			Description: "External scanners have attempted to authenticate using invalid or guessed credentials.",
			Evidence:    fmt.Sprintf("%d failed attempts logged", a.SSHAnalysis.FailedAttemptsCount),
			Remediation: "Enforce SSH key-based authentication only and consider deploying Fail2ban or Cloudflare Access.",
		})
		score -= 8
	}

	// 4. Firewall check
	if strings.Contains(strings.ToLower(a.SystemIntegrity.FirewallStatus), "disabled") {
		a.Findings = append(a.Findings, SecurityFinding{
			ID:          "firewall-inactive",
			Title:       "Host Firewall Appears Inactive or Not Configured",
			Category:    "System Integrity",
			Severity:    SeverityHigh,
			Description: "No active UFW, iptables, or nftables firewall rules were detected, leaving all listening host ports directly exposed.",
			Evidence:    "ufw status reported inactive",
			Remediation: "Enable UFW: sudo ufw default deny incoming && sudo ufw allow ssh && sudo ufw allow 80/tcp && sudo ufw allow 443/tcp && sudo ufw enable.",
		})
		score -= 15
	}

	// 5. Web application attack signatures
	totalWebAttacks := a.WebAttacks.SQLiAttempts + a.WebAttacks.PathTraversalAttempts + a.WebAttacks.CommandInjectionAttempts + a.WebAttacks.ScansDetected
	if totalWebAttacks > 0 {
		a.Findings = append(a.Findings, SecurityFinding{
			ID:          "web-attacks-detected",
			Title:       fmt.Sprintf("Web Exploitation Patterns Detected in Logs (%d attempts)", totalWebAttacks),
			Category:    "Web & Reverse Proxy",
			Severity:    SeverityMedium,
			Description: "Automated vulnerability scanners or exploit payloads were recorded targeting your reverse proxy or web containers.",
			Evidence:    fmt.Sprintf("SQLi: %d, Path Traversal: %d, Command Injection: %d, Scanners: %d", a.WebAttacks.SQLiAttempts, a.WebAttacks.PathTraversalAttempts, a.WebAttacks.CommandInjectionAttempts, a.WebAttacks.ScansDetected),
			Remediation: "Ensure containers and applications are patched. Consider adding WAF rate-limiting rules in Nginx or placing Cloudflare in front of public domains.",
		})
		score -= 8
	}

	// 6. Suspicious processes
	if len(a.SystemIntegrity.SuspiciousProcesses) > 0 {
		a.Findings = append(a.Findings, SecurityFinding{
			ID:          "suspicious-procs",
			Title:       "Potential Cryptominer or Unauthorized Shell Process Running",
			Category:    "System Integrity",
			Severity:    SeverityCritical,
			Description: "Processes matching known miner or reverse shell patterns were discovered in the process table.",
			Evidence:    strings.Join(a.SystemIntegrity.SuspiciousProcesses, "\n"),
			Remediation: "Investigate and terminate the identified process PID, inspect cron jobs and systemd unit files for persistence, and rotate server credentials.",
		})
		score -= 30
	}

	if score < 0 {
		score = 0
	}
	a.Score = score

	for _, f := range a.Findings {
		switch f.Severity {
		case SeverityCritical:
			a.CriticalCount++
		case SeverityHigh:
			a.HighCount++
		case SeverityMedium:
			a.MediumCount++
		case SeverityLow:
			a.LowCount++
		case SeverityInfo:
			a.InfoCount++
		}
	}

	switch {
	case score >= 90:
		a.Grade = "A"
		a.Status = "secure"
		a.Summary = "Server is well-hardened with minimal security exposure detected."
	case score >= 75:
		a.Grade = "B"
		a.Status = "warning"
		a.Summary = "Good security posture, but a few hardening recommendations should be addressed."
	case score >= 60:
		a.Grade = "C"
		a.Status = "warning"
		a.Summary = "Moderate security vulnerabilities and attack patterns require prompt operator attention."
	case score >= 40:
		a.Grade = "D"
		a.Status = "critical"
		a.Summary = "Significant security exposure or active attacks detected. Urgent remediation recommended."
	default:
		a.Grade = "F"
		a.Status = "critical"
		a.Summary = "Critical security compromise or severe host misconfiguration detected."
	}
}

func splitSections(raw string) map[string]string {
	sections := make(map[string]string)
	lines := strings.Split(raw, "\n")
	currentSection := ""
	var currentBuilder strings.Builder

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "===SECTION:") && strings.HasSuffix(trimmed, "===") {
			if currentSection != "" {
				sections[currentSection] = currentBuilder.String()
			}
			currentSection = strings.TrimSuffix(strings.TrimPrefix(trimmed, "===SECTION:"), "===")
			currentBuilder.Reset()
			continue
		}
		if currentSection != "" {
			currentBuilder.WriteString(line)
			currentBuilder.WriteString("\n")
		}
	}
	if currentSection != "" {
		sections[currentSection] = currentBuilder.String()
	}
	return sections
}
