-- +goose Up

CREATE TABLE servers (
    id       uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name     text    NOT NULL UNIQUE,
    host     text    NOT NULL,
    port     integer NOT NULL DEFAULT 22,
    username text    NOT NULL,

    auth_method text NOT NULL,

    -- Credentials live in the secrets table; a server row holds only pointers.
    -- ON DELETE RESTRICT keeps a secret from vanishing out from under a server
    -- that still needs it. Deleting a server clears its secrets in the same
    -- transaction, in the correct order.
    secret_id               uuid REFERENCES secrets(id) ON DELETE RESTRICT,
    passphrase_secret_id    uuid REFERENCES secrets(id) ON DELETE RESTRICT,
    sudo_password_secret_id uuid REFERENCES secrets(id) ON DELETE RESTRICT,

    -- Pinned at creation, after the user confirmed it. Connections verify
    -- against this and refuse anything else, so a swapped host key stops the
    -- platform rather than being silently accepted.
    host_key_fingerprint text NOT NULL,

    docker_socket text NOT NULL DEFAULT '/var/run/docker.sock',

    -- Results of the last capability probe. Cached so the dashboard can show
    -- what a server supports without reconnecting on every page load.
    status         text        NOT NULL DEFAULT 'unknown',
    status_message text        NOT NULL DEFAULT '',
    capabilities   jsonb       NOT NULL DEFAULT '{}'::jsonb,
    last_seen_at   timestamptz,

    created_by uuid        REFERENCES users(id) ON DELETE SET NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT servers_name_not_empty CHECK (name <> ''),
    CONSTRAINT servers_host_not_empty CHECK (host <> ''),
    CONSTRAINT servers_port_range CHECK (port BETWEEN 1 AND 65535),
    CONSTRAINT servers_auth_method_valid CHECK (auth_method IN ('password', 'key')),
    CONSTRAINT servers_fingerprint_present CHECK (host_key_fingerprint <> ''),
    CONSTRAINT servers_status_valid
        CHECK (status IN ('unknown', 'online', 'offline', 'unauthorized'))
);

CREATE TRIGGER servers_set_updated_at
    BEFORE UPDATE ON servers
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE INDEX servers_status_idx ON servers (status);

-- Per-server grants layered on top of the global role. A member sees only the
-- servers they were given; admins see everything without needing a row here.
CREATE TABLE server_members (
    server_id  uuid        NOT NULL REFERENCES servers(id) ON DELETE CASCADE,
    user_id    uuid        NOT NULL REFERENCES users(id)   ON DELETE CASCADE,
    permission text        NOT NULL DEFAULT 'operate',
    created_at timestamptz NOT NULL DEFAULT now(),

    PRIMARY KEY (server_id, user_id),
    CONSTRAINT server_members_permission_valid
        CHECK (permission IN ('read', 'operate'))
);

CREATE INDEX server_members_user_idx ON server_members (user_id);

-- +goose Down
DROP TABLE server_members;
DROP TABLE servers;
