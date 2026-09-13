-- +goose Up

ALTER TABLE users ADD COLUMN totp_secret_id uuid REFERENCES secrets(id) ON DELETE SET NULL;
ALTER TABLE users ADD COLUMN totp_recovery_secret_id uuid REFERENCES secrets(id) ON DELETE SET NULL;
ALTER TABLE users ADD COLUMN totp_enabled boolean NOT NULL DEFAULT false;

-- +goose Down

ALTER TABLE users DROP COLUMN IF EXISTS totp_enabled;
ALTER TABLE users DROP COLUMN IF EXISTS totp_recovery_secret_id;
ALTER TABLE users DROP COLUMN IF EXISTS totp_secret_id;
