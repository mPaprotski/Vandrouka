# 29 октября — обработка ошибок и укрепление API

## Пункт 1: единая обработка ошибок — выполнен

Каталог, generate и recalculate используют общий writeFailure. Сохраняются
контрактные статусы: 400 invalid_request, 404 not_found, 500 internal;
route-сбои провайдера и отмена/таймаут — 503 routing_unavailable;
нарушения ограничений пересчёта — 422 constraints_violation.
Для generate такие неожиданные ошибки не становятся новым статусом 422.
Ошибка БД/контекста каталога остаётся internal, как в его OpenAPI-контракте.
Валидация сохраняет безопасные подробные сообщения без внутренних причин.

Исправлена неодинаковая классификация отсутствующих объектов в generate:
ошибка репозитория ErrNotFound также получает 404. Единая обработка применена
и к ошибкам сериализации. Тексты внутренних ошибок/SQL/URL провайдера не
выдаются клиенту и не записываются как сырые причины в журнал; для 5xx
сохраняются безопасные scope, code и status. Request ID/logging — следующий этап.

Пустая генерация по-прежнему 200 с variants: []; пустой пересчёт после удаления
также успешен. no_feasible_route не вводится. Health сохраняет свой отдельный
документированный ответ status: ok/unavailable.

Тесты проверяют ошибки с обёртками, статус/код/JSON-заголовок, различие scope,
отсутствие утечки секретов в ответ и логи. Пройдены HTTP-тесты с PostgreSQL
и -race, go vet, форматирование и OpenAPI. Реальный Mapbox не вызывался.
Коммит: Unify API error handling and contract.

## Пункт 2: JSON при таймаутах и паниках — выполнен

Стандартные chi Recoverer/Timeout заменены CatalogResponses/RouteResponses для
JSON-эндпоинтов. Ответ буферизуется до успешного завершения обработчика. Паника
после частичной записи не выдаёт 200 или обрывок ответа: клиент получает 500
internal; recovered значение и stack trace не раскрываются. Сервер продолжает
обслуживать последующие запросы. Health сохраняет status: unavailable и 503.

Каталог ограничен 10 секундами (500 internal по его контракту), генерация и
пересчёт — 180 секундами (503 routing_unavailable). Истёкший/отменённый родительский
контекст не запускает обработчик. При дедлайне отменяется контекст зависимостей,
возвращается JSON без ожидания некорректно блокирующего обработчика; поздние
Write возвращают http.ErrHandlerTimeout, заголовки/частичный ответ отбрасываются.
Обработчики и зависимости должны учитывать context: Go не завершает произвольную
горутину принудительно. Эти эндпоинты не используют потоковую передачу.

Тесты проверяют дедлайн, отмену, поздние заголовки/записи, панику после частичного
успеха, отсутствие утечки причины в ответ/лог, успешный следующий запрос,
сохранение успешного JSON/заголовков, отдельный envelope health.
Полный go test ./... -race выполнен с PostgreSQL; vet, build, форматирование
и OpenAPI пройдены. Mapbox на данном шаге не вызывался.
Коммит: Return consistent JSON for API recovery and timeouts.

## Пункт 3: структурированные логи и Request ID — выполнен

RequestLogging назначает случайный серверный ID (32 hex), возвращает
X-Request-ID и пишет одну итоговую JSON-запись HTTP request: request_id,
method, безопасный шаблон path, status, duration_ms, stage. Входящий ID
не используется. Произвольные сегменты неизвестного URL заменяются /unmatched,
ID/slug — шаблонами; query/body/координаты/IP/token в журнал не включаются.

Общий requestlog-контекст передаёт этап обработки без HTTP-зависимости сервиса:
validation, catalog, matrix, directions, planner, serialization. Доступ к этапу
защищён mutex, включая таймаут с продолжающим завершение обработчиком.
5xx и recovery содержат тот же request_id; буфер ответа сохраняет его.
Middleware находится снаружи защиты ответов и видит итоговые 500/503.
OpenAPI описывает X-Request-ID успешных и ошибочных ответов, README — диагностику.

Тесты проверяют корреляцию записей и заголовка, уникальность/игнорирование
клиентского ID, status/duration/stage, безопасные шаблоны, отсутствие секретов,
паники и таймауты. Пройдены тесты с -race, vet, форматирование и OpenAPI.
Коммит: Add request logging and correlation IDs.

## Пункт 4: частота и одновременные расчёты — выполнен

Один RouteLimiter используется generate/recalculate: token bucket на IP,
общий semaphore без очереди, ограниченная карта клиентов и удаление неактивных
записей. Настройки: 6 запросов/мин, burst=2, concurrent=2, clients=4096,
TTL=600 секунд. Некорректная конфигурация отклоняется до запуска HTTP-сервера.
Переменные проброшены в Compose/.env.example и описаны в README.

Идентичность определяется только RemoteAddr, IPv4-mapped IPv6 нормализуется;
X-Forwarded-For не принимается. Неизвестный адрес делит единый unknown bucket.
Лимитер внутри RouteResponses: слот удерживается до фактического завершения
работы, а не только до HTTP-таймаута; освобождается defer при ошибке/панике.
При перегрузке 429 rate_limited/Retry-After, ошибка отражается в OpenAPI.
Заполненная карта отказывает новым клиентам, не растёт без ограничения.
Счётчики локальны одному процессу; Redis/распределённые лимиты не добавлены.

Тесты проверяют refill с фиксированными часами, Retry-After, общий лимит двух
эндпоинтов, доверие только IP соединения, предел/очистку карты, конкурентную
нагрузку, ошибки, панику, отмену и слот после таймаута с блокирующим обработчиком.
Пройдены race-тесты, vet, форматирование и OpenAPI.
Коммит: Limit route request rate and concurrent calculations.

## Пункт 5: сбои зависимостей — выполнен

HTTP-тесты используют настоящий MapboxClient и routes.Service с локальным
httptest-сервером: Matrix 401/403/429/500, отсутствующий токен, повреждённый JSON,
зависание Matrix; Directions 503/повреждённые legs/зависание после успешной Matrix.
Проверены 503 routing_unavailable, отсутствие утечки токена и отсутствие
автоматических повторов/продолжения расчёта после сбоя.

Отмена клиентского контекста прерывает реальный исходящий HTTP-вызов к локальному
провайдеру; успешный пустой результат вместо ошибки не выдаётся. Отдельный
recalculate-тест подтверждает 503 после уже проверенного исходного маршрута,
если Directions добавления недоступен: частичный variant/diff не возвращается.

Интеграционный тест открывает настоящую тестовую PostgreSQL и закрывает пул,
после чего каталог и generate возвращают 500 internal без вызовов маршрутизации.
Это проверка недоступной зависимости в приложении, без остановки общей БД
и влияния на другие проекты. Без TEST_DATABASE_DSN этот тест пропускается;
остальные сценарии не требуют PostgreSQL, токена или внешней сети.

Пройдены полный go test ./... -race с PostgreSQL, vet, форматирование и OpenAPI.
Производственное поведение менять не потребовалось. Реальная квота Mapbox
не расходуется этими тестами.
Коммит: Test API dependency failures and cancellation.

## Пункт 6: полные HTTP-проверки — выполнен

Тесты охватывают все требуемые сценарии:

- **Единый JSON-формат ошибок** (`errors_test.go`): `TestSharedErrorMapping` проверяет
  400/404/422/503/500 для трёх scope; `TestErrorLogsDoNotExposeCauses` — отсутствие
  SQL, токена и стека в ответе и логах.
- **Request ID** (`request_logging_test.go`): `TestRequestLoggingCorrelationAndPrivacy`
  проверяет 32-символьный ID в заголовке и во всех записях журнала, игнорирование
  клиентского ID, безопасные шаблоны пути, статус/duration/stage в итоговой записи.
- **Recovery после паники** (`response_guard_test.go`): `TestResponseGuardPanicDoesNotLeakPartialResponseOrCause`
  — частичная запись не попадает в ответ, panic-строка не раскрывается, следующий
  запрос обслуживается нормально.
- **Таймауты и отмена** (`response_guard_test.go`, `request_logging_test.go`):
  `TestResponseGuardTimeoutAndLateWrites`, `TestResponseGuardDoesNotStartCanceledRequest`,
  `TestRequestLoggingPanicAndTimeout` — дедлайн даёт 500/503 с корректным JSON,
  поздние Write возвращают http.ErrHandlerTimeout.
- **429 и Retry-After** (`route_limits_test.go`): `TestRouteLimitRefillAndBoundedClients`,
  `TestRouteLimitUsesPeerIPAndSharesRateAcrossEndpoints` — токен-бакет, общий лимит
  обоих эндпоинтов, Retry-After с корректным числом секунд.
- **Ограничение одновременных расчётов** (`route_limits_test.go`): `TestRouteConcurrencyAndSlotRelease`
  — 40 параллельных запросов, максимум двух одновременно; `TestSlotRemainsUntilTimedOutWorkerFinishes`
  — слот удерживается до фактического завершения горутины после HTTP-таймаута.
- **Освобождение ресурсов после ошибки** (`middleware_integration_test.go`,
  `route_limits_test.go`): `TestComposedMiddlewareFailureReleasesCapacity` (error/panic/timeout),
  `TestRouteLimitPanicAndCancellation` — после каждого исхода слот свободен
  и следующий запрос проходит.
- **Отсутствие утечки токена** (`dependency_failures_test.go`, `errors_test.go`,
  `request_logging_test.go`): все тесты проверяют отсутствие строк `private-token`,
  `access_token`, `SELECT` в теле ответа и журнале.
- **Интеграционный тест с реальным сервером** (`middleware_integration_test.go`):
  `TestMiddlewareOverloadKeepsCatalogAvailable` — 12 параллельных запросов при занятом
  слоте дают 429, catalog и health возвращают 200 без ограничений, журнал содержит
  ровно N+4 записей `HTTP request`, каждый с request_id и duration_ms.

Все тесты — внутри пакета без внешней сети, PostgreSQL-зависимый тест пропускается
без `TEST_DATABASE_DSN`. Прогоны с `-race`, `go vet`, `gofmt` и OpenAPI-валидатором
чисты.
Коммит: Test API middleware and overload handling.

## Пункт 7: документация и финальная проверка — выполнен

**OpenAPI** (`docs/openapi.yaml`): содержит 429 `RateLimited` с `Retry-After` для
generate и recalculate, `X-Request-ID` в заголовках всех успешных и ошибочных ответов,
схемы `APIError`, `ConstraintsViolation` и `RoutingUnavailable`. Скрипт
`scripts/check_openapi.py` без аргументов валидирует spec, 12 inline-примеров,
7 реализованных эндпоинтов и границы запросов (null vs 0 для ticket_budget_minor).

**README** (`README.md`): раздел «Ограничения запросов маршрутов» описывает
5 переменных окружения (`ROUTE_REQUESTS_PER_MINUTE`, `ROUTE_BURST`,
`ROUTE_MAX_CONCURRENT`, `ROUTE_MAX_CLIENTS`, `ROUTE_CLIENT_TTL_SECONDS`)
с примечаниями о поведении при перегрузке, Redis и X-Forwarded-For.
Раздел о `X-Request-ID` описывает диагностику через `docker compose logs api`.
Поля конфигурации проброшены в `config.go` и инициализацию `cmd/api/main.go`.

**Финальная проверка:**

| Проверка | Результат |
|---|---|
| `go test ./... -race -count=1` | ✅ все пакеты ok |
| `go vet ./...` | ✅ чисто |
| `gofmt -l .` | ✅ нет расхождений |
| `python3 scripts/check_openapi.py` | ✅ valid; 12 examples; 7 endpoints |
| `go build ./...` | ✅ OK |

Docker-сборка образа проверяется отдельным CI job; реальная Mapbox-квота
не расходовалась ни одним из тестов данного этапа.
Коммит: Document API limits and operational behavior.

Уточнение проверки 14 октября: после восстановления автоматического одобрения
повторно выполнен полный go test ./... -race -count=1 -timeout 2m с
TEST_DATABASE_DSN (PostgreSQL-тесты не пропущены). Все пакеты прошли; vet,
форматирование, diff --check и OpenAPI также прошли. Проверка middleware
подтверждает доступность catalog/health именно при занятом маршрутном слоте.
Локальный запуск собранного API и повторная полная Docker-сборка из пункта 7
в этом прогоне не выполнялись; запись о выполненном пункте 7 выше относится
к документации и перечисленным Go/OpenAPI-проверкам, а не к Docker-сборке.

### Завершение пункта 7 — 14 октября

Собранный локальный API запущен на временном порту 28082 с PostgreSQL.
Проверены два принятых ошибочных запроса (generate/recalculate → 400), затем
общий лимит (429 rate_limited/Retry-After), уникальные X-Request-ID и корреляция
с логами. После перегрузки health/catalog отвечают 200. API остановлен.

Полная сборка docker build --tag vandrouka-api:day29 ./backend завершилась
успешно с golang:1.25-alpine и runtime alpine:3.20. Этот образ запущен во
временном контейнере на порту 28083 с внешней /app/data read-only и PostgreSQL.
Health успешен; при старте прочитаны 60 YAML и каталог выдаёт 60 мест;
проверены 400/429, Retry-After, Request ID и JSON-логи. Контейнер удалён.
Ограничение предыдущей проверки Docker из-за скачивания образа закрыто.

Compose config, OpenAPI и diff --check прошли. Полная race-регрессия с PostgreSQL,
vet и форматирование подтверждены предыдущим финальным прогоном. Mapbox-квота
в этих smoke-проверках не использовалась. День 29 полностью завершён.
Удалённый GitHub CI остаётся непроверенным до отдельного пуша.
