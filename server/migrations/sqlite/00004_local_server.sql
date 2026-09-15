-- +goose Up

PRAGMA foreign_keys=OFF;

CREATE TABLE servers_dg_tmp (
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

INSERT INTO servers_dg_tmp SELECT * FROM servers;
DROP TABLE servers;
ALTER TABLE servers_dg_tmp RENAME TO servers;
CREATE INDEX servers_status_idx ON servers (status);

PRAGMA foreign_keys=ON;

-- +goose Down
