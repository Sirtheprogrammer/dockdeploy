# dockdeploy

A self-hosted control plane for deploying and managing Docker containers across
your own servers.

Connect a machine with its SSH credentials and dockdeploy discovers what is
already running on it, deploys new applications from a git repository or a
compose file, and configures nginx so your domains point at them.

**Nothing is installed on the servers you manage.** Everything happens over a
single SSH connection to the machine's existing Docker daemon.

## Status

Under active development. Working today:

- [x] **M0** — control plane skeleton, Postgres schema, encrypted credential
      store, single-container self-hosting, dashboard shell
- [x] **M1** — authentication, roles, invitations, API tokens, audit log
- [x] **M2** — server connections, container discovery, lifecycle, live logs
- [~] **M3** — deployments from git, compose, or a plain image, with live build
      logs and run history in the dashboard. Build-here-and-push-to-a-registry
      is not written yet
- [ ] **M4** — nginx virtual hosts, domains, TLS certificates
- [ ] **M5** — hardening

## Running it

Requires Docker and Docker Compose.

```sh
cp .env.example .env
openssl rand -base64 32   # -> APP_ENCRYPTION_KEY
openssl rand -base64 32   # -> SESSION_SECRET
openssl rand -base64 24   # -> POSTGRES_PASSWORD

docker compose up -d --build
```

The dashboard is on <http://localhost:8080>. The schema migrates itself on
startup.

`APP_ENCRYPTION_KEY` seals every credential the platform stores. **Back it up.**
If you lose it, every saved SSH key, sudo password, registry login and
deployment secret becomes permanently unreadable and has to be re-entered.

By default the container binds to `127.0.0.1` only. Put a reverse proxy with
TLS in front of it before exposing it to the internet.

## Local development

Two processes, so both sides hot-reload:

```sh
# terminal 1 - Postgres only
docker compose up -d postgres

# terminal 2 - API on :8080
cd server
DATABASE_URL='postgres://dockdeploy:<password>@localhost:5432/dockdeploy?sslmode=disable' \
APP_ENCRYPTION_KEY='<base64>' SESSION_SECRET='<base64>' APP_ENV=development \
go run ./cmd/dockdeploy

# terminal 3 - dashboard on :5173, proxying /api to :8080
cd frontend
npm install
npm run dev
```

Expose Postgres locally by adding a `ports: ["127.0.0.1:5432:5432"]` mapping to
the `postgres` service, or run the API in the compose network instead.

In production the frontend is compiled into the Go binary, so the release image
is a single container with no sidecar web server.

### Checks

```sh
cd server   && go build ./... && go vet ./... && go test ./...
cd frontend && npm run lint && npm run build
```

Store tests need a Postgres to talk to. Each run creates a throwaway database
and drops it afterwards, so it never touches development data:

```sh
cd server
DOCKDEPLOY_TEST_DATABASE_URL='postgres://dockdeploy:<password>@localhost:5432/dockdeploy?sslmode=disable'   go test ./internal/store/ -v
```

The SSH-to-Docker tunnel has integration tests that need a real sshd. They skip
without one:

```sh
cd server
sh internal/dockerx/testdata/fixture.sh up    # prints the env to export
DOCKDEPLOY_SSH_HOST=127.0.0.1 DOCKDEPLOY_SSH_PORT=2222 DOCKDEPLOY_SSH_USER=root DOCKDEPLOY_SSH_KEY=internal/dockerx/testdata/id_ed25519   go test ./internal/dockerx/ -run Tunnel -v
sh internal/dockerx/testdata/fixture.sh down
```

## How it works

```
browser ──► dockdeploy (Go + embedded SPA) ──► Postgres
                     │
                     └── SSH ──► your server ──► /var/run/docker.sock
```

The controller opens one pooled SSH connection per server and tunnels straight
to the remote Docker socket, so it speaks the full Docker Engine API — list,
inspect, logs, stats, exec — without an agent on the other end.

Host keys are pinned on first connection and confirmed by you before anything
else happens; unverified keys are never trusted silently.

## Layout

```
server/                Go control plane
  cmd/dockdeploy/      entrypoint
  internal/config/     environment parsing, fails fast on bad input
  internal/secrets/    AES-256-GCM sealing for stored credentials
  internal/db/         pgx pool and goose migrations
  internal/api/        chi router, error envelope, handlers
  internal/web/        serves the embedded dashboard
  migrations/          SQL schema history
frontend/              React 19 + Vite + Tailwind v4 dashboard
```

## Requirements for a managed server

- SSH access, key or password.
- A running Docker daemon whose socket the SSH user can read
  (`sudo usermod -aG docker <user>`, then reconnect).
- An `ssh` client (`openssh-client`) if you deploy from a `git@host:path`
  repository. A machine running sshd does not necessarily have the client.
- `AllowTcpForwarding yes` in `/etc/ssh/sshd_config`. Despite the name it also
  gates the Unix-socket forwarding used to reach the Docker socket. **Edit the
  existing line rather than appending one** — OpenSSH honours the first
  occurrence of a keyword, so an override at the end of the file is ignored.
- For domains (M4): nginx and certbot installed, plus sudo rights to reload
  them. A NOPASSWD sudoers rule scoped to `nginx`, `systemctl reload nginx` and
  `certbot` is the recommended setup.

dockdeploy reports all of this when you add a server, so you do not have to
check in advance.
