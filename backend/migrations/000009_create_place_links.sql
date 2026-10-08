-- +goose Up
CREATE TABLE place_links (
    id          BIGSERIAL PRIMARY KEY,
    place_id    BIGINT NOT NULL REFERENCES places(id) ON DELETE CASCADE,
    url         VARCHAR(500) NOT NULL,
    kind        VARCHAR(50) NOT NULL,
    title       VARCHAR(300),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_place_links_place_id ON place_links(place_id);

-- +goose Down
DROP TABLE IF EXISTS place_links;
