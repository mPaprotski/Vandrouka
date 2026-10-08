-- +goose Up
CREATE TABLE place_categories (
    place_id     BIGINT NOT NULL REFERENCES places(id) ON DELETE CASCADE,
    category_id  BIGINT NOT NULL REFERENCES categories(id) ON DELETE CASCADE,
    PRIMARY KEY (place_id, category_id)
);

CREATE INDEX idx_place_categories_category_id ON place_categories(category_id);

-- +goose Down
DROP TABLE IF EXISTS place_categories;
