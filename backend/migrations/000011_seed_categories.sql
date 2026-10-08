-- +goose Up
INSERT INTO categories (slug, name) VALUES
    ('museums', 'Музеи'),
    ('monuments', 'Памятники и скульптуры'),
    ('streets', 'Значимые улицы и площади'),
    ('architecture', 'Архитектура'),
    ('parks', 'Парки и природа'),
    ('religious', 'Религиозные объекты')
ON CONFLICT (slug) DO NOTHING;

-- +goose Down
DELETE FROM categories WHERE slug IN ('museums', 'monuments', 'streets', 'architecture', 'parks', 'religious');
