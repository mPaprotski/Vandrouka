-- +goose Up
CREATE TABLE opening_hours (
    id           BIGSERIAL PRIMARY KEY,
    place_id     BIGINT NOT NULL REFERENCES places(id) ON DELETE CASCADE,
    weekday      SMALLINT NOT NULL CHECK (weekday BETWEEN 0 AND 6),
    open_time    TIME NOT NULL,
    close_time   TIME NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK (open_time < close_time)
);

CREATE INDEX idx_opening_hours_place_id ON opening_hours(place_id);
CREATE UNIQUE INDEX idx_opening_hours_place_weekday ON opening_hours(place_id, weekday, open_time, close_time);

-- +goose Down
DROP TABLE IF EXISTS opening_hours;
