-- +goose Up
ALTER TABLE oauth_login_attempts ADD COLUMN purpose TEXT NOT NULL DEFAULT 'login' CHECK(purpose IN ('login','link','delete')),
 ADD COLUMN owner_id BIGINT REFERENCES users(id) ON DELETE CASCADE, ADD COLUMN access_hash BYTEA;
ALTER TABLE sessions ADD COLUMN external_login BOOLEAN NOT NULL DEFAULT false;
-- +goose Down
ALTER TABLE sessions DROP COLUMN external_login;
ALTER TABLE oauth_login_attempts DROP COLUMN purpose,DROP COLUMN owner_id,DROP COLUMN access_hash;
