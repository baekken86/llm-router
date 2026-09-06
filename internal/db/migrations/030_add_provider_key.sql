-- +goose Up
ALTER TABLE providers ADD COLUMN provider_key TEXT NOT NULL DEFAULT '';
UPDATE providers SET provider_key = name WHERE provider_key = '';
CREATE UNIQUE INDEX IF NOT EXISTS idx_providers_provider_key ON providers(provider_key);

-- +goose Down
DROP INDEX IF EXISTS idx_providers_provider_key;
ALTER TABLE providers DROP COLUMN provider_key;
