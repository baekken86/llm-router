-- +goose Up
ALTER TABLE virtual_models ADD COLUMN description TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE virtual_models DROP COLUMN description;
