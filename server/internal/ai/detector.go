package ai

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// DetectedAgent represents a discovered local AI service or agent binary.
type DetectedAgent struct {
	ID           string   `json:"id"`            // "ollama", "antigravity", "claude", "hermes", "copilot", "lmstudio"
	Name         string   `json:"name"`          // Human-readable title
	Type         string   `json:"type"`          // "http_llm" or "cli_agent"
	Available    bool     `json:"available"`     // Whether the agent is online or executable
	Status       string   `json:"status"`        // "online", "ready", "offline", "not_found"
	Endpoint     string   `json:"endpoint,omitempty"`
	Command      string   `json:"command,omitempty"`
	Path         string   `json:"path,omitempty"`
	Version      string   `json:"version,omitempty"`
	Models       []string `json:"models,omitempty"`
	DefaultModel string   `json:"default_model,omitempty"`
	Description  string   `json:"description"`
}

// DetectionResult groups all probed local agents and recommends a default.
type DetectionResult struct {
	Agents         []DetectedAgent `json:"agents"`
	RecommendedID  string          `json:"recommended_id,omitempty"`
	HasLocalAgents bool            `json:"has_local_agents"`
}

// DetectAgents inspects the local machine for running LLM daemons and installed agent CLIs.
func DetectAgents(ctx context.Context) DetectionResult {
	var agents []DetectedAgent

	// 1. Probe Ollama (standard port 11434)
	ollamaAgent := probeOllama(ctx)
	agents = append(agents, ollamaAgent)

	// 2. Probe Antigravity CLI (agy)
	agyAgent := probeAntigravity(ctx)
	agents = append(agents, agyAgent)

	// 3. Probe Claude Code CLI (claude)
	claudeAgent := probeClaude(ctx)
	agents = append(agents, claudeAgent)

	// 4. Probe Hermes Agent (hermes)
	hermesAgent := probeHermes(ctx)
	agents = append(agents, hermesAgent)

	// 5. Probe GitHub Copilot CLI
	copilotAgent := probeCopilot(ctx)
	agents = append(agents, copilotAgent)

	// 6. Probe LM Studio (standard port 1234)
	lmStudioAgent := probeHTTPLLM(ctx, "lmstudio", "LM Studio", "http://localhost:1234/v1", "http://localhost:1234/v1/models")
	if lmStudioAgent.Available {
		agents = append(agents, lmStudioAgent)
	}

	// 7. Probe LocalAI (standard port 8080)
	localAIAgent := probeHTTPLLM(ctx, "localai", "LocalAI", "http://localhost:8080/v1", "http://localhost:8080/v1/models")
	if localAIAgent.Available {
		agents = append(agents, localAIAgent)
	}

	// Determine recommended agent
	var recommended string
	var hasLocal bool
	for _, a := range agents {
		if a.Available {
			hasLocal = true
			if recommended == "" {
				recommended = a.ID
			}
			// Prefer Ollama if it has loaded models, or Antigravity/Claude
			if a.ID == "ollama" && len(a.Models) > 0 {
				recommended = a.ID
				break
			}
			if a.ID == "antigravity" && recommended != "ollama" {
				recommended = a.ID
			}
		}
	}

	return DetectionResult{
		Agents:         agents,
		RecommendedID:  recommended,
		HasLocalAgents: hasLocal,
	}
}

func probeOllama(ctx context.Context) DetectedAgent {
	agent := DetectedAgent{
		ID:          "ollama",
		Name:        "Ollama",
		Type:        "http_llm",
		Endpoint:    "http://localhost:11434/v1",
		Status:      "offline",
		Description: "Local LLM server running models like Gemma, Llama, and Mistral with zero API keys.",
	}

	client := &http.Client{Timeout: 1500 * time.Millisecond}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://localhost:11434/api/tags", nil)
	if err != nil {
		return agent
	}

	resp, err := client.Do(req)
	if err != nil {
		// Try 127.0.0.1 as fallback
		req2, err2 := http.NewRequestWithContext(ctx, http.MethodGet, "http://127.0.0.1:11434/api/tags", nil)
		if err2 == nil {
			resp2, err3 := client.Do(req2)
			if err3 == nil {
				resp = resp2
				agent.Endpoint = "http://127.0.0.1:11434/v1"
				err = nil
			}
		}
	}

	if err != nil || resp == nil {
		return agent
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		agent.Available = true
		agent.Status = "online"

		var tagsResp struct {
			Models []struct {
				Name  string `json:"name"`
				Model string `json:"model"`
			} `json:"models"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&tagsResp); err == nil {
			for _, m := range tagsResp.Models {
				name := m.Name
				if name == "" {
					name = m.Model
				}
				if name != "" {
					agent.Models = append(agent.Models, name)
				}
			}
			if len(agent.Models) > 0 {
				agent.DefaultModel = agent.Models[0]
			}
		}
	}

	return agent
}

func probeAntigravity(ctx context.Context) DetectedAgent {
	agent := DetectedAgent{
		ID:          "antigravity",
		Name:        "Antigravity CLI (agy)",
		Type:        "cli_agent",
		Status:      "not_found",
		Description: "Google Antigravity CLI agent for autonomous DevOps workflows and reasoning.",
	}

	path := findBinary("agy")
	if path == "" {
		return agent
	}

	agent.Path = path
	agent.Command = "agy"
	agent.Available = true
	agent.Status = "ready"
	agent.DefaultModel = "agy-default"
	agent.Models = []string{"agy-default", "agy-pro", "agy-flash"}

	// Attempt to get version
	ctxTimeout, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctxTimeout, path, "--version")
	if out, err := cmd.Output(); err == nil {
		agent.Version = strings.TrimSpace(string(out))
	}

	return agent
}

func probeClaude(ctx context.Context) DetectedAgent {
	agent := DetectedAgent{
		ID:          "claude-code",
		Name:        "Claude Code CLI",
		Type:        "cli_agent",
		Status:      "not_found",
		Description: "Anthropic's Claude Code local CLI assistant.",
	}

	path := findBinary("claude")
	if path == "" {
		return agent
	}

	agent.Path = path
	agent.Command = "claude"
	agent.Available = true
	agent.Status = "ready"
	agent.DefaultModel = "claude-code"
	agent.Models = []string{"claude-code"}

	ctxTimeout, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctxTimeout, path, "--version")
	if out, err := cmd.Output(); err == nil {
		agent.Version = strings.TrimSpace(string(out))
	}

	return agent
}

func probeHermes(ctx context.Context) DetectedAgent {
	agent := DetectedAgent{
		ID:          "hermes",
		Name:        "Hermes Agent",
		Type:        "cli_agent",
		Status:      "not_found",
		Description: "Hermes Agent with tool-calling capabilities and local inference support.",
	}

	path := findBinary("hermes")
	if path == "" {
		return agent
	}

	agent.Path = path
	agent.Command = "hermes"
	agent.Available = true
	agent.Status = "ready"
	agent.DefaultModel = "hermes-agent"
	agent.Models = []string{"hermes-agent"}

	ctxTimeout, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctxTimeout, path, "--version")
	if out, err := cmd.Output(); err == nil {
		agent.Version = strings.TrimSpace(string(out))
	}

	return agent
}

func probeCopilot(ctx context.Context) DetectedAgent {
	agent := DetectedAgent{
		ID:          "copilot",
		Name:        "GitHub Copilot",
		Type:        "cli_agent",
		Status:      "not_found",
		Description: "GitHub Copilot agent via CLI or local language model proxy.",
	}

	// Look for copilot binary in path or ~/.local/bin
	path := findBinary("copilot")
	if path == "" {
		path = findBinary("github-copilot-cli")
	}

	// Also check VS Code / Code global storage if available
	if path == "" {
		home, _ := os.UserHomeDir()
		codeCli := filepath.Join(home, ".config/Code/User/globalStorage/github.copilot-chat/copilotCli/copilot")
		if info, err := os.Stat(codeCli); err == nil && !info.IsDir() {
			path = codeCli
		}
	}

	if path != "" {
		agent.Path = path
		agent.Command = filepath.Base(path)
		agent.Available = true
		agent.Status = "ready"
		agent.DefaultModel = "copilot-cli"
		agent.Models = []string{"copilot-cli"}
	}

	return agent
}

func probeHTTPLLM(ctx context.Context, id, name, baseURL, testURL string) DetectedAgent {
	agent := DetectedAgent{
		ID:          id,
		Name:        name,
		Type:        "http_llm",
		Endpoint:    baseURL,
		Status:      "offline",
		Description: name + " OpenAI-compatible local API server.",
	}

	client := &http.Client{Timeout: 1200 * time.Millisecond}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, testURL, nil)
	if err != nil {
		return agent
	}

	resp, err := client.Do(req)
	if err != nil || resp == nil {
		return agent
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		agent.Available = true
		agent.Status = "online"
		agent.DefaultModel = "default"

		var modelResp struct {
			Data []struct {
				ID string `json:"id"`
			} `json:"data"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&modelResp); err == nil {
			for _, m := range modelResp.Data {
				if m.ID != "" {
					agent.Models = append(agent.Models, m.ID)
				}
			}
			if len(agent.Models) > 0 {
				agent.DefaultModel = agent.Models[0]
			}
		}
	}

	return agent
}

func findBinary(name string) string {
	// Check PATH
	if p, err := exec.LookPath(name); err == nil {
		return p
	}

	// Check common user-level install directories
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}

	candidates := []string{
		filepath.Join(home, ".local", "bin", name),
		filepath.Join(home, "bin", name),
		filepath.Join("/usr/local/bin", name),
		filepath.Join("/usr/bin", name),
	}

	for _, c := range candidates {
		if info, err := os.Stat(c); err == nil && !info.IsDir() {
			return c
		}
	}

	return ""
}
