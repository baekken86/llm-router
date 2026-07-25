-- +goose Up
ALTER TABLE models ADD COLUMN disabled_until TIMESTAMP NULL;
ALTER TABLE providers ADD COLUMN disabled_until TIMESTAMP NULL;

-- +goose Down
ALTER TABLE models DROP COLUMN disabled_until;
ALTER TABLE providers DROP COLUMN disabled_until;
