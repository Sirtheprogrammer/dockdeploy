package ai

import (
	"context"
	"fmt"
	"strings"

	"github.com/sirtheprogrammer/docker-deployments/server/internal/servers"
	"github.com/sirtheprogrammer/docker-deployments/server/internal/store"
)

type Service struct {
	Store   *store.Store
	Sealer  store.Sealer
	Servers *servers.Manager
	Client  *Client
}

func NewService(db *store.Store, sealer store.Sealer, servers *servers.Manager) *Service {
	return &Service{
		Store:   db,
		Sealer:  sealer,
		Servers: servers,
		Client:  NewClient(),
	}
}

const BaseSystemPrompt = `You are the Dockdeploy AI Assistant, an expert DevOps and Site Reliability Engineering assistant embedded inside dockdeploy.
dockdeploy is an agentless, self-hosted control plane that manages Docker containers, Nginx reverse proxy virtual hosts, deployments, and SSL certificates across target servers over pooled SSH connections.

Key Architecture & Operating Principles:
1. Agentless: Everything operates over SSH directly to /var/run/docker.sock and host filesystems. No background agents are required on target machines.
2. Nginx & SSL: Nginx configurations live in /etc/nginx/sites-available (or conf.d) and use Let's Encrypt Certbot webroot at /var/www/certbot for ACME HTTP-01 challenges.
3. Strict Safeguards: You NEVER execute state-changing or destructive commands blindly. Whenever you recommend a concrete action, you MUST emit a structured safeguard action block so the operator can review and approve it.

Safeguard Action Block Syntax:
When recommending an actionable fix or operation, emit a fenced code block with language "safeguard_action" containing valid JSON.
The supported action types and schemas are:

1. Restart container:
` + "```" + `safeguard_action
{
  "type": "container:restart",
  "title": "Restart container 'web-service'",
  "description": "Restart to clear deadlocked threads or recover from crash loop",
  "danger_level": "medium",
  "permission": "container:operate",
  "server_id": "<server_id>",
  "resource_id": "<container_id_or_name>",
  "payload": { "server_id": "<server_id>", "container_id": "<container_id_or_name>", "action": "restart" }
}
` + "```" + `

2. Start or Stop container:
` + "```" + `safeguard_action
{
  "type": "container:start", // or "container:stop", "container:pause", "container:unpause"
  "title": "Start container 'redis'",
  "description": "Start stopped container",
  "danger_level": "low",
  "permission": "container:operate",
  "server_id": "<server_id>",
  "resource_id": "<container_id>",
  "payload": { "server_id": "<server_id>", "container_id": "<container_id>", "action": "start" }
}
` + "```" + `

3. Execute command on server as root (diagnostics, cleanup, service restarts):
` + "```" + `safeguard_action
{
  "type": "server:exec_root",
  "title": "Run: docker system prune -f",
  "description": "Prune unused docker containers, networks, and dangling images to reclaim disk space",
  "danger_level": "medium",
  "permission": "server:write",
  "server_id": "<server_id>",
  "payload": { "server_id": "<server_id>", "command": "docker system prune -f" }
}
` + "```" + `

4. Trigger deployment run (redeploy / build):
` + "```" + `safeguard_action
{
  "type": "deployment:deploy",
  "title": "Trigger deployment run",
  "description": "Pull latest commit and run rolling deployment",
  "danger_level": "low",
  "permission": "deployment:deploy",
  "resource_id": "<deployment_id>",
  "payload": { "deployment_id": "<deployment_id>" }
}
` + "```" + `

5. Sync Nginx virtual host configuration:
` + "```" + `safeguard_action
{
  "type": "domain:sync",
  "title": "Sync Nginx configuration for domain",
  "description": "Regenerate upstream proxy rules and reload Nginx",
  "danger_level": "low",
  "permission": "domain:write",
  "resource_id": "<domain_id>",
  "payload": { "domain_id": "<domain_id>" }
}
` + "```" + `

6. Issue Let's Encrypt SSL certificate:
` + "```" + `safeguard_action
{
  "type": "domain:ssl",
  "title": "Issue SSL certificate for domain",
  "description": "Run Certbot ACME HTTP-01 challenge and reload Nginx with HTTPS",
  "danger_level": "low",
  "permission": "domain:write",
  "resource_id": "<domain_id>",
  "payload": { "domain_id": "<domain_id>" }
}
` + "```" + `

7. Probe server health & connection:
` + "```" + `safeguard_action
{
  "type": "server:probe",
  "title": "Probe server health check",
  "description": "Re-check SSH connectivity, Docker daemon status, and system metrics",
  "danger_level": "low",
  "permission": "server:read",
  "server_id": "<server_id>",
  "payload": { "server_id": "<server_id>" }
}
` + "```" + `

8. Install Docker on host:
` + "```" + `safeguard_action
{
  "type": "server:install_docker",
  "title": "Install Docker Engine",
  "description": "Execute automated Docker CE setup script over SSH",
  "danger_level": "high",
  "permission": "server:write",
  "server_id": "<server_id>",
  "payload": { "server_id": "<server_id>" }
}
` + "```" + `

9. Create database backup:
` + "```" + `safeguard_action
{
  "type": "server:backup",
  "title": "Backup containerized databases",
  "description": "Take an immediate SQL dump of detected database containers on this host",
  "danger_level": "low",
  "permission": "server:write",
  "server_id": "<server_id>",
  "payload": { "server_id": "<server_id>" }
}
` + "```" + `

Guidelines for Responses:
- Pinpoint root causes from logs (e.g. OOM exit code 137, missing env variables, port collisions, unmigrated schemas, bad Nginx syntax).
- Whenever a problem can be fixed or verified with an action (e.g. restarting a container, running a prune command, probing a server, redeploying, syncing SSL), ALWAYS provide the corresponding safeguard_action block.
- Be concise, direct, helpful, and format technical details in clear markdown.
`

// PrepareContext builds the prompt context with attached server, deployment, and fleet details.
func (s *Service) PrepareContext(
	ctx context.Context,
	serverID, deploymentID, containerID string,
) (string, error) {
	var sb strings.Builder

	if serverID != "" {
		server, err := s.Store.ServerByID(ctx, serverID)
		if err == nil && server != nil {
			sb.WriteString(fmt.Sprintf("\n[Active Server Context: Name='%s', Host='%s', Status='%s', Port=%d, User='%s']\n",
				server.Name, server.Host, server.Status, server.Port, server.Username))

			diag, err := CollectServerDiagnostic(ctx, s.Servers, server, containerID)
			if err == nil && diag != nil {
				sb.WriteString("\n" + diag.SummaryMarkdown + "\n")
			}

			// Add associated domains
			domains, err := s.Store.ListDomainsByServer(ctx, server.ID)
			if err == nil && len(domains) > 0 {
				sb.WriteString("Associated Domains on this Server:\n")
				for _, d := range domains {
					sb.WriteString(fmt.Sprintf("- Hostname: `%s` -> Upstream Port: %d (SSL Mode: %s, Status: %s)\n",
						d.Hostname, d.UpstreamPort, d.SSLMode, d.Status))
				}
				sb.WriteString("\n")
			}
		}
	} else if deploymentID == "" {
		// Global fleet overview when no specific server/deployment is selected
		servers, err := s.Store.ListServers(ctx, "", true)
		if err == nil && len(servers) > 0 {
			sb.WriteString("\n[Fleet Infrastructure — Registered Servers]\n")
			sb.WriteString("| ID | Name | Host | Status | Port |\n")
			sb.WriteString("| --- | --- | --- | --- | --- |\n")
			for _, srv := range servers {
				sb.WriteString(fmt.Sprintf("| `%s` | %s | `%s` | %s | %d |\n",
					srv.ID, srv.Name, srv.Host, srv.Status, srv.Port))
			}
			sb.WriteString("\n")
		}

		deps, err := s.Store.ListDeployments(ctx, "", true)
		if err == nil && len(deps) > 0 {
			sb.WriteString("[Active Deployments Overview]\n")
			sb.WriteString("| ID | Name | Server ID | Source |\n")
			sb.WriteString("| --- | --- | --- | --- |\n")
			for _, d := range deps {
				sb.WriteString(fmt.Sprintf("| `%s` | %s | `%s` | %s |\n",
					d.ID, d.Name, d.ServerID, d.SourceType))
			}
			sb.WriteString("\n")
		}
	}

	if deploymentID != "" {
		dep, err := s.Store.DeploymentByID(ctx, deploymentID)
		if err == nil && dep != nil {
			sb.WriteString(fmt.Sprintf("\n[Active Deployment Context: Name='%s', Source='%s', ServerID='%s']\n",
				dep.Name, dep.SourceType, dep.ServerID))

			runs, err := s.Store.ListRuns(ctx, dep.ID, 5)
			if err == nil && len(runs) > 0 {
				sb.WriteString("Recent Deployment Runs:\n")
				for _, r := range runs {
					errText := ""
					if r.Error != "" {
						errText = fmt.Sprintf(" | Error: %s", r.Error)
					}
					sb.WriteString(fmt.Sprintf("- Run #%d: Status=%s, QueuedAt=%s, Commit=%s%s\n",
						r.Number, r.Status, r.QueuedAt.Format("2006-01-02 15:04:05"), r.CommitSHA, errText))
				}
				sb.WriteString("\n")
			}
		}
	}

	return sb.String(), nil
}
