-- +goose Up
ALTER TABLE oauth_tokens ADD COLUMN last_refresh_at DATETIME;
ALTER TABLE oauth_tokens ADD COLUMN id_token TEXT;

-- +goose Down
ALTER TABLE oauth_tokens DROP COLUMN last_refresh_at;
ALTER TABLE oauth_tokens DROP COLUMN id_token;
