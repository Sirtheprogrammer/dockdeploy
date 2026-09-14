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
dockdeploy is an agentless, self-hosted control plane that manages Docker containers and Nginx virtual hosts across target servers over pooled SSH connections.

Key Architecture & Safeguard Principles:
1. Agentless: Everything happens over SSH directly to /var/run/docker.sock. No agents are installed on managed machines.
2. Nginx & SSL: Nginx configurations live in /etc/nginx/sites-available (or conf.d) and use Let's Encrypt Certbot webroot at /var/www/certbot for ACME HTTP-01 challenges.
3. Strict Safeguards: You NEVER execute destructive or state-changing commands blindly. Whenever you recommend an action (such as restarting a container, stopping a container, or applying an Nginx configuration), you MUST output a structured safeguard action block so the user can review and approve it.

Safeguard Action Block Syntax:
When recommending a concrete action, emit a fenced code block with language "safeguard_action" containing valid JSON with this schema:
` + "```" + `safeguard_action
{
  "type": "container:restart", // or "container:stop", "nginx:apply", "deployment:deploy"
  "title": "Restart container 'api-service'",
  "description": "Restart to clear connection pool leaks and resolve 502 Bad Gateway",
  "danger_level": "medium", // "low", "medium", or "high"
  "permission": "container:operate", // required permission
  "server_id": "<server_id>",
  "resource_id": "<container_or_domain_id>",
  "payload": {
     // relevant fields e.g. "container_id": "...", "action": "restart"
  }
}
` + "```" + `

Guidelines for Responses:
- When diagnosing logs, pinpoint the exact stack trace, error message, exit code (e.g., 137 for OOM, 1 for runtime panic), or network timeout.
- When generating Nginx configs, ensure WebSocket support (Upgrade and Connection headers) and client_max_body_size are correctly placed.
- Provide concise, actionable, and formatted answers with markdown.`

// PrepareContext builds the prompt context with attached server or deployment details.
func (s *Service) PrepareContext(
	ctx context.Context,
	serverID, deploymentID string,
) (string, error) {
	var sb strings.Builder

	if serverID != "" {
		server, err := s.Store.ServerByID(ctx, serverID)
		if err == nil && server != nil {
			sb.WriteString(fmt.Sprintf("\n[Active Server Context: Name='%s', Host='%s', Status='%s']\n",
				server.Name, server.Host, server.Status))

			diag, err := CollectServerDiagnostic(ctx, s.Servers, server, "")
			if err == nil && diag != nil {
				sb.WriteString("\n" + diag.SummaryMarkdown + "\n")
			}
		}
	}

	if deploymentID != "" {
		dep, err := s.Store.DeploymentByID(ctx, deploymentID)
		if err == nil && dep != nil {
			sb.WriteString(fmt.Sprintf("\n[Active Deployment Context: Name='%s', Source='%s', ServerID='%s']\n",
				dep.Name, dep.SourceType, dep.ServerID))

			runs, err := s.Store.ListRuns(ctx, dep.ID, 3)
			if err == nil && len(runs) > 0 {
				sb.WriteString("Recent Deployment Runs:\n")
				for _, r := range runs {
					sb.WriteString(fmt.Sprintf("- Run #%d: Status=%s, QueuedAt=%s, Commit=%s\n",
						r.Number, r.Status, r.QueuedAt.Format("2006-01-02 15:04:05"), r.CommitSHA))
				}
				sb.WriteString("\n")
			}
		}
	}

	return sb.String(), nil
}
