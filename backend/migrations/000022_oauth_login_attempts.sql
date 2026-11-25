-- +goose Up
CREATE TABLE oauth_login_attempts (
    id TEXT PRIMARY KEY,
    provider TEXT NOT NULL CHECK (provider IN ('google','yandex')),
    device_hash BYTEA NOT NULL CHECK (octet_length(device_hash)=32),
    expires_at TIMESTAMPTZ NOT NULL,
    phase TEXT NOT NULL DEFAULT 'new' CHECK (phase IN ('new','browser','ready')),
    state_hash BYTEA UNIQUE,
    cookie_hash BYTEA,
    verifier TEXT,
    nonce TEXT,
    return_hash BYTEA,
    outcome TEXT CHECK (outcome IN ('verified','cancelled','provider_failed')),
    subject TEXT,
    email TEXT
);
CREATE INDEX oauth_login_expiry ON oauth_login_attempts(expires_at);

-- +goose Down
DROP TABLE oauth_login_attempts;
