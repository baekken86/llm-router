-- +goose Up
ALTER TABLE model_tags ADD COLUMN reasoning_effort TEXT NOT NULL DEFAULT 'default';

DROP INDEX IF EXISTS idx_model_tags_model_key;
CREATE UNIQUE INDEX IF NOT EXISTS idx_model_tags_model_effort_key ON model_tags(model_id, reasoning_effort, key);

-- +goose Down
ALTER TABLE model_tags DROP COLUMN reasoning_effort;
DROP INDEX IF EXISTS idx_model_tags_model_effort_key;
CREATE UNIQUE INDEX IF NOT EXISTS idx_model_tags_model_key ON model_tags(model_id, key);
