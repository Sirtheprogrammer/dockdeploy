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
	"time"

	"github.com/sirtheprogrammer/docker-deployments/server/internal/ai"
	"github.com/sirtheprogrammer/docker-deployments/server/internal/dockerx"
	"github.com/sirtheprogrammer/docker-deployments/server/internal/servers"
	"github.com/sirtheprogrammer/docker-deployments/server/internal/sshx"
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
	JSONRPC string        `json:"jsonrpc"`
	ID      any           `json:"id,omitempty"`
	Result  any           `json:"result,omitempty"`
	Error   *JSONRPCError `json:"error,omitempty"`
}

// JSONRPCError defines a JSON-RPC 2.0 error.
type JSONRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// Server provides Model Context Protocol (MCP) tooling for external AI agents
// (e.g., Claude Code, Antigravity, Hermes, GitHub Copilot, Cursor) to inspect
// and control dockdeploy, run shell commands, interact with containers, and manage deployments.
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

		if strings.HasPrefix(line, "[") {
			var batch []JSONRPCRequest
			if err := json.Unmarshal([]byte(line), &batch); err != nil {
				resp := JSONRPCResponse{
					JSONRPC: "2.0",
					Error:   &JSONRPCError{Code: -32700, Message: "Parse error: " + err.Error()},
				}
				s.sendStdio(resp)
				continue
			}
			var responses []JSONRPCResponse
			for _, req := range batch {
				resp := s.Handle(ctx, req)
				if req.ID != nil {
					responses = append(responses, resp)
				}
			}
			if len(responses) > 0 {
				s.sendStdio(responses)
			}
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

func (s *Server) sendStdio(payload any) {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := json.Marshal(payload)
	if err == nil {
		fmt.Fprintf(os.Stdout, "%s\n", data)
	}
}

// HTTPHandler returns an HTTP handler for web/network-based MCP clients.
func (s *Server) HTTPHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Handle SSE (Server-Sent Events) discovery
		if r.Method == http.MethodGet {
			if strings.Contains(r.Header.Get("Accept"), "text/event-stream") {
				w.Header().Set("Content-Type", "text/event-stream")
				w.Header().Set("Cache-Control", "no-cache")
				w.Header().Set("Connection", "keep-alive")
				// Emit endpoint event pointing to the POST handler with preserved query tokens
				postURL := r.URL.Path
				if r.URL.RawQuery != "" {
					postURL += "?" + r.URL.RawQuery
				}
				fmt.Fprintf(w, "event: endpoint\ndata: %s\n\n", postURL)
				if flusher, ok := w.(http.Flusher); ok {
					flusher.Flush()
				}
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"service":         "dockdeploy-mcp",
				"status":          "ok",
				"protocolVersion": "2024-11-05",
			})
			return
		}

		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var raw json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(JSONRPCResponse{
				JSONRPC: "2.0",
				Error:   &JSONRPCError{Code: -32700, Message: "Parse error: " + err.Error()},
			})
			return
		}

		trimmed := bytes.TrimSpace(raw)
		if bytes.HasPrefix(trimmed, []byte("[")) {
			var batch []JSONRPCRequest
			if err := json.Unmarshal(trimmed, &batch); err != nil {
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(JSONRPCResponse{
					JSONRPC: "2.0",
					Error:   &JSONRPCError{Code: -32700, Message: "Parse error: " + err.Error()},
				})
				return
			}
			var responses []JSONRPCResponse
			for _, req := range batch {
				resp := s.Handle(r.Context(), req)
				if req.ID != nil {
					responses = append(responses, resp)
				}
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(responses)
			return
		}

		var req JSONRPCRequest
		if err := json.Unmarshal(trimmed, &req); err != nil {
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

// Handle routes and executes a JSON-RPC request according to MCP 2024-11-05 specification.
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
				"tools":     map[string]any{"listChanged": false},
				"resources": map[string]any{"subscribe": false, "listChanged": false},
				"prompts":   map[string]any{"listChanged": false},
			},
			"serverInfo": map[string]any{
				"name":    "dockdeploy",
				"version": "1.0.0",
			},
			"instructions": "dockdeploy MCP server provides full infrastructure observability and management for Docker hosts, container lifecycles, file operations, deployment pipelines, and shell command execution.",
		}

	case "notifications/initialized", "notifications/cancelled", "notifications/progress":
		// Standard notifications, acknowledge
		resp.Result = map[string]any{"status": "ok"}

	case "ping":
		resp.Result = map[string]any{}

	case "resources/list":
		resp.Result = map[string]any{
			"resources": []map[string]any{
				{
					"uri":         "dockdeploy://servers",
					"name":        "Managed Servers",
					"description": "List of all registered servers and their current status",
					"mimeType":    "application/json",
				},
				{
					"uri":         "dockdeploy://deployments",
					"name":        "Configured Deployments",
					"description": "List of all applications/deployments and their repository sources",
					"mimeType":    "application/json",
				},
			},
		}

	case "resources/read":
		var params struct {
			URI string `json:"uri"`
		}
		if err := json.Unmarshal(req.Params, &params); err != nil {
			resp.Error = &JSONRPCError{Code: -32602, Message: "Invalid params: " + err.Error()}
			return resp
		}

		switch params.URI {
		case "dockdeploy://servers":
			if s.Store == nil {
				resp.Error = &JSONRPCError{Code: -32000, Message: "Store not initialized"}
				return resp
			}
			serversList, err := s.Store.ListServers(ctx, "", true)
			if err != nil {
				resp.Error = &JSONRPCError{Code: -32000, Message: err.Error()}
				return resp
			}
			data, _ := json.MarshalIndent(serversList, "", "  ")
			resp.Result = map[string]any{
				"contents": []map[string]any{
					{
						"uri":      params.URI,
						"mimeType": "application/json",
						"text":     string(data),
					},
				},
			}

		case "dockdeploy://deployments":
			if s.Store == nil {
				resp.Error = &JSONRPCError{Code: -32000, Message: "Store not initialized"}
				return resp
			}
			deps, err := s.Store.ListDeployments(ctx, "", true)
			if err != nil {
				resp.Error = &JSONRPCError{Code: -32000, Message: err.Error()}
				return resp
			}
			data, _ := json.MarshalIndent(deps, "", "  ")
			resp.Result = map[string]any{
				"contents": []map[string]any{
					{
						"uri":      params.URI,
						"mimeType": "application/json",
						"text":     string(data),
					},
				},
			}

		default:
			resp.Error = &JSONRPCError{Code: -32602, Message: fmt.Sprintf("Resource uri %q not found", params.URI)}
		}

	case "prompts/list":
		resp.Result = map[string]any{
			"prompts": []map[string]any{
				{
					"name":        "diagnose_server",
					"description": "Diagnose health, resources, Docker daemon, and system load on a server",
					"arguments": []map[string]any{
						{
							"name":        "server_id",
							"description": "Server ID (optional, defaults to local or first server)",
							"required":    false,
						},
					},
				},
				{
					"name":        "troubleshoot_container",
					"description": "Inspect container state, logs, and provide remediation steps",
					"arguments": []map[string]any{
						{
							"name":        "container_id",
							"description": "Container ID or name to troubleshoot",
							"required":    true,
						},
						{
							"name":        "server_id",
							"description": "Server ID (optional)",
							"required":    false,
						},
					},
				},
				{
					"name":        "review_deployment",
					"description": "Review deployment configuration, runs, and failure logs",
					"arguments": []map[string]any{
						{
							"name":        "deployment_id",
							"description": "Deployment ID",
							"required":    true,
						},
					},
				},
			},
		}

	case "prompts/get":
		var params struct {
			Name      string            `json:"name"`
			Arguments map[string]string `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &params); err != nil {
			resp.Error = &JSONRPCError{Code: -32602, Message: "Invalid params: " + err.Error()}
			return resp
		}

		switch params.Name {
		case "diagnose_server":
			serverID := params.Arguments["server_id"]
			resp.Result = map[string]any{
				"description": "Diagnose server health and Docker daemon",
				"messages": []map[string]any{
					{
						"role": "user",
						"content": map[string]any{
							"type": "text",
							"text": fmt.Sprintf("Please run a full diagnostic on server %q using dockdeploy_server_diagnostics and dockdeploy_exec_command (e.g. df -h, free -m, docker stats --no-stream), and explain any anomalies or resource bottlenecks found.", serverID),
						},
					},
				},
			}

		case "troubleshoot_container":
			containerID := params.Arguments["container_id"]
			serverID := params.Arguments["server_id"]
			resp.Result = map[string]any{
				"description": "Troubleshoot container state and logs",
				"messages": []map[string]any{
					{
						"role": "user",
						"content": map[string]any{
							"type": "text",
							"text": fmt.Sprintf("Please inspect container %q on server %q using dockdeploy_inspect_container and dockdeploy_get_container_logs. If necessary, execute debugging commands with dockdeploy_docker_exec or dockdeploy_exec_command and recommend remediation steps.", containerID, serverID),
						},
					},
				},
			}

		case "review_deployment":
			depID := params.Arguments["deployment_id"]
			resp.Result = map[string]any{
				"description": "Review deployment configuration and logs",
				"messages": []map[string]any{
					{
						"role": "user",
						"content": map[string]any{
							"type": "text",
							"text": fmt.Sprintf("Please inspect deployment %q using dockdeploy_list_deployments and dockdeploy_get_deployment_logs. Analyze recent build or run errors and outline what went wrong and how to fix it.", depID),
						},
					},
				},
			}

		default:
			resp.Error = &JSONRPCError{Code: -32602, Message: fmt.Sprintf("Prompt %q not found", params.Name)}
		}

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
			"description": "List all servers managed by dockdeploy, including their hostnames, IP, status, and connection mode.",
			"inputSchema": map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
		},
		{
			"name":        "dockdeploy_get_server",
			"description": "Get detailed information about a specific server, including OS capabilities, Docker/Compose versions, Sudo mode, and system status.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"server_id": map[string]any{
						"type":        "string",
						"description": "Server ID (optional, defaults to local or first server)",
					},
				},
			},
		},
		{
			"name":        "dockdeploy_exec_command",
			"description": "Execute a shell command directly on a managed server (or host). Supports working directory, execution timeouts, and optional sudo/root elevation.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"server_id": map[string]any{
						"type":        "string",
						"description": "Server ID (optional, defaults to local or first server)",
					},
					"command": map[string]any{
						"type":        "string",
						"description": "Shell command to execute on the server (e.g. 'df -h', 'ps aux', 'uptime', 'journalctl -u docker -n 50')",
					},
					"sudo": map[string]any{
						"type":        "boolean",
						"description": "Whether to execute the command with elevated sudo/root privileges (default: false)",
					},
					"sudo_password": map[string]any{
						"type":        "string",
						"description": "One-time sudo password if required",
					},
					"workdir": map[string]any{
						"type":        "string",
						"description": "Directory path on the server in which to execute the command",
					},
					"timeout_seconds": map[string]any{
						"type":        "integer",
						"description": "Command execution timeout in seconds (default: 60, max: 600)",
					},
				},
				"required": []string{"command"},
			},
		},
		{
			"name":        "dockdeploy_docker_exec",
			"description": "Execute a shell command inside a running Docker container on a managed server.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"server_id": map[string]any{
						"type":        "string",
						"description": "Server ID (optional, defaults to local or first server)",
					},
					"container_id": map[string]any{
						"type":        "string",
						"description": "Container ID or name",
					},
					"command": map[string]any{
						"type":        "string",
						"description": "Command line to execute inside the container",
					},
					"user": map[string]any{
						"type":        "string",
						"description": "User or UID to execute as inside container (e.g. 'root')",
					},
					"workdir": map[string]any{
						"type":        "string",
						"description": "Working directory inside the container",
					},
					"sudo": map[string]any{
						"type":        "boolean",
						"description": "Run docker exec with host sudo elevation if docker socket requires root (default: false)",
					},
					"timeout_seconds": map[string]any{
						"type":        "integer",
						"description": "Execution timeout in seconds (default: 60)",
					},
				},
				"required": []string{"container_id", "command"},
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
						"description": "Server ID (optional, defaults to local or first server)",
					},
					"all": map[string]any{
						"type":        "boolean",
						"description": "Include stopped containers (default: true)",
					},
				},
			},
		},
		{
			"name":        "dockdeploy_inspect_container",
			"description": "Inspect full details of a Docker container including state, ports, mounts, environment variables, and health status.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"server_id": map[string]any{
						"type":        "string",
						"description": "Server ID (optional)",
					},
					"container_id": map[string]any{
						"type":        "string",
						"description": "Container ID or name",
					},
				},
				"required": []string{"container_id"},
			},
		},
		{
			"name":        "dockdeploy_container_action",
			"description": "Perform lifecycle operations on a Docker container: start, stop, restart, pause, or unpause.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"server_id": map[string]any{
						"type":        "string",
						"description": "Server ID (optional)",
					},
					"container_id": map[string]any{
						"type":        "string",
						"description": "Container ID or name",
					},
					"action": map[string]any{
						"type":        "string",
						"description": "Action to perform: 'start', 'stop', 'restart', 'pause', 'unpause'",
						"enum":        []string{"start", "stop", "restart", "pause", "unpause"},
					},
				},
				"required": []string{"container_id", "action"},
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
				"required": []string{"container_id"},
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
				"required": []string{"container_id"},
			},
		},
		{
			"name":        "dockdeploy_read_file",
			"description": "Read the content of a file on a managed server's filesystem.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"server_id": map[string]any{
						"type":        "string",
						"description": "Server ID (optional)",
					},
					"path": map[string]any{
						"type":        "string",
						"description": "Absolute file path to read on the server",
					},
					"max_bytes": map[string]any{
						"type":        "integer",
						"description": "Maximum bytes to read (default: 65536)",
					},
				},
				"required": []string{"path"},
			},
		},
		{
			"name":        "dockdeploy_write_file",
			"description": "Write or update content of a file on a managed server's filesystem.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"server_id": map[string]any{
						"type":        "string",
						"description": "Server ID (optional)",
					},
					"path": map[string]any{
						"type":        "string",
						"description": "Absolute file path to write on the server",
					},
					"content": map[string]any{
						"type":        "string",
						"description": "Content string to write into the file",
					},
					"sudo_password": map[string]any{
						"type":        "string",
						"description": "Sudo password if writing to root-protected paths",
					},
				},
				"required": []string{"path", "content"},
			},
		},
		{
			"name":        "dockdeploy_list_files",
			"description": "List directory contents with file metadata (size, permissions, mod time) on a managed server.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"server_id": map[string]any{
						"type":        "string",
						"description": "Server ID (optional)",
					},
					"path": map[string]any{
						"type":        "string",
						"description": "Directory path on the server to list",
					},
				},
				"required": []string{"path"},
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
			"name":        "dockdeploy_trigger_deployment",
			"description": "Trigger a new deployment/build run for a specified deployment in dockdeploy.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"deployment_id": map[string]any{
						"type":        "string",
						"description": "Deployment ID to trigger",
					},
				},
				"required": []string{"deployment_id"},
			},
		},
		{
			"name":        "dockdeploy_get_deployment_logs",
			"description": "Fetch logs for a deployment run to monitor build output and diagnose deployment failures.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"deployment_id": map[string]any{
						"type":        "string",
						"description": "Deployment ID",
					},
					"run_id": map[string]any{
						"type":        "string",
						"description": "Run ID (optional, defaults to the latest run)",
					},
				},
				"required": []string{"deployment_id"},
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
						"description": "Server ID (optional)",
					},
				},
			},
		},
		{
			"name":        "dockdeploy_security_audit",
			"description": "Inspect a server for active attack patterns, SSH brute-force attempts, exposed dangerous network ports (Redis, MySQL, unauthenticated Docker socket), Docker container privilege risks, and web exploit signatures.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"server_id": map[string]any{
						"type":        "string",
						"description": "Server ID (optional, defaults to local or first server)",
					},
				},
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

	case "dockdeploy_get_server":
		serverID := getString(args, "server_id")
		server, err := s.resolveServer(ctx, serverID)
		if err != nil {
			return "", err
		}

		var caps *sshx.Capabilities
		if len(server.Capabilities) > 0 {
			var c sshx.Capabilities
			if err := json.Unmarshal(server.Capabilities, &c); err == nil {
				caps = &c
			}
		}

		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("### Server **%s** (`%s`)\n\n", server.Name, server.ID))
		sb.WriteString(fmt.Sprintf("- **Host**: `%s:%d`\n", server.Host, server.Port))
		sb.WriteString(fmt.Sprintf("- **User**: `%s`\n", server.Username))
		sb.WriteString(fmt.Sprintf("- **Auth Method**: `%s`\n", server.AuthMethod))
		sb.WriteString(fmt.Sprintf("- **Status**: `%s`\n", server.Status))
		sb.WriteString(fmt.Sprintf("- **Docker Socket**: `%s`\n", server.DockerSocket))
		if server.LastSeenAt != nil {
			sb.WriteString(fmt.Sprintf("- **Last Seen**: `%s`\n", server.LastSeenAt.Format(time.RFC3339)))
		}
		if server.StatusMessage != "" {
			sb.WriteString(fmt.Sprintf("- **Status Note**: %s\n", server.StatusMessage))
		}

		if caps != nil {
			sb.WriteString("\n#### Capabilities & System Info\n")
			sb.WriteString(fmt.Sprintf("- **OS**: `%s`\n", caps.OS))
			sb.WriteString(fmt.Sprintf("- **Kernel**: `%s`\n", caps.Kernel))
			sb.WriteString(fmt.Sprintf("- **Docker Version**: `%s` (Socket OK: `%t`)\n", caps.DockerVersion, caps.DockerSocketOK))
			sb.WriteString(fmt.Sprintf("- **Docker Compose**: `%s` (%s)\n", caps.ComposeVersion, caps.ComposeCommand))
			sb.WriteString(fmt.Sprintf("- **Sudo Mode**: `%s`\n", caps.SudoMode))
			if caps.NginxVersion != "" {
				sb.WriteString(fmt.Sprintf("- **Nginx**: `%s`\n", caps.NginxVersion))
			}
			if caps.CertbotVersion != "" {
				sb.WriteString(fmt.Sprintf("- **Certbot**: `%s`\n", caps.CertbotVersion))
			}
			if caps.GitVersion != "" {
				sb.WriteString(fmt.Sprintf("- **Git**: `%s`\n", caps.GitVersion))
			}
			if len(caps.Warnings) > 0 {
				sb.WriteString("- **Warnings**:\n")
				for _, w := range caps.Warnings {
					sb.WriteString(fmt.Sprintf("  - %s\n", w))
				}
			}
		}

		return sb.String(), nil

	case "dockdeploy_exec_command":
		serverID := getString(args, "server_id")
		command := getString(args, "command")
		if command == "" {
			return "", fmt.Errorf("command is required")
		}
		sudo := getBool(args, "sudo", false)
		sudoPassword := getString(args, "sudo_password")
		workdir := getString(args, "workdir")
		timeoutSec := getInt(args, "timeout_seconds", 60)
		if timeoutSec <= 0 || timeoutSec > 600 {
			timeoutSec = 60
		}

		server, err := s.resolveServer(ctx, serverID)
		if err != nil {
			return "", err
		}

		cmdCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSec)*time.Second)
		defer cancel()

		start := time.Now()
		var res sshx.Result
		if workdir != "" {
			res, err = s.Servers.RunCommandInDir(cmdCtx, server, workdir, command, sudo, sudoPassword)
		} else {
			res, err = s.Servers.RunCommand(cmdCtx, server, command, sudo, sudoPassword)
		}
		duration := time.Since(start).Round(time.Millisecond)

		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("Command executed on **%s** (`%s`)\n", server.Name, server.ID))
		if workdir != "" {
			sb.WriteString(fmt.Sprintf("- Working Dir: `%s`\n", workdir))
		}
		sb.WriteString(fmt.Sprintf("- Elevated (sudo): `%t`\n", sudo))
		sb.WriteString(fmt.Sprintf("- Duration: `%s`\n", duration))
		sb.WriteString(fmt.Sprintf("- Exit Code: `%d`\n\n", res.ExitCode))

		if strings.TrimSpace(res.Stdout) != "" {
			sb.WriteString("```\n")
			sb.WriteString(res.Stdout)
			if !strings.HasSuffix(res.Stdout, "\n") {
				sb.WriteString("\n")
			}
			sb.WriteString("```\n")
		} else {
			sb.WriteString("*(stdout empty)*\n")
		}

		if strings.TrimSpace(res.Stderr) != "" {
			sb.WriteString("\n**Stderr:**\n```\n")
			sb.WriteString(res.Stderr)
			if !strings.HasSuffix(res.Stderr, "\n") {
				sb.WriteString("\n")
			}
			sb.WriteString("```\n")
		}

		if err != nil && res.ExitCode == 0 {
			sb.WriteString(fmt.Sprintf("\n**Execution error:** %v\n", err))
		}
		return sb.String(), nil

	case "dockdeploy_docker_exec":
		serverID := getString(args, "server_id")
		containerID := getString(args, "container_id")
		if containerID == "" {
			return "", fmt.Errorf("container_id is required")
		}
		command := getString(args, "command")
		if command == "" {
			return "", fmt.Errorf("command is required")
		}
		user := getString(args, "user")
		workdir := getString(args, "workdir")
		sudo := getBool(args, "sudo", false)
		timeoutSec := getInt(args, "timeout_seconds", 60)
		if timeoutSec <= 0 || timeoutSec > 600 {
			timeoutSec = 60
		}

		server, err := s.resolveServer(ctx, serverID)
		if err != nil {
			return "", err
		}

		cmdCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSec)*time.Second)
		defer cancel()

		var dockerArgs []string
		dockerArgs = append(dockerArgs, "docker", "exec", "-i")
		if user != "" {
			dockerArgs = append(dockerArgs, "-u", servers.ShellQuote(user))
		}
		if workdir != "" {
			dockerArgs = append(dockerArgs, "-w", servers.ShellQuote(workdir))
		}
		dockerArgs = append(dockerArgs, servers.ShellQuote(containerID), "sh", "-c", servers.ShellQuote(command))
		fullExecCmd := strings.Join(dockerArgs, " ")

		start := time.Now()
		res, err := s.Servers.RunCommand(cmdCtx, server, fullExecCmd, sudo)
		duration := time.Since(start).Round(time.Millisecond)

		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("Docker Exec on container `%s` (server: **%s**)\n", containerID, server.Name))
		sb.WriteString(fmt.Sprintf("- Duration: `%s`\n", duration))
		sb.WriteString(fmt.Sprintf("- Exit Code: `%d`\n\n", res.ExitCode))

		if strings.TrimSpace(res.Stdout) != "" {
			sb.WriteString("```\n")
			sb.WriteString(res.Stdout)
			if !strings.HasSuffix(res.Stdout, "\n") {
				sb.WriteString("\n")
			}
			sb.WriteString("```\n")
		} else {
			sb.WriteString("*(stdout empty)*\n")
		}

		if strings.TrimSpace(res.Stderr) != "" {
			sb.WriteString("\n**Stderr:**\n```\n")
			sb.WriteString(res.Stderr)
			if !strings.HasSuffix(res.Stderr, "\n") {
				sb.WriteString("\n")
			}
			sb.WriteString("```\n")
		}

		if err != nil && res.ExitCode == 0 {
			sb.WriteString(fmt.Sprintf("\n**Execution error:** %v\n", err))
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

	case "dockdeploy_inspect_container":
		serverID := getString(args, "server_id")
		containerID := getString(args, "container_id")
		if containerID == "" {
			return "", fmt.Errorf("container_id is required")
		}

		server, err := s.resolveServer(ctx, serverID)
		if err != nil {
			return "", err
		}

		session, err := s.Servers.Session(ctx, server)
		if err != nil {
			return "", fmt.Errorf("connect to server: %w", err)
		}
		defer session.Close()

		inspect, err := session.Docker.InspectContainer(ctx, containerID)
		if err != nil {
			return "", fmt.Errorf("inspect container: %w", err)
		}

		summary := map[string]any{
			"id":             inspect.ID,
			"name":           inspect.Name,
			"created":        inspect.Created,
			"state":          inspect.State,
			"restart_policy": inspect.HostConfig.RestartPolicy,
		}
		if inspect.Config != nil {
			summary["image"] = inspect.Config.Image
			summary["cmd"] = inspect.Config.Cmd
			summary["env"] = inspect.Config.Env
		}
		if inspect.NetworkSettings != nil {
			summary["ports"] = inspect.NetworkSettings.Ports
			summary["ip_address"] = inspect.NetworkSettings.IPAddress
		}
		summary["mounts"] = inspect.Mounts

		data, err := json.MarshalIndent(summary, "", "  ")
		if err != nil {
			return "", err
		}

		return fmt.Sprintf("Inspection summary for container `%s` on `%s`:\n\n```json\n%s\n```",
			containerID, server.Name, string(data)), nil

	case "dockdeploy_container_action":
		serverID := getString(args, "server_id")
		containerID := getString(args, "container_id")
		action := strings.ToLower(getString(args, "action"))
		if containerID == "" {
			return "", fmt.Errorf("container_id is required")
		}

		validActions := map[string]dockerx.Action{
			"start":   dockerx.ActionStart,
			"stop":    dockerx.ActionStop,
			"restart": dockerx.ActionRestart,
			"pause":   dockerx.ActionPause,
			"unpause": dockerx.ActionUnpause,
		}
		act, ok := validActions[action]
		if !ok {
			return "", fmt.Errorf("invalid action %q (supported: start, stop, restart, pause, unpause)", action)
		}

		server, err := s.resolveServer(ctx, serverID)
		if err != nil {
			return "", err
		}

		session, err := s.Servers.Session(ctx, server)
		if err != nil {
			return "", fmt.Errorf("connect to server: %w", err)
		}
		defer session.Close()

		if err := session.Docker.Do(ctx, act, containerID); err != nil {
			return "", fmt.Errorf("execute %s on container %s: %w", action, containerID, err)
		}

		return fmt.Sprintf("Action `%s` successfully executed on container `%s` on server `%s`.", action, containerID, server.Name), nil

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

	case "dockdeploy_read_file":
		serverID := getString(args, "server_id")
		path := getString(args, "path")
		if path == "" {
			return "", fmt.Errorf("path is required")
		}
		maxBytes := int64(getInt(args, "max_bytes", 65536))
		if maxBytes <= 0 || maxBytes > 2*1024*1024 {
			maxBytes = 65536
		}

		server, err := s.resolveServer(ctx, serverID)
		if err != nil {
			return "", err
		}

		fileContent, err := s.Servers.ReadFileContent(ctx, server, path, maxBytes)
		if err != nil {
			return "", fmt.Errorf("read file %q: %w", path, err)
		}

		if fileContent.IsBinary {
			return fmt.Sprintf("File `%s` (%d bytes, mode %s) is binary and cannot be displayed as text.",
				fileContent.Path, fileContent.Size, fileContent.Permissions), nil
		}

		return fmt.Sprintf("File `%s` (%d bytes, mode %s, modified %s):\n\n```\n%s\n```",
			fileContent.Path, fileContent.Size, fileContent.Permissions, fileContent.ModTime.Format(time.RFC3339), fileContent.Content), nil

	case "dockdeploy_write_file":
		serverID := getString(args, "server_id")
		path := getString(args, "path")
		if path == "" {
			return "", fmt.Errorf("path is required")
		}
		content := getString(args, "content")
		sudoPassword := getString(args, "sudo_password")

		server, err := s.resolveServer(ctx, serverID)
		if err != nil {
			return "", err
		}

		if err := s.Servers.WriteFileContent(ctx, server, path, content, sudoPassword); err != nil {
			return "", fmt.Errorf("write file %q: %w", path, err)
		}

		return fmt.Sprintf("File `%s` successfully written on server `%s` (%d bytes).", path, server.Name, len(content)), nil

	case "dockdeploy_list_files":
		serverID := getString(args, "server_id")
		path := getString(args, "path")
		if path == "" {
			path = "/"
		}

		server, err := s.resolveServer(ctx, serverID)
		if err != nil {
			return "", err
		}

		listing, err := s.Servers.ListFiles(ctx, server, path)
		if err != nil {
			return "", fmt.Errorf("list files in %q: %w", path, err)
		}

		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("Directory listing for `%s` on **%s** (%d files, %d dirs):\n\n",
			listing.Path, server.Name, listing.TotalFiles, listing.TotalDirs))
		sb.WriteString("| Name | Type | Size | Permissions | Modified |\n")
		sb.WriteString("| --- | --- | --- | --- | --- |\n")
		for _, entry := range listing.Entries {
			entryType := "file"
			if entry.IsDir {
				entryType = "dir"
			} else if entry.IsSymlink {
				entryType = "symlink"
			}
			sb.WriteString(fmt.Sprintf("| `%s` | %s | %d B | `%s` | %s |\n",
				entry.Name, entryType, entry.Size, entry.Permissions, entry.ModTime.Format("2006-01-02 15:04:05")))
		}
		return sb.String(), nil

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

	case "dockdeploy_trigger_deployment":
		deploymentID := getString(args, "deployment_id")
		if deploymentID == "" {
			return "", fmt.Errorf("deployment_id is required")
		}

		dep, err := s.Store.DeploymentByID(ctx, deploymentID)
		if err != nil {
			return "", fmt.Errorf("deployment not found: %w", err)
		}

		run, err := s.Store.EnqueueRun(ctx, dep.ID, store.TriggerAPI, nil)
		if err != nil {
			return "", fmt.Errorf("enqueue run: %w", err)
		}

		return fmt.Sprintf("Triggered deployment run #%d (`%s`) for application **%s** (`%s`). Status: `%s`.",
			run.Number, run.ID, dep.Name, dep.ID, run.Status), nil

	case "dockdeploy_get_deployment_logs":
		deploymentID := getString(args, "deployment_id")
		if deploymentID == "" {
			return "", fmt.Errorf("deployment_id is required")
		}

		runID := getString(args, "run_id")
		if runID == "" {
			runs, err := s.Store.ListRuns(ctx, deploymentID, 1)
			if err != nil || len(runs) == 0 {
				return "", fmt.Errorf("no deployment runs found for deployment %s", deploymentID)
			}
			runID = runs[0].ID
		}

		run, err := s.Store.RunByID(ctx, runID)
		if err != nil {
			return "", fmt.Errorf("run not found: %w", err)
		}

		logs, err := s.Store.ListLogs(ctx, runID, 0, 500)
		if err != nil {
			return "", fmt.Errorf("fetch logs: %w", err)
		}

		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("Logs for Run #%d (`%s`) - Status: `%s`:\n\n```\n", run.Number, run.ID, run.Status))
		for _, entry := range logs {
			sb.WriteString(fmt.Sprintf("[%s] %s\n", entry.TS.Format("15:04:05"), entry.Line))
		}
		sb.WriteString("```")
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

	case "dockdeploy_security_audit":
		serverID := getString(args, "server_id")
		server, err := s.resolveServer(ctx, serverID)
		if err != nil {
			return "", err
		}

		audit, err := s.Servers.SecurityAudit(ctx, server)
		if err != nil {
			return "", fmt.Errorf("run security audit: %w", err)
		}

		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("## Security Audit Report: **%s** (`%s`)\n\n", server.Name, server.ID))
		sb.WriteString(fmt.Sprintf("- **Score**: `%d/100` (Grade: `%s`)\n", audit.Score, audit.Grade))
		sb.WriteString(fmt.Sprintf("- **Status**: `%s`\n", audit.Status))
		sb.WriteString(fmt.Sprintf("- **Summary**: %s\n", audit.Summary))
		sb.WriteString(fmt.Sprintf("- **Findings**: %d Critical, %d High, %d Medium, %d Low\n\n",
			audit.CriticalCount, audit.HighCount, audit.MediumCount, audit.LowCount))

		if len(audit.Findings) > 0 {
			sb.WriteString("### Detected Vulnerabilities & Threats:\n\n")
			for i, f := range audit.Findings {
				sb.WriteString(fmt.Sprintf("%d. **[%s] %s**\n", i+1, strings.ToUpper(string(f.Severity)), f.Title))
				sb.WriteString(fmt.Sprintf("   - *Description*: %s\n", f.Description))
				sb.WriteString(fmt.Sprintf("   - *Evidence*: `%s`\n", f.Evidence))
				sb.WriteString(fmt.Sprintf("   - *Remediation*: %s\n\n", f.Remediation))
			}
		} else {
			sb.WriteString("✅ No security vulnerabilities or attack patterns detected.\n\n")
		}

		if audit.SSHAnalysis.FailedAttemptsCount > 0 {
			sb.WriteString(fmt.Sprintf("### SSH Brute-Force Activity:\n- Failed Login Attempts: %d\n", audit.SSHAnalysis.FailedAttemptsCount))
			if len(audit.SSHAnalysis.TopAttackerIPs) > 0 {
				sb.WriteString("- Top Attacking IPs: ")
				var ipStrs []string
				for _, ip := range audit.SSHAnalysis.TopAttackerIPs {
					ipStrs = append(ipStrs, fmt.Sprintf("%s (%d hits)", ip.IP, ip.Count))
				}
				sb.WriteString(strings.Join(ipStrs, ", ") + "\n\n")
			}
		}

		return sb.String(), nil

	default:
		return "", fmt.Errorf("unknown tool: %s", name)
	}
}

func (s *Server) resolveServer(ctx context.Context, serverID string) (*store.Server, error) {
	if serverID != "" && serverID != "local" {
		return s.Store.ServerByID(ctx, serverID)
	}
	serversList, err := s.Store.ListServers(ctx, "", true)
	if err != nil {
		return nil, err
	}
	if len(serversList) == 0 {
		localServer, createErr := s.Store.CreateServer(ctx, s.Sealer, store.NewServer{
			Name:               "Local Server",
			Host:               "localhost",
			Port:               0,
			Username:           "local",
			AuthMethod:         sshx.AuthLocal,
			HostKeyFingerprint: "local",
			DockerSocket:       "/var/run/docker.sock",
		})
		if createErr == nil {
			return localServer, nil
		}
		return &store.Server{
			ID:           "local",
			Name:         "Local Server",
			Host:         "localhost",
			Port:         0,
			Username:     "local",
			AuthMethod:   sshx.AuthLocal,
			DockerSocket: "/var/run/docker.sock",
			Status:       store.ServerOnline,
		}, nil
	}
	// Prefer local server
	for i := range serversList {
		if serversList[i].AuthMethod == sshx.AuthLocal || serversList[i].ID == serverID {
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

func getBool(args map[string]any, key string, fallback bool) bool {
	if v, ok := args[key]; ok {
		if b, ok := v.(bool); ok {
			return b
		}
		if s, ok := v.(string); ok {
			return strings.EqualFold(s, "true") || s == "1"
		}
	}
	return fallback
}
