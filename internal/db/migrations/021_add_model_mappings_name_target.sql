-- +goose Up
DROP TABLE IF EXISTS model_mappings;
CREATE TABLE model_mappings (
    source_model_id INTEGER PRIMARY KEY REFERENCES models(id) ON DELETE CASCADE,
    target_model_name TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);

-- +goose Down
DROP TABLE model_mappings;
