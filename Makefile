-include .env
export

COMPOSE := docker compose
BIN := ./bin/server
SSLMODE := $(or $(POSTGRES_SSLMODE),disable)

.PHONY: help check-env app-build app-run dev dev-down \
		migrate-create migrate-up migrate-down migrate-action \
        db-up db-down db-reset tidy-check test lint fmt fmt-check check

help:
	@echo "app-build          build the binary"
	@echo "app-run            build and run the app"
	@echo "dev                db + migrations + app"
	@echo "dev-down           stop containers, keep data"
	@echo "db-up / db-down    postgres only"
	@echo "db-reset           remove containers and volumes (asks first)"
	@echo "migrate-create     make migrate-create seq=<name>"
	@echo "migrate-up         apply all migrations"
	@echo "migrate-down       roll back one migration (steps=N for more)"
	@echo "tidy-check         check go.mod/go.sum are tidy (no changes made)"
	@echo "test               unit tests with -race -shuffle -count=1"
	@echo "lint               golangci-lint"
	@echo "fmt                format code (gofumpt + import grouping + golines)"
	@echo "fmt-check          check formatting (gofumpt + import grouping + golines)"
	@echo "check              fmt-check + tidy-check + build + vet + test + lint"

check-env:
	@test -f .env || { echo "error: .env is missing. Copy .env.example to .env and fill it in." >&2; exit 1; }
	@test -n "$(POSTGRES_USER)" || { echo "error: POSTGRES_USER is empty in .env" >&2; exit 1; }
	@test -n "$(POSTGRES_PASSWORD)" || { echo "error: POSTGRES_PASSWORD is empty in .env" >&2; exit 1; }
	@test -n "$(POSTGRES_DB)" || { echo "error: POSTGRES_DB is empty in .env" >&2; exit 1; }

app-build:
	@go build -o $(BIN) ./cmd/server

app-run: check-env app-build
	@$(BIN)

dev: db-up migrate-up app-run

dev-down:
	@$(COMPOSE) down

db-up: check-env
	@$(COMPOSE) up -d --wait db

db-down:
	@$(COMPOSE) stop db

db-reset:
	@printf "Delete the environment's volumes and all DB data? [y/N]: "; \
	read ans; \
	if [ "$$ans" = "y" ]; then \
		$(COMPOSE) down -v; \
		echo "Environment stopped, volumes removed."; \
	else \
		echo "Reset cancelled."; \
	fi

migrate-create:
	@if [ -z "$(seq)" ]; then \
		echo "seq parameter is required. Example: make migrate-create seq=init"; \
		exit 1; \
	fi; \
	mkdir -p migrations; \
	$(COMPOSE) run --rm --no-deps --user $(shell id -u):$(shell id -g) migrate \
		create \
		-ext sql \
		-dir /migrations \
		-seq "$(seq)"

migrate-up:
	@$(MAKE) migrate-action action=up

migrate-down:
	@$(MAKE) migrate-action action=down steps=$(or $(steps),1)

migrate-action: check-env
	@if [ -z "$(action)" ]; then \
		echo "action parameter is required. Example: make migrate-action action=up"; \
		exit 1; \
	fi; \
	$(COMPOSE) run --rm migrate \
		-path /migrations \
		-database "postgres://$(POSTGRES_USER):$(POSTGRES_PASSWORD)@db:5432/$(POSTGRES_DB)?sslmode=$(SSLMODE)" \
		"$(action)" $(if $(steps),$(steps))


tidy-check:
	@go mod tidy -diff

test:
	@go test -race -shuffle=on -count=1 -timeout=2m ./...

lint:
	@golangci-lint run

fmt:
	@gofmt -w .
	@for i in 1 2 3; do \
		golangci-lint fmt || exit 1; \
		golangci-lint fmt --diff >/dev/null 2>&1 && exit 0; \
	done; \
	echo "error: golangci-lint fmt did not converge after 3 passes" >&2; \
	exit 1

fmt-check:
	@unformatted="$$(gofmt -l .)"; \
	if [ -n "$$unformatted" ]; then \
		echo "These files are not gofmt'ed:"; \
		echo "$$unformatted"; \
		exit 1; \
	fi
	@golangci-lint fmt --diff

check:
	@$(MAKE) fmt-check
	@$(MAKE) tidy-check
	@go build ./...
	@go vet ./...
	@$(MAKE) test
	@$(MAKE) lint
