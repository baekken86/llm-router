-- +goose Up
CREATE TABLE IF NOT EXISTS model_tags (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    model_id INTEGER NOT NULL REFERENCES models(id) ON DELETE CASCADE,
    key TEXT NOT NULL,
    value TEXT NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(model_id, key)
);

CREATE INDEX IF NOT EXISTS idx_model_tags_key ON model_tags(key);
CREATE INDEX IF NOT EXISTS idx_model_tags_value ON model_tags(value);

-- +goose Down
DROP TABLE IF EXISTS model_tags;
