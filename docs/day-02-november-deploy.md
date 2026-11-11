# 2 ноября — внешний demo backend и P0 E2E

Статус на 11 ноября 2026 года — Neon подключён, миграции и каталог загружены.
Render ожидает создания сервиса. Приватный бакет Backblaze B2 создан,
серверные ключи и внешнее фотохранилище проверены.
Пользователь сообщил об успешном основном сценарии на телефоне. Проверки
сбоев/двух аккаунтов и внешний сценарий после развёртывания остаются отдельными.
Выбранный пользователем вариант — Render Free + Neon Free + Backblaze B2.

### Выполненное подключение Neon

Прямое TLS-подключение сохранено в игнорируемом `backend/.env.neon.local`
с правами 0600. Проверочный Docker image этапа 12 применил все миграции до 16
и импортировал внешний каталог с `-check-links=false`.
Отдельный SQL-запрос подтвердил версию 16, 60 активных мест Минска и все
3 места первой коллекции. Используется созданная пользователем база `neondb`
в ветке `production`, PostgreSQL 18. Данные локальных аккаунтов остаются
в локальной базе. Следующий шаг — сервис Render.

Expo CLI авторизован, приложение связано с проектом
`@mpaprotskis-team/vandrouka`, ID `4c0b449b-fe92-4f4a-abaa-dbfe1a955ba8`.
Самостоятельный APK и внешняя приёмка ожидают завершения настройки backend.

## 1. Создать аккаунты и ресурсы

### Neon — PostgreSQL

1. Открыть https://console.neon.tech и создать Free project `vandrouka-demo`.
2. Выбрать европейский регион, по возможности близкий к Render.
3. Создать отдельную базу demo. В Connect скопировать **direct**, не pooled DSN
   с TLS (`sslmode=require` или строже). Миграции Goose используют session lock,
   поэтому direct-подключение предпочтительно для первоначального запуска.
4. Сохранить DSN локально в защищённом env-файле, не в Git и не в чате.
   Не копировать локальную БД с пользовательскими аккаунтами: начнём с чистой demo.

### Backblaze B2 — приватные фотографии

1. Зарегистрироваться на https://www.backblaze.com/sign-up/s3.
2. В B2 Cloud Storage → Buckets создать `vandrouka-visit-photos` с доступом
   **Private**, Object Lock **Disable**. При занятом имени выбрать своё уникальное.
3. Скопировать Endpoint. Созданный пользователем бакет находится на
   `https://s3.eu-central-003.backblazeb2.com`.
4. В Application Keys → Add a New Application Key создать стандартный ключ
   `vandrouka-api`, ограниченный этим бакетом, с доступом Read and Write.
   Для совместимости S3 включить Allow listing all bucket names.
5. Сохранить keyID в `MEDIA_S3_ACCESS_KEY`, applicationKey в
   `MEDIA_S3_SECRET_KEY` файла `backend/.env.b2.local`. Файл игнорируется Git,
   имеет права 0600. Ключ применяется только сервером.
6. В окружении backend задать следующие значения.

```dotenv
MEDIA_S3_PROVIDER=b2
MEDIA_S3_ENDPOINT=https://s3.eu-central-003.backblazeb2.com
MEDIA_S3_BUCKET=vandrouka-visit-photos
MEDIA_S3_ACCESS_KEY=<keyID>
MEDIA_S3_SECRET_KEY=<applicationKey>
```

Регион подписи определяется из Endpoint. Бакет создаётся заранее.
При инициализации сервер читает подписанный Get Bucket ACL и допускает
только ACL владельца с FULL_CONTROL. Публичный, недоступный или некорректный
ACL приводит к `media_unavailable`.

B2 сохраняет версии файлов. Очередь удаления перечисляет версии только точного
object_key, затем удаляет их вместе с hide markers с указанием versionId.
Ошибки перечисления/удаления сохраняют задание для повторной обработки.
Object Lock должен позволять предусмотренное пользователем удаление файлов.

Тариф B2 включает первые 10 ГБ хранения, исходящий трафик имеет отдельную квоту.
Текущие условия и лимиты проверять в панели и на странице тарифов провайдера.

### Render — Go API

1. Открыть https://dashboard.render.com, подключить GitHub.
2. Изменения должны попасть в доступную Render ветку. Сейчас они локальные;
   коммит и push выполняются только по отдельной просьбе пользователя.
3. New → Blueprint → выбрать репозиторий с `render.yaml`.
4. Проверить **Free** plan. Blueprint не создаёт платную БД или диск.
5. Заполнить секреты POSTGRES_DSN, MEDIA_S3_ENDPOINT, MEDIA_S3_ACCESS_KEY,
   MEDIA_S3_SECRET_KEY. Бакет и provider уже заданы в Blueprint.
6. MAPBOX_ACCESS_TOKEN для основного игрового цикла не нужен: маршрутизация
   без него недоступна, но каталог/фото/visits/XP работают. Не добавлять его ради проверки.
7. Создать сервис. Автодеплой отключён; выкладка ручная. HTTPS-адрес выдаст Render,
   покупать домен для теста не обязательно. HTTP_PORT=10000 задан явно.

## 2. Применить миграции и импортировать внешний каталог

Free Render не предоставляет shell/pre-deploy job для нашего сценария.
Инициализируем **новую пустую demo-базу** локальной утилитой из Docker image.
Создать вне Git env-файл `/tmp/vandrouka-demo.env` с одной переменной
`POSTGRES_DSN=...` (direct TLS DSN Neon), права `chmod 600`.
Не использовать backend/.env: он указывает на локальную БД.

Из корня репозитория:

```bash
docker build -t vandrouka-demo ./backend
docker run --rm --env-file /tmp/vandrouka-demo.env --entrypoint /migrate vandrouka-demo up
docker run --rm --env-file /tmp/vandrouka-demo.env \
  --mount type=bind,src="$(realpath ../data)",dst=/app/data,readonly \
  --entrypoint /import vandrouka-demo -city minsk -check-links=false /app/data/places/minsk
```

Данные остаются вне Git. Docker image содержит API, migrate/import и миграции,
но не внешний каталог, секреты или личные фотографии. API отдаёт каталог из БД;
отсутствие DATA_DIR в Render даёт предупреждение startup loader, не пустую БД.
После миграции/импорта выполнить Manual Deploy/Restart сервиса.
Миграции не запускаются автоматически при каждом рестарте API.

## 3. Проверить внешний сервис

```bash
python3 scripts/check_public_api.py https://YOUR-SERVICE.onrender.com
```

Проверка read-only: HTTPS без redirect, health, непустой каталог,
401/no-store без токена на progress/history. Она не вызывает Mapbox, не создаёт
аккаунты. Приватность B2 дополнительно проверяется ACL и запросом фото.
Не запускать verify_game_loop.mjs
против production: он намеренно ограничен loopback и использует локальный SQL.

В Backblaze проверить Type Private. Создать тестовое фото
через приложение и убедиться, что чтение без сессии и другим пользователем
запрещено. Ключи B2 и DSN хранятся в серверном окружении.

## 4. Подключить телефон

В mobile/.env.local заменить только EXPO_PUBLIC_API_URL на HTTPS origin Render.
Перезапустить Expo. При изменении origin старые локальные сессии не переиспользуются;
зарегистрировать новый demo-аккаунт. Mapbox pk/style не менять.

Проверить через мобильный интернет (Metro при этом тоже должен быть доступен
телефону: отдельный Expo tunnel или самостоятельная сборка; внешний API сам по
себе не публикует JS-пакет). Пройти фото → upload → confirm в радиусе 70 м,
accuracy ≤30 м → +100 XP → повтор 0 → history/photo/map/profile → restart.
Отдельно проверить offline/retry и второй аккаунт, доступ к файлам и физическое удаление версий B2.

Free Render засыпает после 15 минут простоя. Первое пробуждение может превышать
10-секундный клиентский таймаут: заранее открыть /health, дождаться ответа,
затем повторить действие. Не подтверждать неизвестный исход новым ключом.
Neon и B2 имеют квоты. Это demo-окружение; независимые backup/restore и полноценная
публичная эксплуатация остаются следующими задачами, бесплатный тариф не заменяет backup.

## Критерий завершения

Зафиксировать реальный HTTPS origin, успешный внешний read-only check,
результаты телефона и приватности B2/двух аккаунтов; составить баг-лист.
До этого этап 2 ноября **не считается закрытым**.

Источники:
- https://render.com/docs/blueprint-spec
- https://render.com/docs/free
- https://neon.com/docs/connect/connect-from-any-app
- https://www.backblaze.com/docs/cloud-storage-s3-compatible-api
- https://www.backblaze.com/apidocs/s3-get-bucket-acl
- https://www.backblaze.com/apidocs/s3-delete-object
- https://www.backblaze.com/cloud-storage/pricing

## Локальный отчёт подготовки

Go `-race` (media/config/http), `go vet`, gofmt, git diff --check прошли.
Blueprint прошёл локальную структурную проверку Free/Docker/секретов;
валидация Render API и реальное создание сервиса ещё не выполнялись.
Read-only check скомпилирован Python, но не запускался против вымышленного адреса.
Локальный E2E прошёл после запуска остановленных контейнеров: два аккаунта,
потеря ответа upload/confirm, replay, +100/0 XP, история, карта, приватность фото.
Он использует локальный MinIO. Реальный B2 проверяется отдельно после получения ключей.
Docker image `vandrouka-demo` успешно собран; внутри проверены /api, /migrate,
/import, миграции и timezone Europe/Minsk. Утилиты также компилируются Go локально.

### Подготовка Backblaze B2, 11 ноября

Добавлен режим `MEDIA_S3_PROVIDER=b2`, регион подписи определяется из S3
endpoint. Blueprint Render настроен на B2. Приватность проверяется по Bucket ACL,
удаление очищает версии и hide markers только указанного object_key.

Полный Go-прогон с race и отдельной локальной тестовой PostgreSQL, go vet,
OpenAPI, gofmt и проверка diff пройдены. TLS-заглушки проверяют регион подписи,
приватные/публичные/некорректные ACL, upload/read, пагинацию версий,
сохранение соседних ключей, повторное удаление и retry после частичного сбоя.
Проверочный Docker image `vandrouka-api:b2-check` собран успешно.

Опциональный `TestB2PrivateIntegration` проверяет реальный бакет с временным
JPEG, двумя версиями, анонимным чтением и полной очисткой. Он включается
переменными `TEST_MEDIA_B2_ENDPOINT`, `TEST_MEDIA_B2_BUCKET`,
`TEST_MEDIA_B2_ACCESS_KEY`, `TEST_MEDIA_B2_SECRET_KEY`.
После сохранения ключей `TestB2PrivateIntegration` пройден на реальном бакете
11 ноября. Подтверждены приватный ACL, две загрузки JPEG под одним временным
ключом, чтение тех же байтов, отказ анонимному GET, физическое удаление всех
версий и отсутствие оставшихся версий тестового объекта. Повторная очистка
успешна. Следующий шаг — передать серверные переменные в Render и проверить API.
