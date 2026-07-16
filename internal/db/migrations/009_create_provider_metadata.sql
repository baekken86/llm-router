-- +goose Up
CREATE TABLE IF NOT EXISTS provider_metadata (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    provider_id INTEGER NOT NULL REFERENCES providers(id) ON DELETE CASCADE,
    key TEXT NOT NULL,
    value TEXT NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_provider_metadata_provider_key ON provider_metadata(provider_id, key);
CREATE INDEX IF NOT EXISTS idx_provider_metadata_key ON provider_metadata(key);
CREATE INDEX IF NOT EXISTS idx_provider_metadata_value ON provider_metadata(value);

-- +goose Down
DROP TABLE IF EXISTS provider_metadata;
