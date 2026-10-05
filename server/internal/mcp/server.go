package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"

	"github.com/sirtheprogrammer/docker-deployments/server/internal/ai"
	"github.com/sirtheprogrammer/docker-deployments/server/internal/dockerx"
	"github.com/sirtheprogrammer/docker-deployments/server/internal/servers"
	"github.com/sirtheprogrammer/docker-deployments/server/internal/store"
)

// JSONRPCRequest represents a standard JSON-RPC 2.0 request.
type JSONRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// JSONRPCResponse represents a standard JSON-RPC 2.0 response.
type JSONRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *JSONRPCError   `json:"error,omitempty"`
}

// JSONRPCError defines a JSON-RPC 2.0 error.
type JSONRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// Server provides Model Context Protocol (MCP) tooling for external AI agents
// (e.g., Claude Code, Antigravity, Hermes, GitHub Copilot) to inspect and control dockdeploy.
type Server struct {
	Store   *store.Store
	Sealer  store.Sealer
	Servers *servers.Manager
	mu      sync.Mutex
}

func NewServer(st *store.Store, sealer store.Sealer, mgr *servers.Manager) *Server {
	return &Server{
		Store:   st,
		Sealer:  sealer,
		Servers: mgr,
	}
}

// RunStdio serves the MCP protocol over standard input/output for CLI agents.
func (s *Server) RunStdio(ctx context.Context) error {
	scanner := bufio.NewScanner(os.Stdin)
	// Support payloads up to 10MB
	buf := make([]byte, 64*1024)
	scanner.Buffer(buf, 10*1024*1024)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var req JSONRPCRequest
		if err := json.Unmarshal([]byte(line), &req); err != nil {
			resp := JSONRPCResponse{
				JSONRPC: "2.0",
				Error:   &JSONRPCError{Code: -32700, Message: "Parse error: " + err.Error()},
			}
			s.sendStdio(resp)
			continue
		}

		resp := s.Handle(ctx, req)
		if req.ID != nil {
			s.sendStdio(resp)
		}
	}

	return scanner.Err()
}

func (s *Server) sendStdio(resp JSONRPCResponse) {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := json.Marshal(resp)
	if err == nil {
		fmt.Fprintf(os.Stdout, "%s\n", data)
	}
}

// HTTPHandler returns an HTTP handler for web/network-based MCP clients.
func (s *Server) HTTPHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req JSONRPCRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(JSONRPCResponse{
				JSONRPC: "2.0",
				Error:   &JSONRPCError{Code: -32700, Message: "Parse error: " + err.Error()},
			})
			return
		}

		resp := s.Handle(r.Context(), req)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}
}

// Handle routes and executes a JSON-RPC request.
func (s *Server) Handle(ctx context.Context, req JSONRPCRequest) JSONRPCResponse {
	resp := JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
	}

	switch req.Method {
	case "initialize":
		resp.Result = map[string]any{
			"protocolVersion": "2024-11-05",
			"capabilities": map[string]any{
				"tools": map[string]any{},
			},
			"serverInfo": map[string]any{
				"name":    "dockdeploy",
				"version": "1.0.0",
			},
		}

	case "notifications/initialized":
		// Notification, nothing to return
		resp.Result = map[string]any{"status": "ok"}

	case "tools/list":
		resp.Result = map[string]any{
			"tools": s.toolDefinitions(),
		}

	case "tools/call":
		var params struct {
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &params); err != nil {
			resp.Error = &JSONRPCError{Code: -32602, Message: "Invalid params: " + err.Error()}
			return resp
		}

		text, err := s.callTool(ctx, params.Name, params.Arguments)
		if err != nil {
			resp.Result = map[string]any{
				"content": []map[string]any{
					{"type": "text", "text": fmt.Sprintf("Error: %v", err)},
				},
				"isError": true,
			}
		} else {
			resp.Result = map[string]any{
				"content": []map[string]any{
					{"type": "text", "text": text},
				},
			}
		}

	default:
		resp.Error = &JSONRPCError{Code: -32601, Message: fmt.Sprintf("Method '%s' not found", req.Method)}
	}

	return resp
}

func (s *Server) toolDefinitions() []map[string]any {
	return []map[string]any{
		{
			"name":        "dockdeploy_list_servers",
			"description": "List all servers managed by dockdeploy, including their hostnames, status, and connection mode.",
			"inputSchema": map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
		},
		{
			"name":        "dockdeploy_list_containers",
			"description": "List all running and stopped Docker containers on a managed server.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"server_id": map[string]any{
						"type":        "string",
						"description": "Server ID (optional, defaults to the first available or local server)",
					},
					"all": map[string]any{
						"type":        "boolean",
						"description": "Include stopped containers (default: true)",
					},
				},
			},
		},
		{
			"name":        "dockdeploy_get_container_logs",
			"description": "Retrieve recent log output from a container on a managed server.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"server_id": map[string]any{
						"type":        "string",
						"description": "Server ID",
					},
					"container_id": map[string]any{
						"type":        "string",
						"description": "Container ID or container name",
					},
					"tail": map[string]any{
						"type":        "integer",
						"description": "Number of lines to fetch (default: 100)",
					},
				},
				"required": []string{"server_id", "container_id"},
			},
		},
		{
			"name":        "dockdeploy_restart_container",
			"description": "Restart a specific Docker container on a managed server.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"server_id": map[string]any{
						"type":        "string",
						"description": "Server ID",
					},
					"container_id": map[string]any{
						"type":        "string",
						"description": "Container ID or name to restart",
					},
				},
				"required": []string{"server_id", "container_id"},
			},
		},
		{
			"name":        "dockdeploy_list_deployments",
			"description": "List all configured deployments/applications, source repos, and their latest status in dockdeploy.",
			"inputSchema": map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
		},
		{
			"name":        "dockdeploy_server_diagnostics",
			"description": "Collect complete system metrics and health diagnostics for a server (CPU, RAM, disk, Docker status, container health).",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"server_id": map[string]any{
						"type":        "string",
						"description": "Server ID",
					},
				},
				"required": []string{"server_id"},
			},
		},
	}
}

func (s *Server) callTool(ctx context.Context, name string, args map[string]any) (string, error) {
	if s.Store == nil {
		return "", fmt.Errorf("dockdeploy store is not initialized")
	}

	switch name {
	case "dockdeploy_list_servers":
		serversList, err := s.Store.ListServers(ctx, "", true)
		if err != nil {
			return "", err
		}
		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("Found %d managed server(s):\n\n", len(serversList)))
		for _, srv := range serversList {
			sb.WriteString(fmt.Sprintf("- **%s** (`%s`)\n  Host: `%s`, Status: `%s`, Auth: `%s`, Docker: `%s`\n",
				srv.Name, srv.ID, srv.Host, srv.Status, srv.AuthMethod, srv.DockerSocket))
		}
		return sb.String(), nil

	case "dockdeploy_list_containers":
		serverID := getString(args, "server_id")
		server, err := s.resolveServer(ctx, serverID)
		if err != nil {
			return "", err
		}

		session, err := s.Servers.Session(ctx, server)
		if err != nil {
			return "", fmt.Errorf("connect to server: %w", err)
		}
		defer session.Close()

		containers, err := session.Docker.ListContainers(ctx)
		if err != nil {
			return "", fmt.Errorf("list containers: %w", err)
		}

		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("Containers on server **%s** (%s):\n\n", server.Name, server.ID))
		for _, c := range containers {
			var ports []string
			for _, p := range c.Ports {
				if p.HostPort > 0 {
					ports = append(ports, fmt.Sprintf("%d:%d", p.HostPort, p.Container))
				}
			}
			portStr := strings.Join(ports, ", ")
			if portStr == "" {
				portStr = "none"
			}

			sb.WriteString(fmt.Sprintf("- **%s** (ID: `%s`)\n  State: `%s` (%s), Image: `%s`, Ports: %s\n",
				c.Name, c.ID[:min(12, len(c.ID))], c.State, c.Status, c.Image, portStr))
		}
		return sb.String(), nil

	case "dockdeploy_get_container_logs":
		serverID := getString(args, "server_id")
		containerID := getString(args, "container_id")
		tail := getInt(args, "tail", 100)

		server, err := s.resolveServer(ctx, serverID)
		if err != nil {
			return "", err
		}

		session, err := s.Servers.Session(ctx, server)
		if err != nil {
			return "", fmt.Errorf("connect to server: %w", err)
		}
		defer session.Close()

		var stdout bytes.Buffer
		opts := dockerx.LogOptions{
			Tail: fmt.Sprintf("%d", tail),
		}
		if err := session.Docker.Logs(ctx, containerID, opts, &stdout, &stdout); err != nil {
			return "", fmt.Errorf("fetch container logs: %w", err)
		}

		return fmt.Sprintf("Logs for container `%s` on `%s` (tail %d):\n\n```\n%s\n```",
			containerID, server.Name, tail, stdout.String()), nil

	case "dockdeploy_restart_container":
		serverID := getString(args, "server_id")
		containerID := getString(args, "container_id")

		server, err := s.resolveServer(ctx, serverID)
		if err != nil {
			return "", err
		}

		session, err := s.Servers.Session(ctx, server)
		if err != nil {
			return "", fmt.Errorf("connect to server: %w", err)
		}
		defer session.Close()

		if err := session.Docker.Do(ctx, dockerx.ActionRestart, containerID); err != nil {
			return "", fmt.Errorf("restart container: %w", err)
		}

		return fmt.Sprintf("Container `%s` on server `%s` has been restarted successfully.", containerID, server.Name), nil

	case "dockdeploy_list_deployments":
		deps, err := s.Store.ListDeployments(ctx, "", true)
		if err != nil {
			return "", err
		}

		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("Found %d deployment(s):\n\n", len(deps)))
		for _, d := range deps {
			sb.WriteString(fmt.Sprintf("- **%s** (`%s`)\n  Source: `%s`, ServerID: `%s`\n",
				d.Name, d.ID, d.SourceType, d.ServerID))
		}
		return sb.String(), nil

	case "dockdeploy_server_diagnostics":
		serverID := getString(args, "server_id")
		server, err := s.resolveServer(ctx, serverID)
		if err != nil {
			return "", err
		}

		diag, err := ai.CollectServerDiagnostic(ctx, s.Servers, server, "")
		if err != nil {
			return "", fmt.Errorf("collect diagnostics: %w", err)
		}

		return diag.SummaryMarkdown, nil

	default:
		return "", fmt.Errorf("unknown tool: %s", name)
	}
}

func (s *Server) resolveServer(ctx context.Context, serverID string) (*store.Server, error) {
	if serverID != "" {
		return s.Store.ServerByID(ctx, serverID)
	}
	serversList, err := s.Store.ListServers(ctx, "", true)
	if err != nil {
		return nil, err
	}
	if len(serversList) == 0 {
		return nil, fmt.Errorf("no servers configured in dockdeploy")
	}
	// Prefer local server
	for i := range serversList {
		if serversList[i].AuthMethod == "local" {
			return &serversList[i], nil
		}
	}
	return &serversList[0], nil
}

func getString(args map[string]any, key string) string {
	if v, ok := args[key]; ok {
		if s, ok := v.(string); ok {
			return strings.TrimSpace(s)
		}
	}
	return ""
}

func getInt(args map[string]any, key string, fallback int) int {
	if v, ok := args[key]; ok {
		switch num := v.(type) {
		case float64:
			return int(num)
		case int:
			return num
		}
	}
	return fallback
}
