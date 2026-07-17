-- +goose Up
ALTER TABLE virtual_models ADD COLUMN include_models TEXT NOT NULL DEFAULT '[]';

-- +goose Down
ALTER TABLE virtual_models DROP COLUMN include_models;
