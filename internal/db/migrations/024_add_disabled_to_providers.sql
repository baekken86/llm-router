-- +goose Up
ALTER TABLE providers ADD COLUMN disabled INTEGER NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE providers DROP COLUMN disabled;
