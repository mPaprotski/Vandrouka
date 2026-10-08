-- +goose Up
INSERT INTO cities (slug, name, center_lat, center_lon, timezone) VALUES
    ('minsk', 'Минск', 53.9045, 27.5615, 'Europe/Minsk')
ON CONFLICT (slug) DO NOTHING;

-- +goose Down
DELETE FROM cities WHERE slug = 'minsk';
