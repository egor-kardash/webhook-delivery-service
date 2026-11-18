# Webhook Delivery Service

Сервис доставки webhook-событий: принимает событие, находит активные подписки, ставит доставки в очередь и отправляет HTTP POST с HMAC-подписью, повторами и журналом статусов. Гарантия - at-least-once.

## Статус

Скелет. Реализованы только health/ready и каркас HTTP-слоя (request id, access log, JSON-ошибки). Бизнес-эндпоинтов пока нет.

## Быстрый старт

Нужны Go 1.27+, Docker с compose и три обязательные переменные: `POSTGRES_DB`, `POSTGRES_USER`, `POSTGRES_PASSWORD`.

1. Создать `.env` и заполнить переменные `POSTGRES_DB`, `POSTGRES_USER`, `POSTGRES_PASSWORD`:

       cp .env.example .env

2. Поднять Postgres и приложение:

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

## Эндпоинты

| Метод | Путь      | Ответ                                                        |
|-------|-----------|--------------------------------------------------------------|
| GET   | /healthz  | 200 `{"status":"ok"}`                                        |
| GET   | /readyz   | 200 `{"status":"ready"}` / 503 `{"error":{"code":"database_unavailable"}}` |

Ошибки - единый envelope `{"error":{"code":"not_found","message":"route not found","fields":{}}}`.
Неизвестный маршрут → JSON 404, неподдерживаемый метод → JSON 405 + `Allow`.

## Команды

`make help` - полный список. Основное: `dev`, `dev-down`, `db-reset`, `test`, `check`.
