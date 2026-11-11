# Схема базы данных

Схема соответствует миграциям `000001`–`000016` в `backend/migrations`.
Источник истины — SQL миграций. Диаграмма показывает связи и ключевые поля.

```mermaid
erDiagram
    cities ||--o{ places : contains
    places ||--o{ place_categories : categorized
    categories ||--o{ place_categories : assigned
    places ||--o{ opening_hours : schedule
    places ||--o{ opening_exceptions : exceptions
    places ||--o{ ticket_prices : tickets
    places ||--o{ place_links : sources
    users ||--o{ session_families : owns
    users ||--o{ sessions : authenticates
    session_families o|--o{ sessions : rotates
    users ||--o{ visit_photos : owns
    places ||--o{ visit_photos : photographed
    users ||--o{ visits : records
    places ||--o{ visits : visited
    visit_photos ||--o{ visits : used
    users ||--o{ xp_transactions : earns
    places ||--o{ xp_transactions : rewards
    visits ||--o| xp_transactions : grants
    users ||--o{ user_achievements : unlocks

    cities {
        bigint id PK
        varchar slug UK
        varchar name
        varchar timezone
        double center_lat
        double center_lon
    }
    places {
        bigint id PK
        bigint city_id FK
        varchar slug
        varchar name
        double lat
        double lon
        int visit_minutes
        boolean is_active
    }
    categories {
        bigint id PK
        varchar slug UK
    }
    place_categories {
        bigint place_id PK,FK
        bigint category_id PK,FK
    }
    opening_hours {
        bigint id PK
        bigint place_id FK
        smallint weekday
        time open_time
        time close_time
    }
    opening_exceptions {
        bigint id PK
        bigint place_id FK
        date date
        boolean is_closed
        time open_time
        time close_time
    }
    ticket_prices {
        bigint id PK
        bigint place_id FK
        varchar kind
        numeric amount
        char currency
    }
    place_links {
        bigint id PK
        bigint place_id FK
    }
    users {
        bigint id PK
        varchar email UK
        text password_hash
        timestamptz created_at
    }
    session_families {
        uuid id PK
        bigint user_id FK
        timestamptz expires_at
        timestamptz revoked_at
    }
    sessions {
        bigint id PK
        bigint user_id FK
        uuid family_id FK
        bytea access_token_hash UK
        bytea refresh_token_hash UK
        timestamptz refresh_used_at
        timestamptz expires_at
        timestamptz revoked_at
    }
    visit_photos {
        uuid id PK
        bigint user_id FK
        bigint place_id FK
        text object_key UK
        text status
        timestamptz upload_expires_at
        timestamptz deleted_at
    }
    visits {
        bigint id PK
        bigint user_id FK
        bigint place_id FK
        uuid photo_id FK
        uuid idempotency_key
        bytea request_hash
        text status
        text rejection_code
        timestamptz confirmed_at
        timestamptz deleted_at
    }
    xp_transactions {
        bigint id PK
        bigint user_id FK
        bigint place_id FK
        bigint visit_id FK,UK
        int amount
    }
    user_achievements {
        bigint user_id PK,FK
        text code PK
        timestamptz earned_at
    }
    media_deletions {
        text object_key PK
        timestamptz not_before
        int attempts
    }
```

## Каталог

`places` уникален по `(city_id, slug)`. Повторный импорт сохраняет ID места.
Связующие категории используют составной первичный ключ. Расписание допускает
несколько интервалов в один день, исключение уникально по `(place_id, date)`.
Цены представлены `NUMERIC(10,2)` в BYN, планировщик сравнивает целые копейки.
Часовой пояс берётся из города.

## Сессии и посещения

Пароли хранятся как Argon2id-хеши. В таблице `sessions` находятся SHA256-хеши
access/refresh токенов. `session_families` задаёт абсолютный срок refresh-family
и её отзыв. `family_id` допускает NULL для сессий, созданных до миграции 15.

Составной внешний ключ `(photo_id, user_id, place_id)` связывает посещение
с фотографией того же владельца и места. Триггер блокирует и проверяет готовность
фото при подтверждении. Одно фото используется максимум одним подтверждённым
посещением, отклонённые попытки могут ссылаться на него повторно.

Ключ `(user_id, idempotency_key)` уникален. `request_hash` позволяет отличить
повтор того же запроса от изменённого тела. GPS-координаты используются при
проверке запроса. Сохраняются время фиксации, вычисленное расстояние и точность.

## XP, достижения и коллекция

`xp_transactions` уникален по `(user_id, place_id)` и `visit_id`. Составной
внешний ключ включает владельца, место и статус `confirmed`. Сумма одной
награды ограничена CHECK `amount = 100`. Подтверждение и первая награда
записываются в одной транзакции.

Триггер миграции 16 выдаёт достижения за 1, 5 и 10 открытых мест.
`user_achievements` сохраняет дату первого получения. Уровень вычисляется
из суммы XP, коллекция из первых наград за её три места. Эти значения
вычисляются при чтении API.

## Удаление и файлы

`visits.deleted_at` скрывает запись из истории, `visit_photos.deleted_at`
закрывает доступ к фото. Журнал XP и запись идемпотентности сохраняются.
Удаление аккаунта каскадно удаляет его сессии, фото, посещения, XP и достижения.

Файлы находятся в приватном S3/MinIO/R2, таблица `visit_photos` содержит их
метаданные. `media_deletions` сохраняет задания физического удаления по
`object_key`, независимо от срока жизни аккаунта и метаданных фото.
В этой таблице отсутствует внешний ключ владельца. Worker учитывает
`not_before` и повторяет удаление при сбоях хранилища.

Служебная `goose_db_version` ведётся Goose. `schema_version` создана начальной
миграцией и содержит историческую запись версии 1. Текущую применённую версию
проверять через `make migrate-status`.
