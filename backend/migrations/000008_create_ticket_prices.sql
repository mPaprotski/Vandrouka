-- +goose Up
CREATE TABLE ticket_prices (
    id          BIGSERIAL PRIMARY KEY,
    place_id    BIGINT NOT NULL REFERENCES places(id) ON DELETE CASCADE,
    kind        VARCHAR(50) NOT NULL,
    amount      NUMERIC(10, 2) NOT NULL CHECK (amount >= 0),
    currency    CHAR(3) NOT NULL DEFAULT 'BYN',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_ticket_prices_place_id ON ticket_prices(place_id);

-- +goose Down
DROP TABLE IF EXISTS ticket_prices;
