-- +goose Up
CREATE TABLE quests (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    city_id BIGINT NOT NULL REFERENCES cities(id),
    title TEXT NOT NULL DEFAULT '' CHECK (char_length(title) <= 120),
    description TEXT NOT NULL DEFAULT '' CHECK (char_length(description) <= 3000),
    status TEXT NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'published', 'archived')),
    revision BIGINT NOT NULL DEFAULT 1 CHECK (revision >= 1),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK (status <> 'published' OR char_length(title) >= 3)
);
CREATE INDEX quests_owner_idx ON quests(owner_id, id);

CREATE TABLE quest_steps (
    quest_id UUID NOT NULL REFERENCES quests(id) ON DELETE CASCADE,
    position INTEGER NOT NULL CHECK (position BETWEEN 1 AND 20),
    place_id BIGINT NOT NULL REFERENCES places(id),
    clue TEXT NOT NULL CHECK (char_length(clue) <= 1000),
    photo_task TEXT NOT NULL CHECK (char_length(photo_task) <= 1000),
    reveal_text TEXT NOT NULL CHECK (char_length(reveal_text) <= 3000),
    PRIMARY KEY (quest_id, position),
    UNIQUE (quest_id, place_id)
);

CREATE TABLE quest_runs (
    id UUID PRIMARY KEY,
    quest_id UUID NOT NULL REFERENCES quests(id) ON DELETE CASCADE,
    quest_revision BIGINT NOT NULL CHECK (quest_revision >= 1),
    title TEXT NOT NULL CHECK (char_length(title) BETWEEN 3 AND 120),
    join_code_hash BYTEA NOT NULL UNIQUE CHECK (octet_length(join_code_hash) = 32),
    join_enabled BOOLEAN NOT NULL DEFAULT TRUE,
    radius_m INTEGER NOT NULL DEFAULT 70 CHECK (radius_m > 0),
    accuracy_m INTEGER NOT NULL DEFAULT 30 CHECK (accuracy_m > 0),
    location_max_age_seconds INTEGER NOT NULL DEFAULT 120 CHECK (location_max_age_seconds > 0),
    location_future_skew_seconds INTEGER NOT NULL DEFAULT 30 CHECK (location_future_skew_seconds >= 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    started_at TIMESTAMPTZ
);
CREATE INDEX quest_runs_quest_idx ON quest_runs(quest_id, id);

CREATE TABLE quest_run_steps (
    run_id UUID NOT NULL REFERENCES quest_runs(id) ON DELETE CASCADE,
    id UUID NOT NULL UNIQUE DEFAULT gen_random_uuid(),
    position INTEGER NOT NULL CHECK (position BETWEEN 1 AND 20),
    place_id BIGINT NOT NULL REFERENCES places(id),
    place_name TEXT NOT NULL,
    address TEXT NOT NULL,
    lat DOUBLE PRECISION NOT NULL CHECK (lat BETWEEN -90 AND 90),
    lon DOUBLE PRECISION NOT NULL CHECK (lon BETWEEN -180 AND 180),
    links JSONB NOT NULL CHECK (jsonb_typeof(links) = 'array'),
    clue TEXT NOT NULL,
    photo_task TEXT NOT NULL,
    reveal_text TEXT NOT NULL,
    PRIMARY KEY (run_id, position),
    UNIQUE (run_id, place_id)
);

CREATE TABLE quest_participations (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    run_id UUID NOT NULL REFERENCES quest_runs(id) ON DELETE CASCADE,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    display_name TEXT NOT NULL CHECK (char_length(display_name) BETWEEN 1 AND 60),
    current_position INTEGER NOT NULL DEFAULT 1,
    joined_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (run_id, user_id),
    FOREIGN KEY (run_id, current_position) REFERENCES quest_run_steps(run_id, position) ON DELETE CASCADE
);
CREATE INDEX quest_participations_user_idx ON quest_participations(user_id, id);
CREATE INDEX quest_participations_run_idx ON quest_participations(run_id, id);

-- +goose Down
DROP TABLE quest_participations;
DROP TABLE quest_run_steps;
DROP TABLE quest_runs;
DROP TABLE quest_steps;
DROP TABLE quests;
