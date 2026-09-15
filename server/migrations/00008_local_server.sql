-- +goose Up

ALTER TABLE servers DROP CONSTRAINT IF EXISTS servers_auth_method_valid;
ALTER TABLE servers ADD CONSTRAINT servers_auth_method_valid CHECK (auth_method IN ('password', 'key', 'local'));

ALTER TABLE servers DROP CONSTRAINT IF EXISTS servers_port_range;
ALTER TABLE servers ADD CONSTRAINT servers_port_range CHECK (port BETWEEN 0 AND 65535);

-- +goose Down

ALTER TABLE servers DROP CONSTRAINT IF EXISTS servers_auth_method_valid;
ALTER TABLE servers ADD CONSTRAINT servers_auth_method_valid CHECK (auth_method IN ('password', 'key'));

ALTER TABLE servers DROP CONSTRAINT IF EXISTS servers_port_range;
ALTER TABLE servers ADD CONSTRAINT servers_port_range CHECK (port BETWEEN 1 AND 65535);
