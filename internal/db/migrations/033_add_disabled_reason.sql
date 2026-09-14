-- +goose Up
ALTER TABLE models ADD COLUMN disabled_reason TEXT;
UPDATE models SET disabled_reason = 'stale' WHERE disabled = 1;

-- +goose Down
ALTER TABLE models DROP COLUMN disabled_reason;
