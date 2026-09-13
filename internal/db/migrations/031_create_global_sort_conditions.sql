-- +goose Up
CREATE TABLE IF NOT EXISTS global_sort_conditions (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL UNIQUE,
    description TEXT,
    sort_expr TEXT NOT NULL DEFAULT '[]',
    enabled INTEGER NOT NULL DEFAULT 1,
    position INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- Per-virtual-model overrides: a row here means the VM disables that global condition.
CREATE TABLE IF NOT EXISTS virtual_model_sort_disabled (
    virtual_model_id INTEGER NOT NULL REFERENCES virtual_models(id) ON DELETE CASCADE,
    condition_id INTEGER NOT NULL REFERENCES global_sort_conditions(id) ON DELETE CASCADE,
    PRIMARY KEY (virtual_model_id, condition_id)
);

-- +goose Down
DROP TABLE IF EXISTS virtual_model_sort_disabled;
DROP TABLE IF EXISTS global_sort_conditions;
