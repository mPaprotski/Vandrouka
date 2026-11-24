-- +goose Up
ALTER TABLE quest_runs ADD COLUMN finished_at TIMESTAMPTZ,
    ADD CONSTRAINT quest_run_finished_check CHECK (
        finished_at IS NULL OR (started_at IS NOT NULL AND finished_at >= started_at AND NOT join_enabled)
    );

-- +goose Down
ALTER TABLE quest_runs DROP CONSTRAINT quest_run_finished_check, DROP COLUMN finished_at;
