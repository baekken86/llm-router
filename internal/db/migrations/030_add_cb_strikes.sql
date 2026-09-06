-- +goose Up
ALTER TABLE models ADD COLUMN cb_strikes INTEGER NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE models DROP COLUMN cb_strikes;
