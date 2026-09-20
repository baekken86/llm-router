-- +goose Up
ALTER TABLE models ADD COLUMN rate_limit_isolated INTEGER NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE models DROP COLUMN rate_limit_isolated;
