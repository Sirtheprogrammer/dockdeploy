# dockdeploy

A self-hosted control plane for deploying and managing Docker containers across your own servers.

Connect a machine with its SSH credentials and dockdeploy discovers what is already running on it, deploys new applications from a git repository or a compose file, and configures nginx so your domains point at them with automated Let's Encrypt TLS certificates.

**Nothing is installed on the servers you manage.** Everything happens over a single SSH connection to the machine's existing Docker daemon and system utilities.

---

## Status

All planned milestones are complete and verified:

- [x] **M0 — Foundation & Self-Hosting**: Go control plane, PostgreSQL database with embedded goose migrations, AES-256-GCM encrypted credential store, single-container self-hosting, and React 19 + Vite + Tailwind v4 dashboard shell.
- [x] **M1 — Auth & RBAC**: Argon2id password hashing, session cookies, RBAC policy enforcement at the router level, invite links, scoped API tokens, and mutating request audit logging.
- [x] **M2 — Servers & Container Management**: Agentless SSH-to-Docker socket tunneling, host-key TOFU confirmation, capability probes (Docker, Compose, sudo, Nginx, certbot, git), container lifecycle controls, live multiplexed logs, stats streaming, and interactive web terminal.
- [x] **M3 — Deployments & Engine**: Deployments from Git repositories (Dockerfile), Git Compose, raw compose files, or pre-existing images. Live build log streaming, dual build strategies (target server build vs controller build + registry push), push-to-deploy webhooks (GitHub HMAC, GitLab, Bearer tokens), and instant image rollback.
- [x] **M4 — Nginx & Custom Domains**: System Nginx virtual host management over SSH, safe 5-step non-destructive pipeline with syntax testing and automatic rollback on failure, automated Let's Encrypt TLS via certbot webroot, certificate expiration tracking, and full domains UI.
- [x] **M5 — Hardening & Production Operations**: Auth rate limiting, CSRF protection on cookie-authenticated mutations, request body size guards, periodic automated housekeeping (pruning sessions, runs, and audit logs), and comprehensive operations documentation.

---

## Running It

dockdeploy supports two running modes:
1. **Embedded SQLite (Zero Dependencies / Default)**: If `DATABASE_URL` is omitted or empty, dockdeploy automatically runs using a pure-Go embedded SQLite database (default: `dockdeploy.db` or `SQLITE_PATH`) with zero CGO dependencies.
2. **PostgreSQL**: Set `DATABASE_URL=postgres://...` to use an external or containerized PostgreSQL instance.

### Running with Docker Compose (SQLite Default)

```sh
cp .env.example .env
openssl rand -base64 32   # -> APP_ENCRYPTION_KEY
openssl rand -base64 32   # -> SESSION_SECRET

docker compose up -d --build
```

dockdeploy automatically boots with embedded SQLite persisted to the `dockdeploy-data` volume. No separate database container is required. (To use PostgreSQL instead, set `DATABASE_URL=postgres://...` in `.env`).

### Running Standalone with SQLite

```sh
export APP_ENCRYPTION_KEY=$(openssl rand -base64 32)
export SESSION_SECRET=$(openssl rand -base64 32)
# DATABASE_URL is not set: dockdeploy uses embedded SQLite automatically!
./server/cmd/dockdeploy/dockdeploy
```

The dashboard is served on <http://localhost:8080>. The database schema automatically migrates itself on startup (both SQLite and PostgreSQL).

> [!IMPORTANT]
> `APP_ENCRYPTION_KEY` seals every credential the platform stores (SSH private keys, passwords, registry credentials, env secrets, and webhook secrets). **Back it up immediately.** If lost, stored secrets cannot be decrypted and must be re-entered.

By default, the container binds to `127.0.0.1:8080`. Put a reverse proxy with TLS in front of it before exposing it to the internet.

---

## Architecture & How It Works

```
browser ──► dockdeploy (Go + embedded SPA) ──► SQLite (default) or PostgreSQL
                     │
                     └── SSH ──► your server ──► /var/run/docker.sock
                                             └── /etc/nginx (vhosts)
                                             └── certbot (TLS)
```

1. **Agentless Tunneling**: The controller opens one pooled SSH connection per server and forwards a stream straight to `/var/run/docker.sock`. It speaks the official Docker Engine API directly without requiring third-party agents or exposed TCP ports.
2. **Strict Host Key Pinning**: Host keys are captured on the first connection (TOFU). The user explicitly confirms the fingerprint before dockdeploy executes any commands. Afterwards, all connections enforce `ssh.FixedHostKey`.
3. **Database-Backed Job Queue**: Deployments are queued in SQLite (with thread-safe event broadcaster) or PostgreSQL (using `SKIP LOCKED` and `LISTEN/NOTIFY`). Runs persist across browser refreshes and server reboots.
4. **Isolated Virtual Hosts**: Each managed domain receives an independent Nginx server configuration block in `/etc/nginx/sites-available` (Debian/Ubuntu) or `/etc/nginx/conf.d` (RHEL/CentOS).

---

## Managed Server Requirements

When adding a server, dockdeploy probes and reports capabilities automatically. The target server needs:

1. **SSH Access**: Key-based (recommended) or password authentication.
2. **Docker Engine**: The SSH user must have permission to access the Docker daemon:
   ```sh
   sudo usermod -aG docker $USER
   ```
3. **TCP Forwarding**: `AllowTcpForwarding yes` in `/etc/ssh/sshd_config`. (OpenSSH uses this setting to gate Unix socket forwarding).
4. **Git Client**: `git` installed on the target machine if using the remote build strategy with Git sources.
5. **Nginx & Sudo (for Domains / M4)**: Nginx and Certbot installed, with sudo privileges to reload Nginx and run Certbot.

### Sudoers Configuration (`/etc/sudoers.d/dockdeploy`)

To manage Nginx and Let's Encrypt certificates safely without granting full root access, create `/etc/sudoers.d/dockdeploy` on the managed server:

```sudoers
# /etc/sudoers.d/dockdeploy
# Replace 'deploy' with the SSH username dockdeploy connects as
deploy ALL=(ALL) NOPASSWD: /usr/sbin/nginx, /bin/systemctl reload nginx, /bin/systemctl restart nginx, /usr/bin/certbot
```

Set permissions to mode `0440`:
```sh
sudo chmod 0440 /etc/sudoers.d/dockdeploy
```

dockdeploy checks for sudo capability on connection and tests `sudo -n true`. If passwordless sudo is configured, it operates smoothly without prompting for passwords.

---

## Features

### 1. Deployments & Build Strategies

dockdeploy supports four deployment sources:
- **Git repository with Dockerfile**: Clones the repo and builds the container image.
- **Git repository with Docker Compose**: Clones the repo and starts services via `docker compose`.
- **Raw Compose**: Paste a `docker-compose.yml` directly in the UI.
- **Existing Image**: Pulls and runs an image from Docker Hub or a private registry.

#### Build Strategies
- **Remote Build (Target Server)**: Clones code directly onto the target server into `~/.dockdeploy/apps/<slug>` and invokes the remote Docker daemon. Ideal for lightweight repositories or fast target servers.
- **Controller Build + Registry Push**: Clones repository on the dockdeploy controller, builds the image, pushes it to your configured container registry (Docker Hub, GitHub Packages, AWS ECR, self-hosted), and pulls it onto the target server. Keeps build overhead off production servers.

### 2. Push to Deploy (Webhooks)

Every deployment has a unique, HMAC-secured webhook URL:

- **GitHub**: Paste the webhook URL into **Settings &rarr; Webhooks**. Set content type to `application/json` and enter the secret. dockdeploy automatically validates `X-Hub-Signature-256`.
- **GitLab**: Paste the URL and secret token (`X-Gitlab-Token`).
- **Generic CI / cURL**:
  ```sh
  curl -X POST -H "Authorization: Bearer <WEBHOOK_SECRET>" \
    https://dockdeploy.example.com/api/deployments/<DEPLOYMENT_ID>/webhook
  ```
  Or via query parameter:
  ```sh
  curl -X POST "https://dockdeploy.example.com/api/deployments/<DEPLOYMENT_ID>/webhook?token=<WEBHOOK_SECRET>"
  ```

### 3. Instant Image Rollback

Every successful deployment run records its exact immutable image digest (`image_ref`) and commit SHA. In the deployment history table, click **Rollback** on any previous run:
- dockdeploy immediately deploys the target historical image.
- Skips rebuilding and cloning, providing instantaneous recovery in production.

### 4. Nginx Virtual Hosts & Safe Deployment Pipeline

Nginx configuration changes on managed servers use a non-destructive 5-step safety pipeline:

1. **Render & Upload**: Configuration is rendered from tested templates and uploaded to a temporary file (`/tmp/dockdeploy-<domain>.conf`).
2. **Syntax Verification**: A validation wrapper tests the configuration with `nginx -t`. If syntax fails, the process halts without affecting active traffic.
3. **Atomic Backup**: The existing virtual host is backed up to a `.bak` file.
4. **Atomic Swap & Reload**: The new configuration is moved into `/etc/nginx/sites-available/dockdeploy-<domain>.conf` (symlinked into `/etc/nginx/sites-enabled`) and Nginx reloads via `systemctl reload nginx`.
5. **Automatic Rollback**: If Nginx reload fails, the backup is restored and reloaded immediately.

### 5. Automated Let's Encrypt TLS

- Generates certificates via `certbot certonly --webroot -w /var/www/certbot -d <domain>`.
- Preserves the virtual host structure (never uses `--nginx` plugin which can corrupt custom blocks).
- Automatically upgrades configuration to HTTPS with HTTP-to-HTTPS redirect once the certificate is verified.
- Monitors expiration and tracks days remaining directly in the UI.

### 6. Hardening & Security (M5)

- **Encrypted Credentials at Rest**: All private keys, passwords, registry credentials, environment secrets, and webhook tokens are encrypted using AES-256-GCM. Secrets are write-only and never exposed in API responses.
- **Log Scrubbing**: Secrets, tokens, and passwords configured in deployment environments are automatically sanitized from live and recorded build/run logs.
- **Sliding-Window Rate Limiting**: Sensitive auth endpoints (`/api/auth/login`, `/api/auth/setup`) are rate-limited per IP address to defend against brute force attacks.
- **CSRF Protection**: State-changing requests authenticated via session cookies verify `Origin` and `Referer` headers against allowed application hosts.
- **Request Body Limits**: JSON APIs strictly enforce 1MB body limits (`10MB` for raw compose files) via `http.MaxBytesReader`.
- **Automated Housekeeping**: A periodic background worker prunes expired sessions, cleans historical run records beyond retention limits, and purges audit logs older than 90 days.

---

## Local Development

```sh
# Terminal 1: PostgreSQL
docker compose up -d postgres

# Terminal 2: API Server (:8080)
cd server
DATABASE_URL='postgres://dockdeploy:dockdeploy@localhost:5432/dockdeploy?sslmode=disable' \
APP_ENCRYPTION_KEY='$(openssl rand -base64 32)' \
SESSION_SECRET='$(openssl rand -base64 32)' \
APP_ENV=development \
go run ./cmd/dockdeploy

# Terminal 3: Frontend Dashboard (:5173, proxies /api to :8080)
cd frontend
npm install
npm run dev
```

### Verification & Test Suite

Run all tests and linters across the project:

```sh
# Backend tests
cd server
go test -count=1 ./...
go build ./cmd/dockdeploy

# Frontend tests
cd frontend
npm run lint
npm run build
```

---

## Backup & Recovery

A complete backup of dockdeploy requires two components:

### 1. The Encryption Key
Save your `APP_ENCRYPTION_KEY` securely (e.g., in a password manager or secrets vault).

### 2. The Database

#### For SQLite:
Copy the SQLite database file:

```sh
# Local standalone:
cp dockdeploy.db dockdeploy_backup_$(date +%F).db

# Docker compose container:
docker compose cp app:/home/app/data/dockdeploy.db dockdeploy_backup_$(date +%F).db
```

#### For PostgreSQL:
Generate a standard PostgreSQL dump:

```sh
pg_dump -U dockdeploy -h <host> dockdeploy > dockdeploy_backup_$(date +%F).sql
```

### Restore

To restore dockdeploy onto a new machine:

1. Restore `.env` with the **same** `APP_ENCRYPTION_KEY` and `SESSION_SECRET`.
2. For SQLite: copy your backed-up database file to `dockdeploy.db` (or copy into the container volume via `docker compose cp dockdeploy_backup_*.db app:/home/app/data/dockdeploy.db`).
3. For PostgreSQL: restore your pg_dump into your PostgreSQL instance before starting dockdeploy.

---

## License

MIT License.
