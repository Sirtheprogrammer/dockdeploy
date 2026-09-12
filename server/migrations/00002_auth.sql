-- +goose Up

CREATE TABLE users (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    -- Normalised to lowercase in Go; the constraint keeps a stray direct
    -- INSERT from creating a duplicate that only differs by case.
    email         text        NOT NULL UNIQUE,
    name          text        NOT NULL,
    password_hash text        NOT NULL,
    role          text        NOT NULL,
    status        text        NOT NULL DEFAULT 'active',
    last_login_at timestamptz,
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT users_email_lowercase CHECK (email = lower(email)),
    CONSTRAINT users_email_shape CHECK (email LIKE '%_@_%'),
    CONSTRAINT users_name_not_empty CHECK (name <> ''),
    CONSTRAINT users_role_valid CHECK (role IN ('admin', 'member', 'viewer')),
    CONSTRAINT users_status_valid CHECK (status IN ('active', 'suspended'))
);

CREATE TRIGGER users_set_updated_at
    BEFORE UPDATE ON users
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- Cookie sessions. The cookie carries a random token; only its keyed hash is
-- stored, so a database dump does not hand over live sessions.
CREATE TABLE sessions (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id      uuid        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash   bytea       NOT NULL UNIQUE,
    expires_at   timestamptz NOT NULL,
    last_used_at timestamptz NOT NULL DEFAULT now(),
    ip           text,
    user_agent   text,
    created_at   timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX sessions_user_id_idx ON sessions (user_id);
CREATE INDEX sessions_expires_at_idx ON sessions (expires_at);

-- Invitations are links an admin hands out; there is no mail server to depend
-- on in a self-hosted install.
CREATE TABLE invitations (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    email            text        NOT NULL,
    name             text        NOT NULL DEFAULT '',
    role             text        NOT NULL,
    token_hash       bytea       NOT NULL UNIQUE,
    invited_by       uuid        REFERENCES users(id) ON DELETE SET NULL,
    expires_at       timestamptz NOT NULL,
    accepted_at      timestamptz,
    accepted_user_id uuid        REFERENCES users(id) ON DELETE SET NULL,
    created_at       timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT invitations_email_lowercase CHECK (email = lower(email)),
    CONSTRAINT invitations_role_valid CHECK (role IN ('admin', 'member', 'viewer'))
);

CREATE INDEX invitations_email_idx ON invitations (email);

-- Bearer tokens so CI can trigger deploys without a browser session.
CREATE TABLE api_tokens (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id      uuid        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name         text        NOT NULL,
    token_hash   bytea       NOT NULL UNIQUE,
    -- Shown in the UI so a token can be recognised after its secret is gone.
    prefix       text        NOT NULL,
    last_used_at timestamptz,
    expires_at   timestamptz,
    created_at   timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT api_tokens_name_not_empty CHECK (name <> '')
);

CREATE INDEX api_tokens_user_id_idx ON api_tokens (user_id);

-- Who did what. Servers and deployments are shared, destructive surfaces, so
-- every mutating request lands here.
CREATE TABLE audit_log (
    id            bigserial PRIMARY KEY,
    user_id       uuid        REFERENCES users(id) ON DELETE SET NULL,
    -- Kept alongside user_id so history stays readable after a user is removed.
    actor_email   text        NOT NULL DEFAULT '',
    action        text        NOT NULL,
    resource_type text        NOT NULL DEFAULT '',
    resource_id   text        NOT NULL DEFAULT '',
    status        integer     NOT NULL,
    ip            text        NOT NULL DEFAULT '',
    meta          jsonb       NOT NULL DEFAULT '{}'::jsonb,
    created_at    timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX audit_log_created_at_idx ON audit_log (created_at DESC);
CREATE INDEX audit_log_user_idx ON audit_log (user_id, created_at DESC);
CREATE INDEX audit_log_resource_idx ON audit_log (resource_type, resource_id, created_at DESC);

-- +goose Down
DROP TABLE audit_log;
DROP TABLE api_tokens;
DROP TABLE invitations;
DROP TABLE sessions;
DROP TABLE users;
