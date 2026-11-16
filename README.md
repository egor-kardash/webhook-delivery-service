# Webhook Delivery Service

Сервис доставки webhook-событий: принимает событие, находит активные подписки, ставит доставки в очередь и отправляет HTTP POST с HMAC-подписью, повторами и журналом статусов. Гарантия - at-least-once.

## Статус

В разработке.

## Стек

Go, PostgreSQL, pgx, net/http, golang-migrate, docker.
