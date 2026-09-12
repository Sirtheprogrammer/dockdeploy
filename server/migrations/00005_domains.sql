-- +goose Up

CREATE TABLE domains (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    deployment_id   uuid REFERENCES deployments(id) ON DELETE CASCADE,
    server_id       uuid NOT NULL REFERENCES servers(id) ON DELETE CASCADE,
    hostname        text NOT NULL UNIQUE,
    upstream_port   integer NOT NULL,

    -- ssl_mode: none | letsencrypt
    ssl_mode        text NOT NULL DEFAULT 'none',
    websocket       boolean NOT NULL DEFAULT false,

    config_rendered text NOT NULL DEFAULT '',
    cert_expires_at timestamptz,

    status          text NOT NULL DEFAULT 'pending',
    status_message  text NOT NULL DEFAULT '',

    created_by      uuid REFERENCES users(id) ON DELETE SET NULL,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT domains_hostname_not_empty CHECK (hostname <> ''),
    CONSTRAINT domains_hostname_lowercase CHECK (hostname = lower(hostname)),
    CONSTRAINT domains_upstream_port_range CHECK (upstream_port BETWEEN 1 AND 65535),
    CONSTRAINT domains_ssl_mode_valid CHECK (ssl_mode IN ('none', 'letsencrypt')),
    CONSTRAINT domains_status_valid CHECK (status IN ('pending', 'active', 'error'))
);

CREATE TRIGGER domains_set_updated_at
    BEFORE UPDATE ON domains FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE INDEX domains_server_idx ON domains (server_id);
CREATE INDEX domains_deployment_idx ON domains (deployment_id);
CREATE INDEX domains_hostname_idx ON domains (hostname);

-- +goose Down
DROP TABLE domains;
