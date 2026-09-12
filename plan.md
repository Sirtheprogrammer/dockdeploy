# Self-Hosted Docker Deployment Platform

## Context

The repo is greenfield: `frontend/` is an untouched Vite + React 19 + TS scaffold, `server/` is an empty Go module (`module docker-deplo`, go 1.26.5). Nothing to reuse — this plan is the architecture.

**What we're building.** A developer self-hosts this tool (as a Docker container). From its dashboard they register their own servers by SSH credentials; the tool connects, auto-discovers running containers, and gives them lifecycle control. They then create deployments from a git repo or a compose file, build and run them on a chosen server, and generate nginx vhosts on that server's system nginx to point domains at those services, with Let's Encrypt certificates.

**Why the shape it has.** The controller holds no agent on the target machines — everything happens over one SSH connection, so onboarding a server is "paste credentials" and nothing else. That single constraint drives most of the design below (socket tunnelling, remote command execution, sudo handling).

## Decisions locked

| Area | Decision |
|---|---|
| Backend | Go 1.26, `chi` router, `pgx/v5` |
| Database | PostgreSQL (compose stack: app + postgres) |
| Frontend | Tailwind v4 + shadcn/ui, React Router, TanStack Query |
| Transport to servers | SSH only, no remote agent |
| Build model | **Both** — remote build on target, or controller build + registry push; chosen per deployment |
| Nginx | **System nginx on the host**, managed over SSH |
| Git | PAT / deploy keys, behind a provider interface so a GitHub App can slot in later |
| Auth | Multi-user with roles + per-resource access control |
| Scope | Full product: foundation → containers → deployments → nginx → domains/SSL |

---

## Architecture

### Repo layout

```
server/
  cmd/dockdeploy/main.go        entrypoint: config, migrate, workers, http
  internal/
    config/      env parsing + validation (fail fast on missing secrets)
    db/          pgxpool, goose migrations (embedded), tx helper
    store/       one repository per aggregate; all SQL lives here
    api/         chi router, handlers, DTOs, middleware
    auth/        argon2id, sessions, RBAC policy + middleware
    secrets/     AES-256-GCM seal/open for credentials at rest
    sshx/        connection pool, exec, sftp, host-key TOFU
    dockerx/     Engine API client over the SSH tunnel
    gitx/        provider interface + git CLI driver
    build/       Builder interface: remote strategy, registry strategy
    deploy/      orchestrator, port allocator, compose runner
    nginxx/      vhost templates, validate/reload, certbot
    stream/      websocket hub, log fan-out + DB replay
    jobs/        Postgres-backed queue (SKIP LOCKED + LISTEN/NOTIFY)
  migrations/*.sql
frontend/src/    routes/, components/ui (shadcn), lib/api.ts, lib/ws.ts, hooks/
Dockerfile       multi-stage: node build → go build → single image
docker-compose.yml   app + postgres + volumes
```

First step: rename the module from `docker-deplo` to `github.com/sirtheprogrammer/docker-deployments/server` while it's free to do.

### The SSH → Docker connection (the core mechanism)

Do **not** shell out to the `docker` CLI for the API surface, and do not use `docker/cli/connhelper` (it requires an `ssh` binary and can't take a password or an in-memory key).

Instead, in `internal/sshx`, dial with `golang.org/x/crypto/ssh`, then open a channel straight to the remote socket:

```go
conn, _ := sshClient.Dial("unix", "/var/run/docker.sock")   // direct-streamlocal@openssh.com
```

Wrap that in an `http.Client` whose `DialContext` returns those channels, and hand it to
`client.NewClientWithOpts(client.WithHTTPClient(hc), client.WithHost("http://docker"), client.WithAPIVersionNegotiation())`.
The result is the full Engine API — list, inspect, logs, stats, exec — over one SSH connection, with no agent installed.

Three things this must get right:

- **Host keys.** Never `ssh.InsecureIgnoreHostKey`. On first connect, capture the fingerprint, return it to the UI unverified, have the user confirm it, store it on the server row, and use `ssh.FixedHostKey` on every connect afterwards.
- **Pooling.** One `*ssh.Client` per server, reference-counted, with keepalives and reconnect-on-failure. Opening a fresh SSH handshake per API call is unusably slow.
- **Socket permissions.** If the SSH user can't read `/var/run/docker.sock`, the error is opaque. Detect it and return an actionable message ("add `<user>` to the `docker` group").

Also support a `local` server type that talks to the controller's own socket, so the tool can manage itself.

Log streams from non-TTY containers are multiplexed with an 8-byte frame header — demux with `docker/pkg/stdcopy.StdCopy` before sending to the browser, or output is garbled.

### Sudo

System nginx and certbot need root, and this is the sharpest friction point in the product. Per server, store a sudo mode: `none` / `nopasswd` / `password` (password encrypted). Probe it at connection time with `sudo -n true` and record the result, so the UI can tell the user exactly what to configure before they try to add a domain. Recommend a NOPASSWD sudoers entry scoped to `nginx`, `systemctl reload nginx`, and `certbot` in the docs.

### Job queue

Deployments are minutes-long and must survive a page refresh. `deployment_runs` rows are the queue: workers claim with `SELECT ... FOR UPDATE SKIP LOCKED`, and `LISTEN/NOTIFY` wakes them so there's no polling latency. Every log line is appended to `deployment_logs` **and** published to the websocket hub — a client that connects late replays from the table, then attaches to the live stream. No Redis.

---

## Data model

```
users(id, email, password_hash, role, status, created_at)
      role: admin | member | viewer
invitations(id, email, role, token_hash, expires_at, accepted_at)
sessions(id, user_id, token_hash, expires_at, ip, user_agent)
api_tokens(id, user_id, name, token_hash, last_used_at)

servers(id, name, host, port, username, auth_method, secret_id,
        host_key_fingerprint, sudo_mode, docker_version, os, status, last_seen_at)
server_members(server_id, user_id, permission)

secrets(id, kind, nonce, ciphertext, created_at)        -- AES-256-GCM

git_credentials(id, provider, name, secret_id, username)
registries(id, name, url, username, secret_id)

projects(id, name, owner_id)
project_members(project_id, user_id, permission)

deployments(id, project_id, server_id, name, slug, source_type, source_ref,
            build_strategy, dockerfile_path, compose_path, registry_id,
            git_credential_id, workdir, host_port, status, current_run_id)
      source_type: git_dockerfile | git_compose | raw_compose | image
      build_strategy: remote | registry
deployment_env(deployment_id, key, secret_id, is_secret)
deployment_runs(id, deployment_id, trigger, commit_sha, image_ref,
                status, started_at, finished_at, error)
deployment_logs(run_id, seq, stream, line, ts)

domains(id, deployment_id, server_id, hostname, upstream_port, ssl_mode,
        websocket, config_rendered, cert_expires_at, status)
audit_log(id, user_id, action, resource_type, resource_id, meta, ip, ts)
```

Container state is **not** stored — it's read live from the Engine API and cached briefly in memory. The DB only holds what the tool owns.

---

## Milestones

Each milestone should end in a working, demonstrable state.

### M0 — Skeleton and self-hosting

- `config` (fail fast if `APP_ENCRYPTION_KEY` / `SESSION_SECRET` / `DATABASE_URL` missing), `db` with goose migrations embedded and run on boot, chi router, `/api/health`, `log/slog`.
- `internal/secrets`: AES-256-GCM seal/open, key from base64 env. Table-driven test including tamper-detection.
- Root `Dockerfile` (node stage → go stage → single small image) and `docker-compose.yml` (app + postgres + named volume). Go serves the built SPA from `embed.FS`, so the controller is one container.
- Frontend: Tailwind v4 via `@tailwindcss/vite` (`@import "tailwindcss"`, no config file), shadcn/ui init with `@/*` aliases in both `tsconfig.json` and `vite.config.ts`, React Router, TanStack Query, `lib/api.ts` fetch wrapper that surfaces API errors as typed rejections. Vite dev proxy `/api` → `:8080`.

**Done when** `docker compose up` serves the dashboard shell and `/api/health` is green.

### M1 — Auth and RBAC

- argon2id hashing, session cookies (HttpOnly, SameSite=Lax, Secure behind TLS), first-run setup route that creates the first `admin` and then disables itself.
- RBAC: `admin` full, `member` acts on resources they're granted, `viewer` read-only. Declare the required permission **per route in the router**, not inside handlers — with this many endpoints, an unguarded handler is the likely bug. Add a startup assertion that every non-public route has a policy attached.
- Invitations (email-less: admin generates a link), user management UI, API tokens for CI.
- `audit_log` middleware on every mutating request.

### M2 — Servers and container management

- `sshx` pool + host-key TOFU flow; "Add server" wizard: credentials → fingerprint confirmation → capability probe (docker version, compose v2 vs `docker-compose`, sudo mode, nginx present, certbot present, git present) → save.
- `dockerx` client + endpoints: list/inspect containers, images, volumes, networks, `docker system df`; start/stop/restart/kill/remove; container logs (follow, tail, since); `stats` stream; `exec` for an interactive terminal.
- `stream`: websocket hub. One connection per client, multiplexed by topic, so logs + stats + terminal don't each open a socket.
- UI: server list with health, server detail with Containers / Images / Volumes / Networks tabs, log viewer (virtualized — container logs get long), xterm.js terminal, stats sparklines.

**Done when** adding a real VPS lists its running containers and streams a live log.

### M3 — Deployments

`build.Builder` interface with two implementations, chosen by `deployments.build_strategy`:

- **`remote`** — over SSH: clone/fetch into `~/.dockdeploy/apps/<slug>`, then `docker build` or `docker compose build` on the target. No registry required.
- **`registry`** — on the controller: `git clone` (git CLI, installed in the image — handles submodules/LFS/shallow properly), `ImageBuild` against the controller's own daemon with a tar context, tag, push with `RegistryAuth`. Target then does `docker login` + `docker pull`.

Shared afterwards: a `Deployer` that takes the resulting image ref or compose file and applies it.

- Compose has no usable Go library — write the file over SFTP and shell out to `docker compose -f <path> -p <project> up -d`, using the variant detected in M2. This is what every comparable tool does.
- Env vars: encrypted at rest, written to a `0600` `.env` in the remote workdir, referenced by compose. Redact from all log output.
- Port allocator: for non-compose deployments, assign a free port from a configured range and publish to `127.0.0.1:<port>` so services aren't exposed publicly before nginx fronts them.
- Generic webhook URL per deployment (HMAC-signed) for push-to-deploy.
- `gitx.Provider` interface (`Clone`, `Fetch`, `ResolveRef`, `Auth`) with a single credential-based driver — the seam for a future GitHub App.
- UI: new-deployment wizard (source → server → build strategy → env → ports), deployment detail with live build logs and run history, redeploy and rollback-to-previous-image.

**Done when** a public GitHub repo with a Dockerfile deploys to a real server both ways and the container is reachable on its port.

### M4 — Nginx and domains

The risk the user accepted here is real — a bad config can take down unrelated sites on that box. The write path must therefore be:

1. Render vhost from template → upload to a temp path.
2. `nginx -t` against it. **Reject and stop on failure.**
3. Back up the existing file, move the new one into place.
4. `systemctl reload nginx`.
5. If reload fails, restore the backup, reload again, and report the failure.

Never write straight into `sites-enabled`. Detect Debian (`sites-available` + symlink) vs RHEL (`conf.d`) layout during the M2 probe. Store `config_rendered` in Postgres so the UI can show and diff exactly what is on disk.

Template covers: `proxy_pass` to `127.0.0.1:<upstream_port>`, standard proxy headers, optional `Upgrade`/`Connection` for websockets, `client_max_body_size`, and an ACME challenge location.

**SSL:** `certbot certonly --webroot -w /var/www/certbot -d <host>` — *not* `--nginx`, which would rewrite the vhost we own. Our template references the issued cert paths and adds the 80→443 redirect once the cert exists. Certbot's own timer handles renewal; add a `--deploy-hook` that reloads nginx. Surface `cert_expires_at` in the UI by reading the cert. If certbot isn't installed, say so plainly rather than failing mid-issuance.

### M5 — Hardening

Rate-limit auth, CSRF for cookie-auth mutations, request size limits, structured error envelope, pagination on list endpoints, log retention/pruning, graceful shutdown draining in-flight runs, and a README covering setup, the sudoers entry, and backup/restore.

---

## Cross-cutting security requirements

These apply from M0 and should not be deferred:

- Secrets are AES-256-GCM sealed with a key from env and **never** returned by any endpoint — not even redacted-with-a-reveal-button.
- Host keys are pinned after user confirmation; `InsecureIgnoreHostKey` must not appear anywhere.
- Env values, PATs, and registry passwords are scrubbed from build/deploy logs before they're persisted or streamed.
- Every mutating route is behind an RBAC policy and writes an audit entry.
- Deployed containers publish to loopback by default, not `0.0.0.0`.

---

## Verification

- **Unit:** `secrets` seal/open + tamper, nginx template rendering (golden files), port allocator, RBAC policy matrix, log scrubbing.
- **Store:** integration tests against a throwaway Postgres container.
- **SSH/Docker:** an `sshd` container with the docker socket mounted, as a fixture for `sshx`/`dockerx` — covers the tunnel and log demuxing without a VPS.
- **End-to-end, manual, against one real VPS** (the only way to prove M4): add server → confirm fingerprint → see containers → deploy a repo remotely → deploy a repo via registry → add a domain → issue a cert → hit the domain over HTTPS. Then deliberately deploy a broken nginx config and confirm existing sites stay up and the change rolls back.

## Open items

- Server-side metrics history (CPU/mem over time) is not in scope above; `stats` is live-only. Add later if wanted.
- Multi-replica controller is out of scope — the job queue supports it, but websocket fan-out would need Postgres NOTIFY-based routing between instances.
