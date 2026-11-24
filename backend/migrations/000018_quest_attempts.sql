-- +goose Up
ALTER TABLE visit_photos ADD COLUMN purpose TEXT NOT NULL DEFAULT 'visit'
    CHECK (purpose IN ('visit', 'quest'));

ALTER TABLE quest_participations
    ADD CONSTRAINT quest_participation_owner_key UNIQUE (id, run_id, user_id),
    ADD COLUMN completed_at TIMESTAMPTZ,
    ALTER COLUMN current_position DROP NOT NULL,
    ADD CONSTRAINT quest_participation_completion_check
        CHECK ((current_position IS NULL) = (completed_at IS NOT NULL));

ALTER TABLE quest_run_steps ADD CONSTRAINT quest_run_step_place_key
    UNIQUE (run_id, position, place_id);

CREATE TABLE quest_attempts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    participation_id UUID NOT NULL,
    run_id UUID NOT NULL,
    user_id BIGINT NOT NULL,
    position INTEGER NOT NULL,
    place_id BIGINT NOT NULL,
    photo_id UUID UNIQUE,
    upload_hash BYTEA CHECK (octet_length(upload_hash) = 32),
    submission_key UUID,
    submission_hash BYTEA CHECK (octet_length(submission_hash) = 32),
    status TEXT NOT NULL DEFAULT 'draft' CHECK (status IN ('draft','pending_review','accepted','rejected','withdrawn')),
    revision BIGINT NOT NULL DEFAULT 1 CHECK (revision >= 1),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    submitted_at TIMESTAMPTZ,
    location_observed_at TIMESTAMPTZ,
    distance_m DOUBLE PRECISION CHECK (distance_m >= 0 AND distance_m < 'Infinity'::double precision),
    accuracy_m DOUBLE PRECISION CHECK (accuracy_m >= 0 AND accuracy_m < 'Infinity'::double precision),
    reviewed_at TIMESTAMPTZ,
    rejection_reason TEXT CHECK (char_length(rejection_reason) BETWEEN 1 AND 1000),
    visit_id BIGINT UNIQUE REFERENCES visits(id) ON DELETE CASCADE,
    xp_awarded INTEGER NOT NULL DEFAULT 0 CHECK (xp_awarded IN (0,100)),
    UNIQUE (participation_id, submission_key),
    FOREIGN KEY (participation_id, run_id, user_id)
        REFERENCES quest_participations(id, run_id, user_id) ON DELETE CASCADE,
    FOREIGN KEY (run_id, position, place_id)
        REFERENCES quest_run_steps(run_id, position, place_id) ON DELETE CASCADE,
    FOREIGN KEY (photo_id, user_id, place_id)
        REFERENCES visit_photos(id, user_id, place_id) ON DELETE CASCADE,
    CHECK ((photo_id IS NULL) = (upload_hash IS NULL)),
    CHECK ((submission_key IS NULL) = (submission_hash IS NULL)),
    CHECK ((submission_key IS NULL) = (submitted_at IS NULL)),
    CHECK (submitted_at IS NULL OR (photo_id IS NOT NULL AND location_observed_at IS NOT NULL
        AND distance_m IS NOT NULL AND accuracy_m IS NOT NULL)),
    CHECK (status NOT IN ('pending_review','accepted','rejected') OR submitted_at IS NOT NULL),
    CHECK ((status IN ('accepted','rejected')) = (reviewed_at IS NOT NULL)),
    CHECK ((status = 'rejected') = (rejection_reason IS NOT NULL)),
    CHECK ((status = 'accepted') = (visit_id IS NOT NULL)),
    CHECK (status = 'accepted' OR xp_awarded = 0)
);
CREATE UNIQUE INDEX quest_attempts_open_step_idx ON quest_attempts(participation_id, position)
    WHERE status IN ('draft','pending_review','accepted');
CREATE INDEX quest_attempts_review_idx ON quest_attempts(run_id, id) WHERE status='pending_review';
CREATE INDEX quest_attempts_current_idx ON quest_attempts(participation_id, position, created_at DESC, id DESC);

-- A storage write can finish after its HTTP call is cancelled. Keep the
-- deletion job delayed until the upload lease expires in that case.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION queue_photo_deletion() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
    delete_after TIMESTAMPTZ := NOW();
BEGIN
    IF OLD.purpose = 'quest' AND OLD.status = 'uploading' THEN
        delete_after := GREATEST(delete_after, OLD.upload_expires_at);
    END IF;
    IF TG_OP = 'DELETE' THEN
        INSERT INTO media_deletions(object_key,not_before)
            VALUES(OLD.object_key,delete_after) ON CONFLICT DO NOTHING;
        RETURN OLD;
    END IF;
    IF NEW.deleted_at IS NOT NULL AND OLD.deleted_at IS NULL THEN
        INSERT INTO media_deletions(object_key,not_before)
            VALUES(NEW.object_key,delete_after) ON CONFLICT DO NOTHING;
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

-- +goose Down
UPDATE quest_participations p SET current_position=(
    SELECT max(position) FROM quest_run_steps WHERE run_id=p.run_id
), completed_at=NULL WHERE completed_at IS NOT NULL;
DROP TABLE quest_attempts;
ALTER TABLE quest_run_steps DROP CONSTRAINT quest_run_step_place_key;
ALTER TABLE quest_participations
    DROP CONSTRAINT quest_participation_completion_check,
    DROP COLUMN completed_at,
    ALTER COLUMN current_position SET NOT NULL,
    DROP CONSTRAINT quest_participation_owner_key;
DELETE FROM visit_photos p WHERE p.purpose='quest'
    AND NOT EXISTS (SELECT 1 FROM visits v WHERE v.photo_id=p.id);
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION queue_photo_deletion() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        INSERT INTO media_deletions(object_key) VALUES(OLD.object_key) ON CONFLICT DO NOTHING;
        RETURN OLD;
    END IF;
    IF NEW.deleted_at IS NOT NULL AND OLD.deleted_at IS NULL THEN
        INSERT INTO media_deletions(object_key) VALUES(NEW.object_key) ON CONFLICT DO NOTHING;
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
ALTER TABLE visit_photos DROP COLUMN purpose;
