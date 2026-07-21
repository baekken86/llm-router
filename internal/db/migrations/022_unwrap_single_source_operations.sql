-- +goose Up
-- Unwrap operation nodes that have only 1 source (invalid state from earlier versions).
-- A union/intersection/difference with <2 sources should be replaced by its sole source.

UPDATE virtual_models
SET composition = json_extract(composition, '$.sources[0]')
WHERE composition IS NOT NULL
  AND composition != ''
  AND composition != 'null'
  AND json_extract(composition, '$.operation') IS NOT NULL
  AND json_array_length(json_extract(composition, '$.sources')) = 1;

-- +goose Down
-- Data migration not reversible without storing original composition.
-- If needed, restore from backup.
