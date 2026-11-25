-- +goose Up
ALTER TABLE users ALTER COLUMN password_hash DROP NOT NULL;

CREATE TABLE user_external_identities (
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    provider TEXT NOT NULL CHECK (provider IN ('google', 'yandex')),
    subject TEXT NOT NULL CHECK (octet_length(subject) BETWEEN 1 AND 255),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_login_at TIMESTAMPTZ,
    PRIMARY KEY (provider, subject),
    UNIQUE (user_id, provider)
);

-- +goose Down
-- Disable provider login at the application level once identities are in use.
-- Rolling back this schema would discard working sign-in methods.
-- +goose StatementBegin
DO $$
BEGIN
    LOCK TABLE user_external_identities IN ACCESS EXCLUSIVE MODE;
    IF EXISTS (SELECT 1 FROM user_external_identities) THEN
        RAISE EXCEPTION 'External identities exist; disable provider login instead of rolling back this migration';
    END IF;
END;
$$;
-- +goose StatementEnd
ALTER TABLE users ALTER COLUMN password_hash SET NOT NULL;
DROP TABLE user_external_identities;
