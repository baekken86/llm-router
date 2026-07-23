-- +goose Up
ALTER TABLE models ADD COLUMN disabled INTEGER NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE models DROP COLUMN disabled;
