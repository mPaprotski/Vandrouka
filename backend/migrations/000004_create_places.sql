-- +goose Up
CREATE TABLE places (
    id              BIGSERIAL PRIMARY KEY,
    city_id         BIGINT NOT NULL REFERENCES cities(id) ON DELETE CASCADE,
    slug            VARCHAR(200) NOT NULL,
    name            VARCHAR(300) NOT NULL,
    summary         TEXT,
    lat             DOUBLE PRECISION NOT NULL,
    lon             DOUBLE PRECISION NOT NULL,
    importance      SMALLINT NOT NULL CHECK (importance BETWEEN 1 AND 5),
    must_see        BOOLEAN NOT NULL DEFAULT FALSE,
    visit_minutes   INTEGER NOT NULL CHECK (visit_minutes > 0),
    hours_known     BOOLEAN NOT NULL DEFAULT FALSE,
    price_known     BOOLEAN NOT NULL DEFAULT FALSE,
    verified_at     DATE NOT NULL,
    is_active       BOOLEAN NOT NULL DEFAULT TRUE,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (city_id, slug)
);

CREATE INDEX idx_places_city_id ON places(city_id);
CREATE INDEX idx_places_slug ON places(slug);
CREATE INDEX idx_places_coords ON places(lat, lon);
CREATE INDEX idx_places_is_active ON places(is_active);

-- +goose Down
DROP TABLE IF EXISTS places;
