-include .env
export

COMPOSE := docker compose
BIN := ./bin/server

.PHONY: help check-env app-build app-run dev dev-down \
        db-up db-down db-reset tidy-check test lint fmt fmt-check check

help:
	@echo "app-build          build the binary"
	@echo "app-run            build and run the app"
	@echo "dev                db + app"
	@echo "dev-down           stop containers, keep data"
	@echo "db-up / db-down    postgres only"
	@echo "db-reset           remove containers and volumes (asks first)"
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

dev: db-up app-run

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
