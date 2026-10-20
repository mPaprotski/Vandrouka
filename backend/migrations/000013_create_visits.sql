-- +goose Up
CREATE TABLE visit_photos (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    place_id BIGINT NOT NULL REFERENCES places(id),
    object_key TEXT NOT NULL UNIQUE CHECK (length(object_key) BETWEEN 1 AND 512),
    status TEXT NOT NULL DEFAULT 'uploading' CHECK (status IN ('uploading', 'ready', 'failed')),
    mime_type TEXT CHECK (mime_type IN ('image/jpeg', 'image/png')),
    size_bytes BIGINT CHECK (size_bytes > 0),
    width INTEGER CHECK (width > 0),
    height INTEGER CHECK (height > 0),
    sha256 BYTEA CHECK (octet_length(sha256) = 32),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    upload_expires_at TIMESTAMPTZ NOT NULL,
    ready_at TIMESTAMPTZ,
    deleted_at TIMESTAMPTZ,
    UNIQUE (id, user_id, place_id),
    CHECK (upload_expires_at > created_at),
    CHECK ((status = 'ready') = (ready_at IS NOT NULL)),
    CHECK (status <> 'ready' OR (mime_type IS NOT NULL AND size_bytes IS NOT NULL
        AND width IS NOT NULL AND height IS NOT NULL AND sha256 IS NOT NULL))
);
CREATE INDEX visit_photos_owner_idx ON visit_photos(user_id, created_at DESC, id DESC);
CREATE INDEX visit_photos_cleanup_idx ON visit_photos(upload_expires_at)
    WHERE status = 'uploading' AND deleted_at IS NULL;

CREATE TABLE visits (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    place_id BIGINT NOT NULL REFERENCES places(id),
    photo_id UUID NOT NULL,
    idempotency_key UUID NOT NULL,
    request_hash BYTEA NOT NULL CHECK (octet_length(request_hash) = 32),
    status TEXT NOT NULL CHECK (status IN ('confirmed', 'rejected')),
    rejection_code TEXT CHECK (rejection_code IN ('too_far', 'poor_accuracy',
        'stale_location', 'future_location', 'inactive_place', 'photo_not_ready')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    confirmed_at TIMESTAMPTZ,
    location_observed_at TIMESTAMPTZ,
    distance_m DOUBLE PRECISION CHECK (distance_m >= 0 AND distance_m < 'Infinity'::double precision),
    accuracy_m DOUBLE PRECISION CHECK (accuracy_m >= 0 AND accuracy_m < 'Infinity'::double precision),
    deleted_at TIMESTAMPTZ,
    UNIQUE (user_id, idempotency_key),
    UNIQUE (id, user_id, place_id, status),
    FOREIGN KEY (photo_id, user_id, place_id) REFERENCES visit_photos(id, user_id, place_id),
    CHECK ((status = 'confirmed' AND confirmed_at IS NOT NULL AND rejection_code IS NULL
            AND location_observed_at IS NOT NULL AND distance_m IS NOT NULL AND accuracy_m IS NOT NULL)
        OR (status = 'rejected' AND confirmed_at IS NULL AND rejection_code IS NOT NULL))
);
CREATE UNIQUE INDEX visits_confirmed_photo_idx ON visits(photo_id) WHERE status = 'confirmed';
CREATE INDEX visits_history_idx ON visits(user_id, created_at DESC, id DESC)
    WHERE status = 'confirmed' AND deleted_at IS NULL;

-- Serialize consumption against media cleanup; raw GPS coordinates are not stored.
-- +goose StatementBegin
CREATE FUNCTION check_visit_photo_ready() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.status = 'confirmed' THEN
        PERFORM 1 FROM visit_photos WHERE id = NEW.photo_id AND user_id = NEW.user_id
            AND place_id = NEW.place_id AND status = 'ready' AND deleted_at IS NULL FOR UPDATE;
        IF NOT FOUND THEN
            RAISE EXCEPTION 'photo is not ready for confirmation' USING ERRCODE = '23514';
        END IF;
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER visits_ready_photo BEFORE INSERT OR UPDATE OF photo_id, user_id, place_id, status
    ON visits FOR EACH ROW EXECUTE FUNCTION check_visit_photo_ready();

CREATE TABLE xp_transactions (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    place_id BIGINT NOT NULL REFERENCES places(id),
    visit_id BIGINT NOT NULL UNIQUE,
    visit_status TEXT NOT NULL DEFAULT 'confirmed' CHECK (visit_status = 'confirmed'),
    reason TEXT NOT NULL DEFAULT 'first_visit' CHECK (reason = 'first_visit'),
    amount INTEGER NOT NULL CHECK (amount = 100),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (user_id, place_id),
    FOREIGN KEY (visit_id, user_id, place_id, visit_status) REFERENCES visits(id, user_id, place_id, status)
);
CREATE INDEX xp_transactions_owner_idx ON xp_transactions(user_id, id);

-- +goose Down
DROP TABLE xp_transactions;
DROP TABLE visits;
DROP FUNCTION check_visit_photo_ready();
DROP TABLE visit_photos;
