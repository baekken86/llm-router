-- +goose Up
CREATE TABLE IF NOT EXISTS virtual_models (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL UNIQUE,
    filter_expr TEXT NOT NULL DEFAULT '{}',
    sort_expr TEXT NOT NULL DEFAULT '[]',
    max_retries INTEGER NOT NULL DEFAULT 1,
    retry_on_status TEXT NOT NULL DEFAULT '[429,500,502,503,504]',
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- +goose Down
DROP TABLE IF EXISTS virtual_models;
