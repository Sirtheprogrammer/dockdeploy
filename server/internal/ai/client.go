package ai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strings"
	"time"
)

type ChatMessage struct {
	Role    string `json:"role"` // "system", "user", "assistant"
	Content string `json:"content"`
}

type StreamOptions struct {
	Provider     string
	Model        string
	BaseURL      string
	APIKey       string
	Temperature  float64
	SystemPrompt string
	Messages     []ChatMessage
}

type Client struct {
	httpClient *http.Client
}

func NewClient() *Client {
	return &Client{
		httpClient: &http.Client{
			Timeout: 120 * time.Second,
		},
	}
}

func normalizeProvider(provider string) string {
	p := strings.ToLower(strings.TrimSpace(provider))
	switch p {
	case "ollama":
		return "ollama"
	case "antigravity", "agy":
		return "antigravity"
	case "claude-code", "claude_code", "claude_cli":
		return "claude-code"
	case "hermes", "hermes-agent":
		return "hermes"
	case "copilot", "github-copilot":
		return "copilot"
	case "lmstudio":
		return "lmstudio"
	case "localai":
		return "localai"
	case "claude", "anthropic":
		return "anthropic"
	case "deepseek":
		return "deepseek"
	case "openrouter":
		return "openrouter"
	case "gemini", "google":
		return "gemini"
	case "custom":
		return "custom"
	default:
		return "openai"
	}
}

// IsLocalProvider returns true if the provider can run locally without requiring external API keys.
func IsLocalProvider(provider string) bool {
	p := normalizeProvider(provider)
	switch p {
	case "ollama", "antigravity", "claude-code", "hermes", "copilot", "lmstudio", "localai", "custom":
		return true
	default:
		return false
	}
}

// IsCLIProvider returns true if the provider is executed as a local agent CLI command.
func IsCLIProvider(provider string) bool {
	p := normalizeProvider(provider)
	switch p {
	case "antigravity", "claude-code", "hermes", "copilot":
		return true
	default:
		return false
	}
}

func defaultBaseURL(provider string) string {
	switch normalizeProvider(provider) {
	case "ollama":
		return "http://localhost:11434/v1"
	case "lmstudio":
		return "http://localhost:1234/v1"
	case "localai":
		return "http://localhost:8080/v1"
	case "antigravity":
		return "local://agy"
	case "claude-code":
		return "local://claude"
	case "hermes":
		return "local://hermes"
	case "copilot":
		return "local://copilot"
	case "anthropic":
		return "https://api.anthropic.com/v1"
	case "deepseek":
		return "https://api.deepseek.com/v1"
	case "openrouter":
		return "https://openrouter.ai/api/v1"
	case "gemini":
		return "https://generativelanguage.googleapis.com/v1beta/openai"
	case "openai":
		return "https://api.openai.com/v1"
	default:
		return "https://api.openai.com/v1"
	}
}

func defaultModel(provider string) string {
	switch normalizeProvider(provider) {
	case "ollama":
		return "gemma3:1b"
	case "antigravity":
		return "agy-default"
	case "claude-code":
		return "claude-code"
	case "hermes":
		return "hermes-agent"
	case "copilot":
		return "copilot-cli"
	case "lmstudio", "localai":
		return "default"
	case "anthropic":
		return "claude-3-7-sonnet-20250219"
	case "deepseek":
		return "deepseek-chat"
	case "openrouter":
		return "anthropic/claude-3.5-sonnet"
	case "gemini":
		return "gemini-2.0-flash"
	case "openai":
		return "gpt-4o"
	default:
		return "gpt-4o"
	}
}

// TestConnection verifies that the API key and model work by issuing a test request or probing the local agent.
func (c *Client) TestConnection(ctx context.Context, opts StreamOptions) (string, error) {
	p := normalizeProvider(opts.Provider)
	if IsCLIProvider(p) {
		var binName string
		switch p {
		case "antigravity":
			binName = "agy"
		case "claude-code":
			binName = "claude"
		case "hermes":
			binName = "hermes"
		case "copilot":
			binName = "copilot"
		}
		path := findBinary(binName)
		if path == "" && p == "copilot" {
			path = findBinary("github-copilot-cli")
		}
		if path == "" {
			return "", fmt.Errorf("local agent CLI '%s' not found on PATH", binName)
		}
		return fmt.Sprintf("Local CLI agent '%s' ready at %s", p, path), nil
	}

	opts.Messages = []ChatMessage{
		{Role: "user", Content: "Reply with the exact word 'READY' and nothing else."},
	}
	opts.SystemPrompt = "You are a test validator."

	var fullResponse strings.Builder
	err := c.StreamCompletion(ctx, opts, func(chunk string) error {
		fullResponse.WriteString(chunk)
		return nil
	})
	if err != nil {
		return "", err
	}
	res := strings.TrimSpace(fullResponse.String())
	if res == "" {
		return "OK", nil
	}
	return res, nil
}

// StreamCompletion sends the request to the configured provider and streams text chunks back.
func (c *Client) StreamCompletion(ctx context.Context, opts StreamOptions, onChunk func(chunk string) error) error {
	p := normalizeProvider(opts.Provider)
	baseURL := strings.TrimRight(strings.TrimSpace(opts.BaseURL), "/")
	if baseURL == "" {
		baseURL = defaultBaseURL(p)
	}
	model := strings.TrimSpace(opts.Model)
	if model == "" {
		model = defaultModel(p)
	}
	if opts.Temperature <= 0 || opts.Temperature > 2.0 {
		opts.Temperature = 0.7
	}

	if IsCLIProvider(p) {
		return c.streamCLIAgent(ctx, p, opts, onChunk)
	}
	if p == "anthropic" {
		return c.streamAnthropic(ctx, baseURL, model, opts, onChunk)
	}
	return c.streamOpenAICompatible(ctx, baseURL, model, opts, onChunk)
}

// streamCLIAgent executes a local CLI agent directly (Antigravity, Claude Code, Hermes, Copilot).
func (c *Client) streamCLIAgent(
	ctx context.Context,
	provider string,
	opts StreamOptions,
	onChunk func(chunk string) error,
) error {
	var sb strings.Builder
	if opts.SystemPrompt != "" {
		sb.WriteString("=== SYSTEM INSTRUCTIONS ===\n")
		sb.WriteString(opts.SystemPrompt)
		sb.WriteString("\n\n")
	}
	if len(opts.Messages) > 0 {
		sb.WriteString("=== CONVERSATION HISTORY ===\n")
		for _, m := range opts.Messages {
			sb.WriteString(fmt.Sprintf("%s: %s\n\n", strings.ToUpper(m.Role), m.Content))
		}
	}
	prompt := strings.TrimSpace(sb.String())

	var cmdName string
	var args []string

	switch provider {
	case "antigravity":
		cmdName = findBinary("agy")
		if cmdName == "" {
			return fmt.Errorf("ai: antigravity CLI ('agy') not found on system")
		}
		args = []string{"-p", prompt, "--print-timeout", "3m", "--dangerously-skip-permissions"}

	case "claude-code":
		cmdName = findBinary("claude")
		if cmdName == "" {
			return fmt.Errorf("ai: claude code CLI ('claude') not found on system")
		}
		args = []string{"-p", prompt}

	case "hermes":
		cmdName = findBinary("hermes")
		if cmdName == "" {
			return fmt.Errorf("ai: hermes CLI ('hermes') not found on system")
		}
		args = []string{"chat", "-q", prompt, "-Q"}
		if opts.Model != "" && opts.Model != "default" && opts.Model != "hermes-agent" {
			args = append(args, "-m", opts.Model)
		}

	case "copilot":
		cmdName = findBinary("copilot")
		if cmdName == "" {
			cmdName = findBinary("github-copilot-cli")
		}
		if cmdName == "" {
			return fmt.Errorf("ai: github copilot CLI not found on system")
		}
		args = []string{"-p", prompt}

	default:
		return fmt.Errorf("ai: unsupported CLI agent '%s'", provider)
	}

	execCmd := exec.CommandContext(ctx, cmdName, args...)
	stdout, err := execCmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("ai: create stdout pipe: %w", err)
	}

	if err := execCmd.Start(); err != nil {
		return fmt.Errorf("ai: start %s: %w", cmdName, err)
	}

	reader := bufio.NewReader(stdout)
	buf := make([]byte, 1024)
	for {
		n, rErr := reader.Read(buf)
		if n > 0 {
			chunk := string(buf[:n])
			if cErr := onChunk(chunk); cErr != nil {
				_ = execCmd.Process.Kill()
				return cErr
			}
		}
		if rErr != nil {
			if errors.Is(rErr, io.EOF) {
				break
			}
			return rErr
		}
	}

	if err := execCmd.Wait(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("ai: %s exited with error: %w", cmdName, err)
	}

	return nil
}

// streamOpenAICompatible handles OpenAI, DeepSeek, OpenRouter, Gemini, and Custom OpenAI endpoints.
func (c *Client) streamOpenAICompatible(
	ctx context.Context,
	baseURL, model string,
	opts StreamOptions,
	onChunk func(chunk string) error,
) error {
	endpoint := baseURL + "/chat/completions"

	type reqMsg struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	var messages []reqMsg
	if opts.SystemPrompt != "" {
		messages = append(messages, reqMsg{Role: "system", Content: opts.SystemPrompt})
	}
	for _, m := range opts.Messages {
		if strings.TrimSpace(m.Content) != "" {
			messages = append(messages, reqMsg{Role: m.Role, Content: m.Content})
		}
	}

	reqBody := map[string]any{
		"model":       model,
		"messages":    messages,
		"temperature": opts.Temperature,
		"stream":      true,
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("ai: marshal openai request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(bodyBytes))
	if err != nil {
		return fmt.Errorf("ai: create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if opts.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+opts.APIKey)
	}
	if normalizeProvider(opts.Provider) == "openrouter" {
		req.Header.Set("HTTP-Referer", "https://dockdeploy.dev")
		req.Header.Set("X-Title", "dockdeploy")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("ai: request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("ai: %s API returned status %d: %s", opts.Provider, resp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	scanner := bufio.NewScanner(resp.Body)
	// Allow larger buffers for long chunks
	buf := make([]byte, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, ":") {
			continue // comment or keep-alive
		}

		if !strings.HasPrefix(line, "data:") {
			continue
		}

		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			break
		}

		var chunk struct {
			Choices []struct {
				Delta struct {
					Content string `json:"content"`
				} `json:"delta"`
				FinishReason *string `json:"finish_reason"`
			} `json:"choices"`
			Error *struct {
				Message string `json:"message"`
			} `json:"error"`
		}

		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}

		if chunk.Error != nil && chunk.Error.Message != "" {
			return errors.New(chunk.Error.Message)
		}

		if len(chunk.Choices) > 0 {
			content := chunk.Choices[0].Delta.Content
			if content != "" {
				if err := onChunk(content); err != nil {
					return err
				}
			}
		}
	}

	return scanner.Err()
}

// streamAnthropic handles Anthropic Claude Messages API.
func (c *Client) streamAnthropic(
	ctx context.Context,
	baseURL, model string,
	opts StreamOptions,
	onChunk func(chunk string) error,
) error {
	endpoint := baseURL + "/messages"

	type anthropicMsg struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}

	var messages []anthropicMsg
	for _, m := range opts.Messages {
		if m.Role == "system" {
			continue
		}
		role := m.Role
		if role != "user" && role != "assistant" {
			role = "user"
		}
		if strings.TrimSpace(m.Content) != "" {
			messages = append(messages, anthropicMsg{Role: role, Content: m.Content})
		}
	}

	if len(messages) == 0 {
		messages = append(messages, anthropicMsg{Role: "user", Content: "Hello"})
	}

	reqBody := map[string]any{
		"model":       model,
		"max_tokens":  4096,
		"messages":    messages,
		"temperature": opts.Temperature,
		"stream":      true,
	}
	if opts.SystemPrompt != "" {
		reqBody["system"] = opts.SystemPrompt
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("ai: marshal anthropic request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(bodyBytes))
	if err != nil {
		return fmt.Errorf("ai: create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", opts.APIKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("ai: anthropic request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("ai: Anthropic API returned status %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	scanner := bufio.NewScanner(resp.Body)
	buf := make([]byte, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	var currentEvent string

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		if strings.HasPrefix(line, "event:") {
			currentEvent = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
			continue
		}

		if strings.HasPrefix(line, "data:") {
			data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if currentEvent == "message_stop" {
				break
			}

			if currentEvent == "content_block_delta" {
				var event struct {
					Delta struct {
						Type string `json:"type"`
						Text string `json:"text"`
					} `json:"delta"`
				}
				if err := json.Unmarshal([]byte(data), &event); err == nil {
					if event.Delta.Text != "" {
						if err := onChunk(event.Delta.Text); err != nil {
							return err
						}
					}
				}
			} else if currentEvent == "error" {
				var errEvent struct {
					Error struct {
						Message string `json:"message"`
					} `json:"error"`
				}
				if err := json.Unmarshal([]byte(data), &errEvent); err == nil {
					return errors.New(errEvent.Error.Message)
				}
			}
		}
	}

	return scanner.Err()
}
