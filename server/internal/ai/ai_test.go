package ai

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNginxGenerator(t *testing.T) {
	spec := NginxConfigSpec{
		Domain:            "app.example.com",
		UpstreamPort:      3000,
		EnableSSL:         true,
		EnableWebSocket:   true,
		ClientMaxBodySize: "100M",
		IncludeSecurity:   true,
	}

	conf := GenerateSafeNginxConfig(spec)

	directives := []string{
		"server_name app.example.com;",
		"proxy_pass http://127.0.0.1:3000;",
		"proxy_set_header Upgrade $http_upgrade;",
		"proxy_set_header Connection \"upgrade\";",
		"client_max_body_size 100M;",
		"/.well-known/acme-challenge/",
		"ssl_certificate /etc/letsencrypt/live/app.example.com/fullchain.pem;",
		"ssl_certificate_key /etc/letsencrypt/live/app.example.com/privkey.pem;",
		"X-Content-Type-Options nosniff",
	}

	for _, d := range directives {
		if !strings.Contains(conf, d) {
			t.Errorf("expected generated config to contain %q, but was missing", d)
		}
	}
}

func TestProviderDefaults(t *testing.T) {
	cases := []struct {
		provider        string
		expectedNorm    string
		expectedBaseURL string
		expectedModel   string
	}{
		{"openai", "openai", "https://api.openai.com/v1", "gpt-4o"},
		{"Claude", "anthropic", "https://api.anthropic.com/v1", "claude-3-7-sonnet-20250219"},
		{"Anthropic", "anthropic", "https://api.anthropic.com/v1", "claude-3-7-sonnet-20250219"},
		{"deepseek", "deepseek", "https://api.deepseek.com/v1", "deepseek-chat"},
		{"openrouter", "openrouter", "https://openrouter.ai/api/v1", "anthropic/claude-3.5-sonnet"},
		{"gemini", "gemini", "https://generativelanguage.googleapis.com/v1beta/openai", "gemini-2.0-flash"},
	}

	for _, tc := range cases {
		norm := normalizeProvider(tc.provider)
		if norm != tc.expectedNorm {
			t.Errorf("[%s] norm: got %s, want %s", tc.provider, norm, tc.expectedNorm)
		}
		base := defaultBaseURL(norm)
		if base != tc.expectedBaseURL {
			t.Errorf("[%s] baseURL: got %s, want %s", tc.provider, base, tc.expectedBaseURL)
		}
		model := defaultModel(norm)
		if model != tc.expectedModel {
			t.Errorf("[%s] model: got %s, want %s", tc.provider, model, tc.expectedModel)
		}
	}
}

func TestMockStreaming_OpenAICompatible(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		flusher, ok := w.(http.Flusher)
		if !ok {
			return
		}

		fmt.Fprintf(w, "data: {\"choices\": [{\"delta\": {\"content\": \"Hello\"}}]}\n\n")
		flusher.Flush()
		fmt.Fprintf(w, "data: {\"choices\": [{\"delta\": {\"content\": \" world\"}}]}\n\n")
		flusher.Flush()
		fmt.Fprintf(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	defer ts.Close()

	client := NewClient()
	var chunks []string

	err := client.StreamCompletion(context.Background(), StreamOptions{
		Provider: "openai",
		BaseURL:  ts.URL,
		Messages: []ChatMessage{
			{Role: "user", Content: "Hi"},
		},
	}, func(chunk string) error {
		chunks = append(chunks, chunk)
		return nil
	})

	if err != nil {
		t.Fatalf("StreamCompletion failed: %v", err)
	}

	combined := strings.Join(chunks, "")
	if combined != "Hello world" {
		t.Errorf("expected 'Hello world', got %q", combined)
	}
}
