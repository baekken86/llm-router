-- +goose Up
CREATE TABLE IF NOT EXISTS model_overrides (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    model_id INTEGER NOT NULL REFERENCES models(id) ON DELETE CASCADE,
    reasoning_effort TEXT NOT NULL DEFAULT '',
    key TEXT NOT NULL,
    value TEXT NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(model_id, reasoning_effort, key)
);

-- +goose Down
DROP TABLE IF EXISTS model_overrides;
