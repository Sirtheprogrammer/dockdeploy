-- +goose Up

ALTER TABLE users ADD COLUMN totp_secret_id TEXT REFERENCES secrets(id) ON DELETE SET NULL;
ALTER TABLE users ADD COLUMN totp_recovery_secret_id TEXT REFERENCES secrets(id) ON DELETE SET NULL;
ALTER TABLE users ADD COLUMN totp_enabled BOOLEAN NOT NULL DEFAULT 0;

-- +goose Down

ALTER TABLE users DROP COLUMN totp_enabled;
ALTER TABLE users DROP COLUMN totp_recovery_secret_id;
ALTER TABLE users DROP COLUMN totp_secret_id;
