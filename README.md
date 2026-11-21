# Webhook Delivery Service

Сервис доставки webhook-событий: принимает событие, находит активные подписки, ставит доставки в очередь и отправляет HTTP POST с HMAC-подписью, повторами и журналом статусов. Гарантия - at-least-once.

## Статус

Скелет. Реализованы health/ready, каркас HTTP-слоя (request id, access log, JSON-ошибки), схема БД с миграциями и создание подписок `POST /subscriptions`. Публикации событий, доставки и worker'а пока нет.

## Требования

- Go 1.27+
- Docker с compose (Postgres и golang-migrate запускаются контейнерами)
- golangci-lint - для `make lint` и `make check`

## Быстрый старт

1. Создать `.env` и заполнить переменные `POSTGRES_DB`, `POSTGRES_USER`, `POSTGRES_PASSWORD`:

       cp .env.example .env

2. Поднять Postgres, накатить миграции и запустить приложение:

       make dev

3. В другом терминале проверить:

       curl -s localhost:8080/healthz
       curl -s localhost:8080/readyz

   `/readyz` отвечает `503`, если БД недоступна.

4. Создать подписку и сохранить `secret_key` из ответа - второй раз сервер его не отдаст:

       curl -s -X POST localhost:8080/subscriptions \
         -H 'Content-Type: application/json' \
         -d '{"target_url":"https://example.com","event_types":["user.created"]}'

5. Остановить, сохранив данные:

       make dev-down

## Конфигурация

Переменные окружения.

| Переменная                | Обязательна | По умолчанию |
|---------------------------|-------------|--------------|
| POSTGRES_DB               | да          | -            |
| POSTGRES_USER             | да          | -            |
| POSTGRES_PASSWORD         | да          | -            |
| POSTGRES_HOST             | нет         | localhost    |
| POSTGRES_PORT             | нет         | 5433         |
| POSTGRES_SSLMODE          | нет         | disable      |
| HTTP_ADDR                 | нет         | :8080        |
| HTTP_READ_HEADER_TIMEOUT  | нет         | 5s           |
| HTTP_READ_TIMEOUT         | нет         | 5s           |
| HTTP_WRITE_TIMEOUT        | нет         | 10s          |
| HTTP_IDLE_TIMEOUT         | нет         | 60s          |
| HTTP_SHUTDOWN_TIMEOUT     | нет         | 10s          |
| LOG_LEVEL                 | нет         | info         |
| LOG_FORMAT                | нет         | text         |

Пустая необязательная переменная = применяется значение по умолчанию. Пустая обязательная - ошибка при старте.

## Схема БД

Схема `webhook`, три таблицы:

- `subscriptions` - подписки: `target_url`, `secret_key`, `event_types text[]`, `active`;
- `events` - события: `event_type`, `payload` (JSON-объект), `idempotency_key`. `idempotency_key` уникален: повторная публикация с тем же ключом не создает событие;
- `deliveries` - доставки: `status`, `attempts`, `next_attempt_at`, `locked_until`, `last_status_code`, `last_error`. Пара `(event_id, subscription_id)` уникальна: одна доставка на событие и подписку.

Индексы рассчитаны на три запроса: GIN по `event_types` (только активные подписки), частичный по `next_attempt_at` для `pending` (claim) и частичный по `locked_until` для `in_progress` (подхват после падения воркера). `next_attempt_at` и `locked_until` считает БД через `now()`.

Внешние ключи - `ON DELETE RESTRICT`: история доставок не теряется.

## Миграции

golang-migrate, файлы в `migrations/`, применяются контейнером `migrate`.

    make migrate-create seq=<name>   # новая пара up/down
    make migrate-up                  # накатить все
    make migrate-down                # откатить один шаг
    make migrate-down steps=2        # откатить два

- `migrate-down` полностью удаляет схему `webhook` вместе с данными.

## Эндпоинты

| Метод | Путь             | Ответ                                                                      |
|-------|------------------|----------------------------------------------------------------------------|
| GET   | /healthz         | 200 `{"status":"ok"}`                                                      |
| GET   | /readyz          | 200 `{"status":"ready"}` / 503 `{"error":{"code":"database_unavailable"}}` |
| POST  | /subscriptions   | 201 + `Location`, тело с `secret_key`; ошибки 400/413/415/500/503          |

Ошибки - единый envelope `{"error":{"code":"...","message":"..."}}`. `fields` появляется только при ошибках валидации. Неизвестный маршрут → JSON 404, неподдерживаемый метод → JSON 405 + `Allow`.

### POST /subscriptions

Создает подписку. Повторный `POST` с теми же данными создаст **еще одну** подписку: идемпотентности у этого пути нет.

Запрос (`Content-Type: application/json` или `*+json`, тело не больше 1 MiB, ровно один JSON-объект, неизвестные поля запрещены):

    {"target_url":"https://example.com","event_types":["user.created","user.updated"]}

Ответ `201`:

    {
      "id": 1,
      "target_url": "https://example.com",
      "secret_key": "<generated-secret>",
      "event_types": ["user.created", "user.updated"],
      "active": true,
      "created_at": "2026-11-19T18:00:00Z"
    }

Заголовки ответа: `Location: /subscriptions/1`, `Cache-Control: no-store`, `Pragma: no-cache` - секрет нельзя кэшировать.

Правила:

- `target_url` - только `http`/`https` с непустым host; пробелы по краям обрезаются,
  схема и host приводятся к нижнему регистру;
- `event_types` - минимум один непустой тип; пробелы обрезаются, дубликаты
  удаляются, порядок сохраняется;
- `secret_key` генерируется сервером, возвращается **только в этом ответе**, в
  логах не появляется и позже не отдается;
- `active` у новой подписки всегда `true`; `id` и `created_at` присваивает БД.

`GET /subscriptions/{id}` пока не реализован: `Location` указывает на будущий ресурс, сейчас такой запрос вернет JSON `404 not_found`.

Коды ошибок `POST /subscriptions`:

| Статус | `code`                   | Когда                                                              |
|--------|--------------------------|--------------------------------------------------------------------|
| 400    | `invalid_json`           | не JSON, не объект, несколько значений, неизвестное поле, неверный тип |
| 400    | `validation_failed`      | `target_url`/`event_types`; причины - в `error.fields`              |
| 413    | `request_too_large`      | тело больше 1 MiB                                                  |
| 415    | `unsupported_media_type` | нет `Content-Type: application/json` (или `*+json`)                 |
| 500    | `internal_error`         | внутренняя ошибка, включая ошибки БД, не классифицированные как временные |
| 503    | `service_unavailable`    | таймаут или отмена запроса к зависимости                            |

## Команды

`make help` - полный список. Основное: `dev`, `dev-down`, `db-up`, `db-reset`, `migrate-up`, `migrate-down`, `test`, `check`.
