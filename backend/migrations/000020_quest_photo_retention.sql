-- +goose Up
ALTER TABLE quest_attempts
    ADD COLUMN photo_expires_at TIMESTAMPTZ,
    DROP CONSTRAINT quest_attempts_status_check,
    ADD CONSTRAINT quest_attempts_status_check CHECK (
        status IN ('draft','pending_review','accepted','rejected','withdrawn','expired')
    );
UPDATE quest_attempts SET photo_expires_at=CASE status
    WHEN 'pending_review' THEN submitted_at+INTERVAL '168 hours'
    WHEN 'rejected' THEN reviewed_at+INTERVAL '24 hours' END
WHERE status IN ('pending_review','rejected');
ALTER TABLE quest_attempts ADD CONSTRAINT quest_attempts_photo_deadline_check CHECK (
    (photo_expires_at IS NOT NULL) = (status IN ('pending_review','rejected','expired'))
);
CREATE INDEX quest_attempts_photo_expiry_idx ON quest_attempts(photo_expires_at,id)
    WHERE status IN ('pending_review','rejected');

ALTER TABLE quest_runs DROP CONSTRAINT quest_run_finished_check,
    ADD CONSTRAINT quest_run_finished_check CHECK (
        finished_at IS NULL OR (NOT join_enabled AND (started_at IS NULL OR finished_at >= started_at))
    );

ALTER TABLE quests ALTER COLUMN owner_id DROP NOT NULL, DROP CONSTRAINT quests_owner_id_fkey,
    ADD CONSTRAINT quests_owner_id_fkey FOREIGN KEY (owner_id) REFERENCES users(id) ON DELETE SET NULL;

-- Keep the playback history and snapshots for the remaining participants.
-- Removal of the organizer closes the runs and removes any pending photos
-- from review immediately. The media worker later removes their bytes.
-- +goose StatementBegin
CREATE FUNCTION close_orphaned_quest() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF OLD.owner_id IS NOT NULL AND NEW.owner_id IS NULL THEN
        NEW.status := 'archived';
        NEW.title := '';
        NEW.description := '';
        DELETE FROM quest_steps WHERE quest_id=NEW.id;
        NEW.updated_at := clock_timestamp();
        NEW.revision := OLD.revision+1;
        UPDATE quest_runs SET join_enabled=false,
            finished_at=COALESCE(finished_at,GREATEST(started_at,clock_timestamp()))
        WHERE quest_id=NEW.id;
        UPDATE quest_attempts a SET photo_expires_at=LEAST(a.photo_expires_at,clock_timestamp())
        FROM quest_runs r WHERE r.quest_id=NEW.id AND a.run_id=r.id
            AND a.status IN ('pending_review','rejected');
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER quest_organizer_removed BEFORE UPDATE OF owner_id ON quests
    FOR EACH ROW EXECUTE FUNCTION close_orphaned_quest();

-- +goose Down
DROP TRIGGER quest_organizer_removed ON quests;
DROP FUNCTION close_orphaned_quest();
-- Older schemas cannot represent orphaned routes. Confirmed visits are independent.
DELETE FROM quests WHERE owner_id IS NULL;
ALTER TABLE quests DROP CONSTRAINT quests_owner_id_fkey, ALTER COLUMN owner_id SET NOT NULL,
    ADD CONSTRAINT quests_owner_id_fkey FOREIGN KEY (owner_id) REFERENCES users(id) ON DELETE CASCADE;
ALTER TABLE quest_runs DROP CONSTRAINT quest_run_finished_check,
    ADD CONSTRAINT quest_run_finished_check CHECK (
        finished_at IS NULL OR (started_at IS NOT NULL AND finished_at >= started_at AND NOT join_enabled)
    );
UPDATE quest_attempts SET status='withdrawn',photo_expires_at=NULL WHERE status='expired';
DROP INDEX quest_attempts_photo_expiry_idx;
ALTER TABLE quest_attempts DROP CONSTRAINT quest_attempts_photo_deadline_check,
    DROP CONSTRAINT quest_attempts_status_check, DROP COLUMN photo_expires_at,
    ADD CONSTRAINT quest_attempts_status_check CHECK (
        status IN ('draft','pending_review','accepted','rejected','withdrawn')
    );
