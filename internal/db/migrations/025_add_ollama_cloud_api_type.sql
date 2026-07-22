-- +goose Up
CREATE TABLE providers_new (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL UNIQUE,
    api_type TEXT NOT NULL CHECK(api_type IN ('openai', 'anthropic', 'cloudflare', 'ollama', 'ollama-cloud')),
    base_url TEXT NOT NULL,
    api_key_encrypted TEXT NOT NULL,
    account_id TEXT NOT NULL DEFAULT '',
    disabled INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
INSERT INTO providers_new (id, name, api_type, base_url, api_key_encrypted, account_id, disabled, created_at, updated_at)
    SELECT id, name, api_type, base_url, api_key_encrypted, account_id, disabled, created_at, updated_at FROM providers;
DROP TABLE providers;
ALTER TABLE providers_new RENAME TO providers;
CREATE UNIQUE INDEX IF NOT EXISTS idx_providers_name ON providers(name);

-- +goose Down
CREATE TABLE providers_old (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL UNIQUE,
    api_type TEXT NOT NULL CHECK(api_type IN ('openai', 'anthropic', 'cloudflare', 'ollama')),
    base_url TEXT NOT NULL,
    api_key_encrypted TEXT NOT NULL,
    account_id TEXT NOT NULL DEFAULT '',
    disabled INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
INSERT INTO providers_old (id, name, api_type, base_url, api_key_encrypted, account_id, disabled, created_at, updated_at)
    SELECT id, name, api_type, base_url, api_key_encrypted, account_id, disabled, created_at, updated_at FROM providers;
DROP TABLE providers;
ALTER TABLE providers_old RENAME TO providers;
CREATE UNIQUE INDEX IF NOT EXISTS idx_providers_name ON providers(name);
