-- +goose Up
CREATE TABLE session_families (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    revoked_at TIMESTAMPTZ,
    CHECK (expires_at > created_at)
);
CREATE INDEX session_families_owner_idx ON session_families(user_id);
ALTER TABLE sessions
    ADD COLUMN family_id UUID REFERENCES session_families(id) ON DELETE CASCADE,
    ADD COLUMN refresh_token_hash BYTEA UNIQUE,
    ADD COLUMN refresh_used_at TIMESTAMPTZ,
    ADD CONSTRAINT sessions_refresh_shape CHECK (
        (family_id IS NULL AND refresh_token_hash IS NULL AND refresh_used_at IS NULL)
        OR (family_id IS NOT NULL AND refresh_token_hash IS NOT NULL AND octet_length(refresh_token_hash)=32)
    );
CREATE INDEX sessions_family_idx ON sessions(family_id);

-- +goose Down
-- Preserve existing access tokens when rolling back refresh support.
ALTER TABLE sessions DROP COLUMN refresh_used_at, DROP COLUMN refresh_token_hash, DROP COLUMN family_id;
DROP TABLE session_families;
