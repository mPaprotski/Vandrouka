# Вандроўка

[![CI](https://github.com/mPaprotski/Vandrouka/actions/workflows/ci.yml/badge.svg)](https://github.com/mPaprotski/Vandrouka/actions/workflows/ci.yml)

Мобильное приложение для пешеходных маршрутов по достопримечательностям Минска.

**Принято решение перейти на Mapbox API** для маршрутизации, матрицы,
подложки карты и геокодирования. [План миграции](docs/mapbox-migration.md)
заменяет исходное решение о локальной Valhalla. CLI уже использует Mapbox
Matrix/Directions; для реального запуска нужен `MAPBOX_ACCESS_TOKEN`.
Карта и геокодирование будут подключены на мобильном этапе.

Реализованы каркас Go API, PostgreSQL, миграции, импорт YAML, каталог мест,
клиент Mapbox Matrix/Directions, жадный планировщик, оптимизация 2-opt/swap
и до трёх вариантов маршрута по четырём профилям (CLI).
Добавлены настраиваемая оценка, метки с порогом 10%, объяснения маршрутов
и сравнение вариантов по времени, расстоянию, билетам и составу мест.
Мобильное приложение и HTTP-эндпоинты генерации маршрутов находятся в разработке.

## Запуск

Нужны Docker Compose и Go 1.23+.
Все команды Makefile запускаются из корня репозитория.

```bash
docker compose up -d --build postgres api
make migrate-up
make import
curl http://localhost:8080/health
curl http://localhost:8080/v1/cities/minsk/places
```

API — `localhost:8080`, PostgreSQL — `localhost:25432`.
`/health` проверяет также доступность PostgreSQL.
Для каталога Mapbox-токен не нужен. Для планировщика задайте
`MAPBOX_ACCESS_TOKEN` в окружении. Valhalla удалена из активного Compose после успешной проверки Mapbox.
Исторические инструкции и данные сохранены.

## Локальный backend

Конфигурация читается из переменных окружения; файл `.env` автоматически
не загружается. Можно использовать предоставленный пример:

```bash
docker compose up -d postgres
make migrate-up
cd backend
set -a
. ./.env.example
set +a
go run ./cmd/api
```

При запущенном API-контейнере для локального API выберите другой `HTTP_PORT`.
Настройки разработки из `.env.example` не предназначены для публичного сервера.

## Команды

```bash
make up             # сборка и запуск PostgreSQL + API
make down           # остановка с сохранением тома БД
make test           # Go-тесты; интеграционные требуют TEST_DATABASE_DSN
make lint           # go vet и проверка форматирования
make migrate-up     # применение миграций
make migrate-status # статус миграций
make migrate-down   # откат одной миграции
make migrate-reset  # полный откат миграций, удаляет таблицы приложения
make import         # импорт мест и проверка доступности источников
```

DSN для Makefile можно переопределить: `make migrate-status DSN='postgres://...'`.
Для CLI установите `POSTGRES_DSN` в окружении.

## Данные и импорт

Один YAML на место в `data/places/minsk/`. Формат, обязательные сведения,
проверки и правила актуализации описаны в [data/README.md](data/README.md).
Набор содержит 60 мест. Первые 15 проверены в [отчёте дня 4](docs/day-4.md),
ещё 20 добавлены при расширении каталога. Из пользовательского GPX Globus
добавлены 25 мест и сопоставлены 8 существующих; происхождение записей и
ограничения описаны в [отчёте импорта GPX](docs/globus-import.md).
Новые сведения из выгрузки 2015 года требуют актуальной проверки.

```bash
make import
# Без проверки сети, из backend с установленным POSTGRES_DSN:
go run ./cmd/import -check-links=false ../data/places/minsk
```

Импорт атомарен. Повторный запуск обновляет записи по `(city_id, slug)` и
сохраняет ID; связанная информация заменяется без накопления дублей.
Недоступные источники дают предупреждение, успешный импорт завершается кодом 0.

## Планировщик и CLI

Минимальный жадный алгоритм (пакет `internal/planner`) строит пешеходный маршрут
с соблюдением бюджета времени, темпа ходьбы, расписаний работы мест (`CanVisit`)
и бюджета на входные билеты:

```bash
cd backend
POSTGRES_DSN='postgres://postgres:postgres@localhost:25432/vandrouka?sslmode=disable' \
go run ./cmd/planner --budget 180 --categories museums,architecture
```

Подробное описание алгоритма, правил и параметров: [docs/planner.md](docs/planner.md).

CLI по умолчанию формирует до трёх разных вариантов, оптимизирует их через
2-opt/swap и уточняет итоговый путь через Mapbox Directions. Для одного
маршрута: `--variants=false --profile=budget`; другие профили: `best_match`,
`more_places`, `less_walking`. `--optimize=false` отключает оптимизацию.
`--geometry=false` отключает Directions и оставляет оценки Matrix.
Перед запуском экспортируйте `MAPBOX_ACCESS_TOKEN`; токен не сохраняйте в Git.
Коэффициенты оценки задаются через `PLANNER_WALK_PENALTY` (за км),
`PLANNER_COST_PENALTY` (за BYN) и `PLANNER_UNVERIFIED_PENALTY` (за место).
Метки сравнивают итоговые маршруты после Directions; при одном варианте
или преимуществе менее 10% метка не выдаётся.
Каждый вариант сопровождается краткой сводкой; последующие варианты
сравниваются с первым. Неизвестные цены и часы явно отмечаются.

Ограничения этапа 19 октября проверены тестами: время, расписания,
сумма билетов, уникальность мест и направленные переходы. При неизвестной
цене итог отражает только подтверждённую стоимость; фактические расходы
могут быть выше лимита. Результаты сверки приведены в документации планировщика.

## API

Контракт: [docs/openapi.yaml](docs/openapi.yaml).

| Метод | Путь | Назначение |
|---|---|---|
| GET | `/health` | API и PostgreSQL |
| GET | `/v1/cities` | Города |
| GET | `/v1/cities/{slug}/categories` | Категории и число активных мест |
| GET | `/v1/cities/{slug}/places?category=&q=` | Каталог, фильтр и поиск |
| GET | `/v1/places/{id}` | Карточка места |

HTTP-эндпоинты генерации и пересчёта маршрутов будут подключены на следующем этапе.

## Проверки

Для PostgreSQL-интеграции используйте отдельную тестовую БД:

```bash
docker compose exec -T postgres createdb -U postgres vandrouka_test
make migrate-up DSN='postgres://postgres:postgres@localhost:25432/vandrouka_test?sslmode=disable'
cd backend
TEST_DATABASE_DSN='postgres://postgres:postgres@localhost:25432/vandrouka_test?sslmode=disable' \
  go test ./... -race -count=1
```

Интеграционные тесты записывают и изменяют тестовые данные.
GitHub Actions запускает [CI](https://github.com/mPaprotski/Vandrouka/actions/workflows/ci.yml)
на каждый push и pull request; доступен также ручной запуск `workflow_dispatch`.
Версия Go берётся из `backend/go.mod`, зависимости и сборки кешируются.

CI проверяет зависимости, форматирование, `go vet`, сборку всех команд,
полный цикл миграций, тесты с PostgreSQL и детектором гонок, повторный импорт
с сохранением ID и проверкой числа активных мест. Отдельная проверка сверяет
выборку GPX, манифест происхождения данных и наличие карточек каталога.
Отдельный job проверяет ограничения и расписания планировщика без внешних
сервисов, собирает CLI и проверяет отклонение некорректного бюджета до подключения
к базе данных и Mapbox. HTTP-заглушки проверяют Mapbox без реальных токенов
и платных вызовов.
Внешние ссылки в CI не запрашиваются: CLI использует `-check-links=false`.
Отдельный job проверяет Compose и собирает Docker-образ API.
PostgreSQL создаётся отдельно для каждого запуска; секреты для этих проверок
не нужны. При новом push предыдущий запуск для той же ветки отменяется.
Проверки доступности ссылок тестируются локальным HTTP-сервером без внешней сети.

## Документация

- [Данные и формат YAML](data/README.md)
- [Результат дня 3](docs/day-3.md)
- [Результат дня 4 и открытые вопросы](docs/day-4.md)
- [Модель планировщика и правила расписаний](docs/planner.md)
- [Mapbox: интеграция и ограничения](docs/mapbox.md)
- [Решение о миграции](docs/mapbox-migration.md)
- [Прежняя настройка Valhalla](infra/README.md)
- [OpenAPI](docs/openapi.yaml)

## Лицензии данных

OpenStreetMap: © OpenStreetMap contributors (ODbL). Описания мест написаны для
проекта; ссылки на энциклопедические и официальные источники сохранены в YAML.
