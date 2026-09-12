package nginxx

import (
	"strings"
	"testing"
)

func TestRenderHTTPOnly(t *testing.T) {
	cfg := Config{
		Hostname:     "app.example.com",
		UpstreamPort: 8080,
		WebSocket:    false,
		HasSSL:       false,
	}

	rendered, err := Render(cfg)
	if err != nil {
		t.Fatalf("unexpected render error: %v", err)
	}

	if !strings.Contains(rendered, "server_name app.example.com;") {
		t.Errorf("expected server_name, got:\n%s", rendered)
	}
	if !strings.Contains(rendered, "proxy_pass http://127.0.0.1:8080;") {
		t.Errorf("expected proxy_pass, got:\n%s", rendered)
	}
	if strings.Contains(rendered, "listen 443") {
		t.Errorf("expected no port 443 in HTTP-only mode, got:\n%s", rendered)
	}
	if strings.Contains(rendered, "proxy_set_header Upgrade") {
		t.Errorf("expected no websocket headers when disabled, got:\n%s", rendered)
	}
}

func TestRenderSSLAndWebSocket(t *testing.T) {
	cfg := Config{
		Hostname:     "api.example.com",
		UpstreamPort: 3000,
		WebSocket:    true,
		HasSSL:       true,
	}

	rendered, err := Render(cfg)
	if err != nil {
		t.Fatalf("unexpected render error: %v", err)
	}

	if !strings.Contains(rendered, "return 301 https://$host$request_uri;") {
		t.Errorf("expected 301 redirect to HTTPS, got:\n%s", rendered)
	}
	if !strings.Contains(rendered, "listen 443 ssl;") {
		t.Errorf("expected 443 ssl listener, got:\n%s", rendered)
	}
	if !strings.Contains(rendered, "/etc/letsencrypt/live/api.example.com/fullchain.pem") {
		t.Errorf("expected default fullchain.pem cert path, got:\n%s", rendered)
	}
	if !strings.Contains(rendered, "proxy_set_header Upgrade $http_upgrade;") {
		t.Errorf("expected websocket upgrade header, got:\n%s", rendered)
	}
	if !strings.Contains(rendered, "proxy_set_header Connection \"upgrade\";") {
		t.Errorf("expected websocket connection header, got:\n%s", rendered)
	}
}

func TestRenderValidation(t *testing.T) {
	if _, err := Render(Config{Hostname: "", UpstreamPort: 80}); err == nil {
		t.Errorf("expected error for empty hostname")
	}
	if _, err := Render(Config{Hostname: "test.com", UpstreamPort: 0}); err == nil {
		t.Errorf("expected error for invalid port 0")
	}
	if _, err := Render(Config{Hostname: "test.com", UpstreamPort: 70000}); err == nil {
		t.Errorf("expected error for invalid port 70000")
	}
}
