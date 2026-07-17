-- +goose Up
ALTER TABLE virtual_models ADD COLUMN composition TEXT DEFAULT NULL;

-- +goose Down
ALTER TABLE virtual_models DROP COLUMN composition;
