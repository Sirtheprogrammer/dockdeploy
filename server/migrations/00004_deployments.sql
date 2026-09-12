-- +goose Up

-- Registries are needed only by the "build here, push, pull there" strategy.
-- The remote strategy builds on the target and needs none of this.
CREATE TABLE registries (
    id        uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name      text NOT NULL UNIQUE,
    url       text NOT NULL,
    username  text NOT NULL,
    secret_id uuid REFERENCES secrets(id) ON DELETE RESTRICT,

    created_by uuid        REFERENCES users(id) ON DELETE SET NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT registries_name_not_empty CHECK (name <> ''),
    CONSTRAINT registries_url_not_empty CHECK (url <> '')
);

CREATE TRIGGER registries_set_updated_at
    BEFORE UPDATE ON registries FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- Credentials for private repositories. provider is always 'generic' today;
-- the column exists so a GitHub App can be added later without a migration
-- that touches every deployment.
CREATE TABLE git_credentials (
    id        uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name      text NOT NULL UNIQUE,
    provider  text NOT NULL DEFAULT 'generic',
    kind      text NOT NULL,
    username  text NOT NULL DEFAULT '',
    secret_id uuid REFERENCES secrets(id) ON DELETE RESTRICT,

    created_by uuid        REFERENCES users(id) ON DELETE SET NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT git_credentials_name_not_empty CHECK (name <> ''),
    CONSTRAINT git_credentials_kind_valid CHECK (kind IN ('token', 'ssh_key'))
);

CREATE TRIGGER git_credentials_set_updated_at
    BEFORE UPDATE ON git_credentials FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE deployments (
    id        uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    server_id uuid NOT NULL REFERENCES servers(id) ON DELETE CASCADE,
    name      text NOT NULL,
    -- slug names the remote working directory and the compose project, so it
    -- is restricted to characters that are safe in both.
    slug      text NOT NULL,

    source_type text NOT NULL,

    repo_url          text NOT NULL DEFAULT '',
    git_ref           text NOT NULL DEFAULT '',
    git_credential_id uuid REFERENCES git_credentials(id) ON DELETE SET NULL,

    dockerfile_path text NOT NULL DEFAULT 'Dockerfile',
    build_context   text NOT NULL DEFAULT '.',
    compose_path    text NOT NULL DEFAULT 'docker-compose.yml',
    -- Holds a pasted compose file for source_type = raw_compose.
    compose_content text NOT NULL DEFAULT '',
    -- Holds a ready-made image for source_type = image.
    image_ref       text NOT NULL DEFAULT '',

    build_strategy text NOT NULL,
    registry_id    uuid REFERENCES registries(id) ON DELETE SET NULL,
    image_name     text NOT NULL DEFAULT '',

    workdir text NOT NULL,
    -- Allocated from a configured range and published on loopback, so a
    -- service is not exposed publicly before nginx fronts it.
    host_port      integer,
    container_port integer NOT NULL DEFAULT 80,

    -- Signs push-to-deploy webhook payloads.
    webhook_secret_id uuid REFERENCES secrets(id) ON DELETE RESTRICT,

    status         text NOT NULL DEFAULT 'never_deployed',
    current_run_id uuid,

    created_by uuid        REFERENCES users(id) ON DELETE SET NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),

    UNIQUE (server_id, slug),
    CONSTRAINT deployments_name_not_empty CHECK (name <> ''),
    CONSTRAINT deployments_slug_shape CHECK (slug ~ '^[a-z0-9][a-z0-9-]{0,48}[a-z0-9]$'),
    CONSTRAINT deployments_source_valid
        CHECK (source_type IN ('git_dockerfile', 'git_compose', 'raw_compose', 'image')),
    CONSTRAINT deployments_strategy_valid
        CHECK (build_strategy IN ('remote', 'registry')),
    CONSTRAINT deployments_status_valid
        CHECK (status IN ('never_deployed', 'deploying', 'running', 'stopped', 'failed')),
    CONSTRAINT deployments_host_port_range
        CHECK (host_port IS NULL OR host_port BETWEEN 1 AND 65535),
    -- A git source needs a repository; a raw compose needs content; an image
    -- deployment needs a reference. Enforced here so a half-configured row
    -- cannot reach the build worker.
    CONSTRAINT deployments_source_complete CHECK (
        (source_type IN ('git_dockerfile', 'git_compose') AND repo_url <> '') OR
        (source_type = 'raw_compose' AND compose_content <> '') OR
        (source_type = 'image' AND image_ref <> '')
    )
);

CREATE TRIGGER deployments_set_updated_at
    BEFORE UPDATE ON deployments FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE INDEX deployments_server_idx ON deployments (server_id);

-- Environment variables. Values marked secret are encrypted; the rest are
-- stored in the clear so they can be shown back in the UI without decryption.
CREATE TABLE deployment_env (
    deployment_id uuid    NOT NULL REFERENCES deployments(id) ON DELETE CASCADE,
    key           text    NOT NULL,
    value         text    NOT NULL DEFAULT '',
    secret_id     uuid    REFERENCES secrets(id) ON DELETE RESTRICT,
    is_secret     boolean NOT NULL DEFAULT false,

    PRIMARY KEY (deployment_id, key),
    CONSTRAINT deployment_env_key_shape CHECK (key ~ '^[A-Za-z_][A-Za-z0-9_]*$'),
    -- A secret value lives in secrets, a plain one in value. Never both.
    CONSTRAINT deployment_env_storage CHECK (
        (is_secret AND secret_id IS NOT NULL AND value = '') OR
        (NOT is_secret AND secret_id IS NULL)
    )
);

-- Runs double as the job queue. Workers claim rows with SELECT ... FOR UPDATE
-- SKIP LOCKED and are woken by LISTEN/NOTIFY, so there is no Redis and no
-- polling latency.
CREATE TABLE deployment_runs (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    deployment_id uuid    NOT NULL REFERENCES deployments(id) ON DELETE CASCADE,
    -- Human-facing counter, per deployment: "run 7" rather than a uuid.
    number        integer NOT NULL,

    trigger text NOT NULL DEFAULT 'manual',
    status  text NOT NULL DEFAULT 'queued',

    commit_sha text NOT NULL DEFAULT '',
    image_ref  text NOT NULL DEFAULT '',
    error      text NOT NULL DEFAULT '',

    triggered_by uuid REFERENCES users(id) ON DELETE SET NULL,

    queued_at   timestamptz NOT NULL DEFAULT now(),
    started_at  timestamptz,
    finished_at timestamptz,

    -- Set when a worker claims the run. A stale value means the worker died
    -- mid-run and the row can be reclaimed.
    locked_at timestamptz,
    locked_by text,

    UNIQUE (deployment_id, number),
    CONSTRAINT deployment_runs_trigger_valid
        CHECK (trigger IN ('manual', 'webhook', 'api', 'rollback')),
    CONSTRAINT deployment_runs_status_valid
        CHECK (status IN ('queued', 'running', 'succeeded', 'failed', 'cancelled'))
);

-- Partial index: the worker only ever queries for queued rows, and this keeps
-- that lookup cheap no matter how much history accumulates.
CREATE INDEX deployment_runs_queued_idx
    ON deployment_runs (queued_at) WHERE status = 'queued';
CREATE INDEX deployment_runs_deployment_idx
    ON deployment_runs (deployment_id, number DESC);

-- Every log line is persisted as well as streamed, so a client that connects
-- late or reloads can replay from here and then attach to the live stream.
CREATE TABLE deployment_logs (
    run_id uuid   NOT NULL REFERENCES deployment_runs(id) ON DELETE CASCADE,
    seq    bigint NOT NULL,
    stream text   NOT NULL DEFAULT 'stdout',
    line   text   NOT NULL,
    ts     timestamptz NOT NULL DEFAULT now(),

    PRIMARY KEY (run_id, seq)
);

ALTER TABLE deployments
    ADD CONSTRAINT deployments_current_run_fk
    FOREIGN KEY (current_run_id) REFERENCES deployment_runs(id) ON DELETE SET NULL;

-- +goose Down
ALTER TABLE deployments DROP CONSTRAINT deployments_current_run_fk;
DROP TABLE deployment_logs;
DROP TABLE deployment_runs;
DROP TABLE deployment_env;
DROP TABLE deployments;
DROP TABLE git_credentials;
DROP TABLE registries;
