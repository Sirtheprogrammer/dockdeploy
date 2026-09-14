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

func defaultBaseURL(provider string) string {
	switch normalizeProvider(provider) {
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

// TestConnection verifies that the API key and model work by issuing a small single-token test request.
func (c *Client) TestConnection(ctx context.Context, opts StreamOptions) (string, error) {
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

	if p == "anthropic" {
		return c.streamAnthropic(ctx, baseURL, model, opts, onChunk)
	}
	return c.streamOpenAICompatible(ctx, baseURL, model, opts, onChunk)
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
