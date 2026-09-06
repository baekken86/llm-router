-- +goose Up
-- reasoning_effort is already part of model_tags as created by migration 003
-- (the column was retroactively inlined there so fresh installs never need
-- this ALTER — running it would fail with "duplicate column"). Existing DBs
-- that predate 003's inlined column applied the original version of this
-- migration, which is already recorded in schema_migrations and never
-- re-runs. This migration now only upgrades the uniqueness index.
DROP INDEX IF EXISTS idx_model_tags_model_key;
CREATE UNIQUE INDEX IF NOT EXISTS idx_model_tags_model_effort_key ON model_tags(model_id, reasoning_effort, key);

-- +goose Down
ALTER TABLE model_tags DROP COLUMN reasoning_effort;
DROP INDEX IF EXISTS idx_model_tags_model_effort_key;
CREATE UNIQUE INDEX IF NOT EXISTS idx_model_tags_model_key ON model_tags(model_id, key);
