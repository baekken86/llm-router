-- +goose Up
ALTER TABLE global_sort_conditions ADD COLUMN priority INTEGER NOT NULL DEFAULT 0;
UPDATE global_sort_conditions SET priority = position * 10;

ALTER TABLE global_filter_conditions ADD COLUMN priority INTEGER NOT NULL DEFAULT 0;
UPDATE global_filter_conditions SET priority = position * 10;

-- +goose Down
ALTER TABLE global_sort_conditions DROP COLUMN priority;
ALTER TABLE global_filter_conditions DROP COLUMN priority;
