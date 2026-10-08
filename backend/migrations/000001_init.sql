-- +goose Up
-- Initial migration - creates schema_version table for tracking
CREATE TABLE IF NOT EXISTS schema_version (
    version INT PRIMARY KEY,
    applied_at TIMESTAMP DEFAULT NOW()
);
INSERT INTO schema_version (version) VALUES (1) ON CONFLICT DO NOTHING;

-- +goose Down
DROP TABLE IF EXISTS schema_version;
