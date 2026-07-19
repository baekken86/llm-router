-- +goose Up
CREATE TABLE model_mappings (
    source_model_id INTEGER PRIMARY KEY REFERENCES models(id) ON DELETE CASCADE,
    target_model_id INTEGER NOT NULL REFERENCES models(id) ON DELETE CASCADE,
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);

-- +goose Down
DROP TABLE model_mappings;
