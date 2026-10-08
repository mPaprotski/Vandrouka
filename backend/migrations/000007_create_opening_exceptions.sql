-- +goose Up
CREATE TABLE opening_exceptions (
    id           BIGSERIAL PRIMARY KEY,
    place_id     BIGINT NOT NULL REFERENCES places(id) ON DELETE CASCADE,
    date         DATE NOT NULL,
    is_closed    BOOLEAN NOT NULL DEFAULT FALSE,
    open_time    TIME,
    close_time   TIME,
    note         VARCHAR(500),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK (
        (is_closed = TRUE AND open_time IS NULL AND close_time IS NULL) OR
        (is_closed = FALSE AND open_time IS NOT NULL AND close_time IS NOT NULL AND open_time < close_time)
    )
);

CREATE INDEX idx_opening_exceptions_place_id ON opening_exceptions(place_id);
CREATE INDEX idx_opening_exceptions_date ON opening_exceptions(date);
CREATE UNIQUE INDEX idx_opening_exceptions_place_date ON opening_exceptions(place_id, date);

-- +goose Down
DROP TABLE IF EXISTS opening_exceptions;
