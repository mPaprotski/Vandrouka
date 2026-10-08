# Локальная маршрутизация

Используется официальный scripted-образ Valhalla, закреплённый по digest.
Данные: OpenStreetMap / Geofabrik, атрибуция © OpenStreetMap contributors (ODbL).

Подготовка данных (Python 3, окружение можно создать в `/tmp`):

```bash
python3 -m venv /tmp/vandrouka-osmium
/tmp/vandrouka-osmium/bin/pip install osmium==4.3.1
mkdir -p infra/osm infra/valhalla
curl -fL --retry 2 -o infra/osm/belarus.osm.pbf \
  https://download.geofabrik.de/europe/belarus-latest.osm.pbf
/tmp/vandrouka-osmium/bin/python infra/extract_minsk.py \
  infra/osm/belarus.osm.pbf infra/valhalla/minsk.osm.pbf
docker compose --profile routing up -d valhalla
docker compose logs -f valhalla
```

Ограничивающий прямоугольник: 53.75–54.05 N, 27.3–27.85 E; дороги,
пересекающие его, сохраняются целиком. Извлечение сохраняет ссылки на узлы.
Маршруты транспорта и административные границы не включаются. Сборка
тайлов использует два потока. Генерируемые файлы исключены из Git.
Сервис запускается после завершения сборки тайлов; первый запуск занимает время.

```bash
curl -f http://localhost:8002/status
curl -f -H 'Content-Type: application/json' \
  -d '{"locations":[{"lat":53.903333,"lon":27.556111},{"lat":53.908333,"lon":27.555833}],"costing":"pedestrian"}' \
  http://localhost:8002/route
```

Пешеходные расчёты не используют транспорт, высоты и дорожные скорости.
Административная и временная базы отключены для этого раннего среза;
локальные часы посещения мест учитывает планировщик отдельно.

Источники инструкции:
- https://github.com/valhalla/valhalla/blob/master/docker/README.md
- https://valhalla.github.io/valhalla/api/matrix/
- https://docs.osmcode.org/pyosmium/latest/cookbooks/Filter-Data-By-Geometry/
- https://download.geofabrik.de/europe/belarus.html
