-- +goose Up
CREATE TABLE IF NOT EXISTS model_metadata_global (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    model_name TEXT NOT NULL,
    reasoning_effort TEXT NOT NULL DEFAULT '',
    key TEXT NOT NULL,
    value TEXT NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_global_model_effort_key ON model_metadata_global(model_name, reasoning_effort, key);

-- +goose Down
DROP TABLE IF EXISTS model_metadata_global;
