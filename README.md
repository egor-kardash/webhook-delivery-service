# Webhook Delivery Service

Сервис доставки webhook-событий: принимает событие, находит активные подписки, ставит доставки в очередь и отправляет HTTP POST с HMAC-подписью, повторами и журналом статусов. Гарантия - at-least-once.

## Статус

Скелет. Реализованы только health/ready и каркас HTTP-слоя (request id, access log, JSON-ошибки) и схема БД с миграциями. Бизнес-эндпоинтов пока нет.

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

4. Остановить, сохранив данные:

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

Индексы рассчитаны на три запроса: GIN по `event_types` (только активные
подписки), частичный по `next_attempt_at` для `pending` (claim) и частичный по
`locked_until` для `in_progress` (подхват после падения воркера). `next_attempt_at`
и `locked_until` считает БД через `now()`.

Внешние ключи - `ON DELETE RESTRICT`: история доставок не теряется.

## Миграции

golang-migrate, файлы в `migrations/`, применяются контейнером `migrate`.

    make migrate-create seq=<name>   # новая пара up/down
    make migrate-up                  # накатить все
    make migrate-down                # откатить один шаг
    make migrate-down steps=2        # откатить два

- `migrate-down` полностью удаляет схему `webhook` вместе с данными.

## Эндпоинты

| Метод | Путь      | Ответ                                                        |
|-------|-----------|--------------------------------------------------------------|
| GET   | /healthz  | 200 `{"status":"ok"}`                                        |
| GET   | /readyz   | 200 `{"status":"ready"}` / 503 `{"error":{"code":"database_unavailable"}}` |

Ошибки - единый envelope `{"error":{"code":"...","message":"..."}}`. `fields` появляется только при ошибках валидации. Неизвестный маршрут → JSON 404, неподдерживаемый метод → JSON 405 + `Allow`.

## Команды

`make help` - полный список. Основное: `dev`, `dev-down`, `db-up`, `db-reset`, `migrate-up`, `migrate-down`, `test`, `check`.
