-- +goose Up

CREATE TABLE secrets (
    id         TEXT PRIMARY KEY,
    kind       TEXT NOT NULL,
    nonce      BLOB NOT NULL,
    ciphertext BLOB NOT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT secrets_kind_not_empty CHECK (kind <> '')
);

CREATE TABLE users (
    id            TEXT PRIMARY KEY,
    email         TEXT NOT NULL UNIQUE,
    name          TEXT NOT NULL,
    password_hash TEXT NOT NULL,
    role          TEXT NOT NULL,
    status        TEXT NOT NULL DEFAULT 'active',
    last_login_at DATETIME,
    created_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT users_email_lowercase CHECK (email = lower(email)),
    CONSTRAINT users_email_shape CHECK (email LIKE '%_@_%'),
    CONSTRAINT users_name_not_empty CHECK (name <> ''),
    CONSTRAINT users_role_valid CHECK (role IN ('admin', 'member', 'viewer')),
    CONSTRAINT users_status_valid CHECK (status IN ('active', 'suspended'))
);

CREATE TABLE sessions (
    id           TEXT PRIMARY KEY,
    user_id      TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash   BLOB NOT NULL UNIQUE,
    expires_at   DATETIME NOT NULL,
    last_used_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    ip           TEXT,
    user_agent   TEXT,
    created_at   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX sessions_user_id_idx ON sessions (user_id);
CREATE INDEX sessions_expires_at_idx ON sessions (expires_at);

CREATE TABLE invitations (
    id               TEXT PRIMARY KEY,
    email            TEXT NOT NULL,
    name             TEXT NOT NULL DEFAULT '',
    role             TEXT NOT NULL,
    token_hash       BLOB NOT NULL UNIQUE,
    invited_by       TEXT REFERENCES users(id) ON DELETE SET NULL,
    expires_at       DATETIME NOT NULL,
    accepted_at      DATETIME,
    accepted_user_id TEXT REFERENCES users(id) ON DELETE SET NULL,
    created_at       DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT invitations_email_lowercase CHECK (email = lower(email)),
    CONSTRAINT invitations_role_valid CHECK (role IN ('admin', 'member', 'viewer'))
);

CREATE INDEX invitations_email_idx ON invitations (email);

CREATE TABLE api_tokens (
    id           TEXT PRIMARY KEY,
    user_id      TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name         TEXT NOT NULL,
    token_hash   BLOB NOT NULL UNIQUE,
    prefix       TEXT NOT NULL,
    last_used_at DATETIME,
    expires_at   DATETIME,
    created_at   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT api_tokens_name_not_empty CHECK (name <> '')
);

CREATE INDEX api_tokens_user_id_idx ON api_tokens (user_id);

CREATE TABLE audit_log (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id       TEXT REFERENCES users(id) ON DELETE SET NULL,
    actor_email   TEXT NOT NULL DEFAULT '',
    action        TEXT NOT NULL,
    resource_type TEXT NOT NULL DEFAULT '',
    resource_id   TEXT NOT NULL DEFAULT '',
    status        INTEGER NOT NULL,
    ip            TEXT NOT NULL DEFAULT '',
    meta          TEXT NOT NULL DEFAULT '{}',
    created_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX audit_log_created_at_idx ON audit_log (created_at DESC);
CREATE INDEX audit_log_user_idx ON audit_log (user_id, created_at DESC);
CREATE INDEX audit_log_resource_idx ON audit_log (resource_type, resource_id, created_at DESC);

CREATE TABLE servers (
    id                      TEXT PRIMARY KEY,
    name                    TEXT NOT NULL UNIQUE,
    host                    TEXT NOT NULL,
    port                    INTEGER NOT NULL DEFAULT 22,
    username                TEXT NOT NULL,
    auth_method             TEXT NOT NULL,
    secret_id               TEXT REFERENCES secrets(id) ON DELETE RESTRICT,
    passphrase_secret_id    TEXT REFERENCES secrets(id) ON DELETE RESTRICT,
    sudo_password_secret_id TEXT REFERENCES secrets(id) ON DELETE RESTRICT,
    host_key_fingerprint    TEXT NOT NULL,
    docker_socket           TEXT NOT NULL DEFAULT '/var/run/docker.sock',
    status                  TEXT NOT NULL DEFAULT 'unknown',
    status_message          TEXT NOT NULL DEFAULT '',
    capabilities            TEXT NOT NULL DEFAULT '{}',
    last_seen_at            DATETIME,
    created_by              TEXT REFERENCES users(id) ON DELETE SET NULL,
    created_at              DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at              DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT servers_name_not_empty CHECK (name <> ''),
    CONSTRAINT servers_host_not_empty CHECK (host <> ''),
    CONSTRAINT servers_port_range CHECK (port BETWEEN 0 AND 65535),
    CONSTRAINT servers_auth_method_valid CHECK (auth_method IN ('password', 'key', 'local')),
    CONSTRAINT servers_fingerprint_present CHECK (host_key_fingerprint <> ''),
    CONSTRAINT servers_status_valid CHECK (status IN ('unknown', 'online', 'offline', 'unauthorized'))
);

CREATE INDEX servers_status_idx ON servers (status);

CREATE TABLE server_members (
    server_id  TEXT NOT NULL REFERENCES servers(id) ON DELETE CASCADE,
    user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    permission TEXT NOT NULL DEFAULT 'operate',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (server_id, user_id),
    CONSTRAINT server_members_permission_valid CHECK (permission IN ('read', 'operate'))
);

CREATE INDEX server_members_user_idx ON server_members (user_id);

CREATE TABLE registries (
    id         TEXT PRIMARY KEY,
    name       TEXT NOT NULL UNIQUE,
    url        TEXT NOT NULL,
    username   TEXT NOT NULL,
    secret_id  TEXT REFERENCES secrets(id) ON DELETE RESTRICT,
    created_by TEXT REFERENCES users(id) ON DELETE SET NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT registries_name_not_empty CHECK (name <> ''),
    CONSTRAINT registries_url_not_empty CHECK (url <> '')
);

CREATE TABLE git_credentials (
    id         TEXT PRIMARY KEY,
    name       TEXT NOT NULL UNIQUE,
    provider   TEXT NOT NULL DEFAULT 'generic',
    kind       TEXT NOT NULL,
    username   TEXT NOT NULL DEFAULT '',
    secret_id  TEXT REFERENCES secrets(id) ON DELETE RESTRICT,
    created_by TEXT REFERENCES users(id) ON DELETE SET NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT git_credentials_name_not_empty CHECK (name <> ''),
    CONSTRAINT git_credentials_kind_valid CHECK (kind IN ('token', 'ssh_key'))
);

CREATE TABLE deployments (
    id                TEXT PRIMARY KEY,
    server_id         TEXT NOT NULL REFERENCES servers(id) ON DELETE CASCADE,
    name              TEXT NOT NULL,
    slug              TEXT NOT NULL,
    source_type       TEXT NOT NULL,
    repo_url          TEXT NOT NULL DEFAULT '',
    git_ref           TEXT NOT NULL DEFAULT '',
    git_credential_id TEXT REFERENCES git_credentials(id) ON DELETE SET NULL,
    dockerfile_path   TEXT NOT NULL DEFAULT 'Dockerfile',
    build_context     TEXT NOT NULL DEFAULT '.',
    compose_path      TEXT NOT NULL DEFAULT 'docker-compose.yml',
    compose_content   TEXT NOT NULL DEFAULT '',
    image_ref         TEXT NOT NULL DEFAULT '',
    build_strategy    TEXT NOT NULL,
    registry_id       TEXT REFERENCES registries(id) ON DELETE SET NULL,
    image_name        TEXT NOT NULL DEFAULT '',
    workdir           TEXT NOT NULL,
    host_port         INTEGER,
    container_port    INTEGER NOT NULL DEFAULT 80,
    webhook_secret_id TEXT REFERENCES secrets(id) ON DELETE RESTRICT,
    status            TEXT NOT NULL DEFAULT 'never_deployed',
    current_run_id    TEXT,
    created_by        TEXT REFERENCES users(id) ON DELETE SET NULL,
    created_at        DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at        DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (server_id, slug),
    CONSTRAINT deployments_name_not_empty CHECK (name <> ''),
    CONSTRAINT deployments_source_valid CHECK (source_type IN ('git_dockerfile', 'git_compose', 'raw_compose', 'image')),
    CONSTRAINT deployments_strategy_valid CHECK (build_strategy IN ('remote', 'registry')),
    CONSTRAINT deployments_status_valid CHECK (status IN ('never_deployed', 'deploying', 'running', 'stopped', 'failed')),
    CONSTRAINT deployments_host_port_range CHECK (host_port IS NULL OR host_port BETWEEN 1 AND 65535)
);

CREATE INDEX deployments_server_idx ON deployments (server_id);

CREATE TABLE deployment_env (
    deployment_id TEXT NOT NULL REFERENCES deployments(id) ON DELETE CASCADE,
    key           TEXT NOT NULL,
    value         TEXT NOT NULL DEFAULT '',
    secret_id     TEXT REFERENCES secrets(id) ON DELETE RESTRICT,
    is_secret     BOOLEAN NOT NULL DEFAULT 0,
    PRIMARY KEY (deployment_id, key)
);

CREATE TABLE deployment_runs (
    id            TEXT PRIMARY KEY,
    deployment_id TEXT NOT NULL REFERENCES deployments(id) ON DELETE CASCADE,
    number        INTEGER NOT NULL,
    trigger       TEXT NOT NULL DEFAULT 'manual',
    status        TEXT NOT NULL DEFAULT 'queued',
    commit_sha    TEXT NOT NULL DEFAULT '',
    image_ref     TEXT NOT NULL DEFAULT '',
    error         TEXT NOT NULL DEFAULT '',
    triggered_by  TEXT REFERENCES users(id) ON DELETE SET NULL,
    queued_at     DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    started_at    DATETIME,
    finished_at   DATETIME,
    locked_at     DATETIME,
    locked_by     TEXT,
    UNIQUE (deployment_id, number),
    CONSTRAINT deployment_runs_trigger_valid CHECK (trigger IN ('manual', 'webhook', 'api', 'rollback')),
    CONSTRAINT deployment_runs_status_valid CHECK (status IN ('queued', 'running', 'succeeded', 'failed', 'cancelled'))
);

CREATE INDEX deployment_runs_queued_idx ON deployment_runs (queued_at);
CREATE INDEX deployment_runs_deployment_idx ON deployment_runs (deployment_id, number DESC);

CREATE TABLE deployment_logs (
    run_id TEXT NOT NULL REFERENCES deployment_runs(id) ON DELETE CASCADE,
    seq    INTEGER NOT NULL,
    stream TEXT NOT NULL DEFAULT 'stdout',
    line   TEXT NOT NULL,
    ts     DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (run_id, seq)
);

CREATE TABLE domains (
    id              TEXT PRIMARY KEY,
    deployment_id   TEXT REFERENCES deployments(id) ON DELETE CASCADE,
    server_id       TEXT NOT NULL REFERENCES servers(id) ON DELETE CASCADE,
    hostname        TEXT NOT NULL UNIQUE,
    upstream_port   INTEGER NOT NULL,
    ssl_mode        TEXT NOT NULL DEFAULT 'none',
    websocket       BOOLEAN NOT NULL DEFAULT 0,
    config_rendered TEXT NOT NULL DEFAULT '',
    cert_expires_at DATETIME,
    status          TEXT NOT NULL DEFAULT 'pending',
    status_message  TEXT NOT NULL DEFAULT '',
    created_by      TEXT REFERENCES users(id) ON DELETE SET NULL,
    created_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT domains_hostname_not_empty CHECK (hostname <> ''),
    CONSTRAINT domains_hostname_lowercase CHECK (hostname = lower(hostname)),
    CONSTRAINT domains_upstream_port_range CHECK (upstream_port BETWEEN 1 AND 65535),
    CONSTRAINT domains_ssl_mode_valid CHECK (ssl_mode IN ('none', 'letsencrypt')),
    CONSTRAINT domains_status_valid CHECK (status IN ('pending', 'active', 'error'))
);

CREATE INDEX domains_server_idx ON domains (server_id);
CREATE INDEX domains_deployment_idx ON domains (deployment_id);
CREATE INDEX domains_hostname_idx ON domains (hostname);

-- +goose Down
DROP TABLE domains;
DROP TABLE deployment_logs;
DROP TABLE deployment_runs;
DROP TABLE deployment_env;
DROP TABLE deployments;
DROP TABLE git_credentials;
DROP TABLE registries;
DROP TABLE server_members;
DROP TABLE servers;
DROP TABLE audit_log;
DROP TABLE api_tokens;
DROP TABLE invitations;
DROP TABLE sessions;
DROP TABLE users;
DROP TABLE secrets;
