-- +goose Up
CREATE TABLE cities (
    id          BIGSERIAL PRIMARY KEY,
    slug        VARCHAR(100) NOT NULL UNIQUE,
    name        VARCHAR(200) NOT NULL,
    center_lat  DOUBLE PRECISION NOT NULL,
    center_lon  DOUBLE PRECISION NOT NULL,
    timezone    VARCHAR(50) NOT NULL DEFAULT 'Europe/Minsk',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_cities_slug ON cities(slug);

-- +goose Down
DROP TABLE IF EXISTS cities;
