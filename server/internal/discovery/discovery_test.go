package discovery

import (
	"regexp"
	"strings"
	"testing"

	"github.com/sirtheprogrammer/docker-deployments/server/internal/gitx"
	"github.com/sirtheprogrammer/docker-deployments/server/internal/store"
)

func TestParseGitCredentialsOutput(t *testing.T) {
	rawOutput := `
===DOCKDEPLOY_CRED:GIT_CREDENTIALS===
https://Sirtheprogrammer:ghp_1234567890abcdef@github.com
https://gitlab-ci-token:glpat-9876543210fedcba@gitlab.com/repo.git
===DOCKDEPLOY_CRED_END===
===DOCKDEPLOY_CRED:NETRC===
machine github.com login bot-user password secret-token
machine api.bitbucket.org
login bbuser
password bbpass
===DOCKDEPLOY_CRED_END===
===DOCKDEPLOY_CRED:SSH_KEY:id_ed25519===
-----BEGIN OPENSSH PRIVATE KEY-----
b3BlbnNzaC1rZXktdjEAAAAABG5vbmUAAAAEbm9uZQAAAAAAAAABAAAAMwAAAAtzc2gtZW
-----END OPENSSH PRIVATE KEY-----
===DOCKDEPLOY_CRED_END===
`

	creds := parseGitCredentialsOutput(rawOutput, "Prod-1")
	if len(creds) != 5 {
		t.Fatalf("expected 5 credentials, got %d: %+v", len(creds), creds)
	}

	// 1. GitHub token
	c1 := creds[0]
	if c1.Kind != gitx.KindToken || c1.Username != "Sirtheprogrammer" || c1.Secret != "ghp_1234567890abcdef" {
		t.Errorf("unexpected cred 1: %+v", c1)
	}
	if !strings.Contains(c1.Name, "Prod-1") || !strings.Contains(c1.Name, "github.com") {
		t.Errorf("unexpected cred name 1: %s", c1.Name)
	}

	// 2. GitLab token
	c2 := creds[1]
	if c2.Kind != gitx.KindToken || c2.Secret != "glpat-9876543210fedcba" {
		t.Errorf("unexpected cred 2: %+v", c2)
	}

	// 3. Netrc github
	c3 := creds[2]
	if c3.Kind != gitx.KindToken || c3.Username != "bot-user" || c3.Secret != "secret-token" {
		t.Errorf("unexpected cred 3: %+v", c3)
	}

	// 4. Netrc bitbucket
	c4 := creds[3]
	if c4.Kind != gitx.KindToken || c4.Username != "bbuser" || c4.Secret != "bbpass" {
		t.Errorf("unexpected cred 4: %+v", c4)
	}

	// 5. SSH Key
	c5 := creds[4]
	if c5.Kind != gitx.KindSSHKey || c5.Username != "git" || !strings.Contains(c5.Secret, "OPENSSH PRIVATE KEY") {
		t.Errorf("unexpected cred 5: %+v", c5)
	}
	if !strings.Contains(c5.Name, "id_ed25519") {
		t.Errorf("unexpected cred name 5: %s", c5.Name)
	}
}

func TestParseNginxConfigs(t *testing.T) {
	configText := `
===DOCKDEPLOY_NGINX_FILE:/etc/nginx/sites-enabled/app.conf===
server {
    listen 80;
    server_name myapp.example.com;
    return 301 https://$host$request_uri;
}

server {
    listen 443 ssl http2;
    server_name myapp.example.com;

    ssl_certificate /etc/letsencrypt/live/myapp.example.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/myapp.example.com/privkey.pem;

    location / {
        proxy_pass http://127.0.0.1:8081;
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
    }
}
===DOCKDEPLOY_NGINX_END===
===DOCKDEPLOY_NGINX_FILE:/etc/nginx/conf.d/api.conf===
upstream api_cluster {
    server 127.0.0.1:3000;
}

server {
    listen 80;
    server_name api.example.com test-api.example.com;

    location / {
        proxy_pass http://api_cluster;
    }
}
===DOCKDEPLOY_NGINX_END===
`

	vhosts := parseNginxConfigs(configText)
	if len(vhosts) != 3 {
		t.Fatalf("expected 3 vhosts (myapp, api, test-api), got %d: %+v", len(vhosts), vhosts)
	}

	vhMap := make(map[string]parsedVHost)
	for _, vh := range vhosts {
		vhMap[vh.Hostname] = vh
	}

	// myapp.example.com
	appVh, ok := vhMap["myapp.example.com"]
	if !ok {
		t.Fatalf("myapp.example.com not found")
	}
	if appVh.UpstreamPort != 8081 {
		t.Errorf("expected port 8081, got %d", appVh.UpstreamPort)
	}
	if appVh.SSLMode != store.DomainSSLLetsEncrypt {
		t.Errorf("expected SSLMode letsencrypt, got %s", appVh.SSLMode)
	}
	if !appVh.WebSocket {
		t.Errorf("expected WebSocket true, got false")
	}

	// api.example.com
	apiVh, ok := vhMap["api.example.com"]
	if !ok {
		t.Fatalf("api.example.com not found")
	}
	if apiVh.UpstreamPort != 3000 {
		t.Errorf("expected port 3000 (from upstream block), got %d", apiVh.UpstreamPort)
	}
	if apiVh.SSLMode != store.DomainSSLNone {
		t.Errorf("expected SSLMode none, got %s", apiVh.SSLMode)
	}
}

func TestExtractServerNamesFiltersInvalid(t *testing.T) {
	block := `
server {
    listen 80 default_server;
    server_name _ localhost 127.0.0.1 valid.example.com ANOTHER-one.org;
}
`
	names := extractServerNames(block)
	if len(names) != 2 {
		t.Fatalf("expected 2 valid domain names, got %d: %+v", len(names), names)
	}
	if names[0] != "valid.example.com" {
		t.Errorf("expected valid.example.com, got %s", names[0])
	}
	if names[1] != "another-one.org" {
		t.Errorf("expected another-one.org, got %s", names[1])
	}
}

func TestSlugify(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"My Super App", "my-super-app"},
		{"app_with_underscores-and-DASHES", "app-with-underscores-and-dashes"},
		{"---weird---name---", "weird-name"},
		{"", "app"},
		{"a", "a-app"},
		{"db", "db"},
		{"deploy", "deploy"},
	}
	slugRegex := regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,48}[a-z0-9]$`)
	for _, tt := range tests {
		got := slugify(tt.in)
		if got != tt.want {
			t.Errorf("slugify(%q) = %q, want %q", tt.in, got, tt.want)
		}
		if !slugRegex.MatchString(got) {
			t.Errorf("slugify(%q) = %q does not match slug regex", tt.in, got)
		}
	}
}
