package nginxx

import (
	"bytes"
	"fmt"
	"strings"
	"text/template"
)

// Config holds everything needed to render an nginx virtual host.
type Config struct {
	Hostname          string
	UpstreamPort      int
	WebSocket         bool
	HasSSL            bool
	CertPath          string
	KeyPath           string
	ClientMaxBodySize string
}

const vhostTemplate = `# Managed by dockdeploy for {{ .Hostname }}
# Generated: do not edit directly. Changes will be overwritten.

server {
    listen 80;
    listen [::]:80;
    server_name {{ .Hostname }};

    # ACME challenge location for Let's Encrypt / Certbot webroot verification
    location /.well-known/acme-challenge/ {
        root /var/www/certbot;
        try_files $uri =404;
    }
{{ if .HasSSL }}
    location / {
        return 301 https://$host$request_uri;
    }
}

server {
    listen 443 ssl;
    listen [::]:443 ssl;
    server_name {{ .Hostname }};

    ssl_certificate {{ .CertPath }};
    ssl_certificate_key {{ .KeyPath }};
    ssl_protocols TLSv1.2 TLSv1.3;
    ssl_prefer_server_ciphers on;
    ssl_ciphers HIGH:!aNULL:!MD5;

    client_max_body_size {{ .ClientMaxBodySize }};

    location /.well-known/acme-challenge/ {
        root /var/www/certbot;
        try_files $uri =404;
    }

    location / {
        proxy_pass http://127.0.0.1:{{ .UpstreamPort }};
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_set_header X-Forwarded-Host $host;
        proxy_set_header X-Forwarded-Port $server_port;
{{ if .WebSocket }}
        # WebSocket proxy support
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
{{ end }}
        proxy_read_timeout 300s;
        proxy_connect_timeout 60s;
        proxy_send_timeout 300s;
    }
}
{{ else }}
    client_max_body_size {{ .ClientMaxBodySize }};

    location / {
        proxy_pass http://127.0.0.1:{{ .UpstreamPort }};
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_set_header X-Forwarded-Host $host;
        proxy_set_header X-Forwarded-Port $server_port;
{{ if .WebSocket }}
        # WebSocket proxy support
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
{{ end }}
        proxy_read_timeout 300s;
        proxy_connect_timeout 60s;
        proxy_send_timeout 300s;
    }
}
{{ end }}
`

var tmpl = template.Must(template.New("vhost").Parse(vhostTemplate))

// Render produces a validated nginx virtual host configuration string.
func Render(c Config) (string, error) {
	c.Hostname = strings.ToLower(strings.TrimSpace(c.Hostname))
	if c.Hostname == "" {
		return "", fmt.Errorf("nginxx: hostname is required")
	}
	if c.UpstreamPort < 1 || c.UpstreamPort > 65535 {
		return "", fmt.Errorf("nginxx: invalid upstream port %d", c.UpstreamPort)
	}
	if c.ClientMaxBodySize == "" {
		c.ClientMaxBodySize = "64M"
	}
	if c.HasSSL {
		if c.CertPath == "" {
			c.CertPath = fmt.Sprintf("/etc/letsencrypt/live/%s/fullchain.pem", c.Hostname)
		}
		if c.KeyPath == "" {
			c.KeyPath = fmt.Sprintf("/etc/letsencrypt/live/%s/privkey.pem", c.Hostname)
		}
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, c); err != nil {
		return "", fmt.Errorf("nginxx: render template: %w", err)
	}
	return buf.String(), nil
}
