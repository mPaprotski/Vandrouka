# Вандроўка

[![CI](https://github.com/mPaprotski/Vandrouka/actions/workflows/ci.yml/badge.svg)](https://github.com/mPaprotski/Vandrouka/actions/workflows/ci.yml)

Мобильное приложение для пешеходных маршрутов по достопримечательностям Минска.

Реализованы каркас Go API, PostgreSQL, миграции, импорт YAML, каталог мест и
клиент Valhalla. Планировщик и мобильное приложение ещё не реализованы.

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
Valhalla запускается отдельно после подготовки OSM: [инструкция](infra/README.md).

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
Набор содержит 15 мест; неизвестные часы и цены отмечены явно.

```bash
make import
# Без проверки сети, из backend с установленным POSTGRES_DSN:
go run ./cmd/import -check-links=false ../data/places/minsk
```

Импорт атомарен. Повторный запуск обновляет записи по `(city_id, slug)` и
сохраняет ID; связанная информация заменяется без накопления дублей.
Недоступные источники дают предупреждение, успешный импорт завершается кодом 0.

## API

Контракт: [docs/openapi.yaml](docs/openapi.yaml).

| Метод | Путь | Назначение |
|---|---|---|
| GET | `/health` | API и PostgreSQL |
| GET | `/v1/cities` | Города |
| GET | `/v1/cities/{slug}/categories` | Категории и число активных мест |
| GET | `/v1/cities/{slug}/places?category=&q=` | Каталог, фильтр и поиск |
| GET | `/v1/places/{id}` | Карточка места |

Генерация и пересчёт маршрутов будут добавлены после реализации планировщика.

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
полный цикл миграций, тесты с PostgreSQL и детектором гонок, повторный импорт.
Внешние ссылки в CI не запрашиваются: CLI использует `-check-links=false`.
Отдельный job проверяет Compose и собирает Docker-образ API.
PostgreSQL создаётся отдельно для каждого запуска; секреты для этих проверок
не нужны. При новом push предыдущий запуск для той же ветки отменяется.
Проверки доступности ссылок тестируются локальным HTTP-сервером без внешней сети.

## Документация

- [Данные и формат YAML](data/README.md)
- [Результат дня 3](docs/day-3.md)
- [Настройка Valhalla](infra/README.md)
- [OpenAPI](docs/openapi.yaml)

## Лицензии данных

OpenStreetMap: © OpenStreetMap contributors (ODbL). Описания мест написаны для
проекта; ссылки на энциклопедические и официальные источники сохранены в YAML.
