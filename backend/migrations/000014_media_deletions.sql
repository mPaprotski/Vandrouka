-- +goose Up
-- No owner FK: deletion jobs must survive a photo/account metadata deletion.
CREATE TABLE media_deletions (
    object_key TEXT PRIMARY KEY,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    not_before TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    attempts INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0)
);
CREATE INDEX media_deletions_due_idx ON media_deletions(not_before, created_at);
-- +goose StatementBegin
CREATE FUNCTION queue_photo_deletion() RETURNS trigger LANGUAGE plpgsql AS $$
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
CREATE TRIGGER photo_object_deleted AFTER DELETE ON visit_photos
    FOR EACH ROW EXECUTE FUNCTION queue_photo_deletion();
CREATE TRIGGER photo_object_hidden AFTER UPDATE OF deleted_at ON visit_photos
    FOR EACH ROW EXECUTE FUNCTION queue_photo_deletion();

-- +goose Down
DROP TRIGGER photo_object_hidden ON visit_photos;
DROP TRIGGER photo_object_deleted ON visit_photos;
DROP FUNCTION queue_photo_deletion();
DROP TABLE media_deletions;
