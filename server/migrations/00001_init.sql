-- +goose Up

-- Shared trigger used by every table that tracks updated_at.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION set_updated_at() RETURNS trigger AS $$
BEGIN
    NEW.updated_at = now();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

-- Every credential the platform stores on a user's behalf lands here as
-- AES-256-GCM ciphertext. Other tables reference a secret by id and never hold
-- sensitive material in their own columns.
--
-- The row id doubles as the GCM associated data, so a ciphertext copied onto a
-- different row will not decrypt.
CREATE TABLE secrets (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    kind       text        NOT NULL,
    nonce      bytea       NOT NULL,
    ciphertext bytea       NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT secrets_kind_not_empty CHECK (kind <> ''),
    CONSTRAINT secrets_nonce_length CHECK (octet_length(nonce) = 12)
);

CREATE TRIGGER secrets_set_updated_at
    BEFORE UPDATE ON secrets
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- +goose Down
DROP TABLE secrets;
DROP FUNCTION set_updated_at();
