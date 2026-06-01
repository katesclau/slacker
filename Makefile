SHELL := /usr/bin/env bash

ifneq (,$(wildcard .env))
include .env
export
endif

APP_NAME ?= slacker
APP_PORT ?= 8080
POSTGRES_DSN ?= postgres://postgres:postgres@localhost:5432/slacker?sslmode=disable
POSTGRES_SERVICE ?= postgres
POSTGRES_USER ?= postgres
POSTGRES_DB ?= slacker
MIGRATION_FILE ?= db/migrations/001_init.sql
AIR_CONFIG ?= .air.toml

.PHONY: help infra-up infra-down infra-restart infra-logs infra-ps db-migrate db-shell run air air-install test test-race fmt fmt-check vet lint check clean

help: ## Show available targets
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make <target>\n\nTargets:\n"} /^[a-zA-Z0-9_.-]+:.*##/ {printf "  %-16s %s\n", $$1, $$2}' $(MAKEFILE_LIST)
	@printf "\n"

infra-up: ## Start local infra (postgres/redis/minio)
	docker compose up -d

infra-down: ## Stop local infra
	docker compose down

infra-restart: ## Restart local infra
	docker compose down && docker compose up -d

infra-logs: ## Tail local infra logs
	docker compose logs -f

infra-ps: ## Show local infra containers
	docker compose ps

db-migrate: ## Apply SQL migration file to Postgres
	docker compose exec -T "$(POSTGRES_SERVICE)" psql -U "$(POSTGRES_USER)" -d "$(POSTGRES_DB)" < "$(MIGRATION_FILE)"

db-shell: ## Open psql shell to configured database
	docker compose exec "$(POSTGRES_SERVICE)" psql -U "$(POSTGRES_USER)" -d "$(POSTGRES_DB)"

run: infra-up db-migrate ## Run slacker locally
	go run ./cmd/slacker

air: infra-up db-migrate ## Run slacker with hot reload (Air)
	@if command -v air >/dev/null 2>&1; then \
		air -c "$(AIR_CONFIG)"; \
	else \
		echo "air not installed. Run 'make air-install' first."; \
		exit 1; \
	fi

air-install: ## Install Air hot reload tool
	go install github.com/air-verse/air@latest

test: ## Run unit tests
	go test ./...

test-race: ## Run tests with race detector
	go test -race ./...

fmt: ## Format Go source files
	gofmt -w $$(rg --files -g '*.go')

fmt-check: ## Check formatting (fails if changes needed)
	@files="$$(rg --files -g '*.go')"; \
	out="$$(gofmt -l $$files)"; \
	if [ -n "$$out" ]; then \
		echo "Unformatted files:"; \
		echo "$$out"; \
		exit 1; \
	fi

vet: ## Run go vet
	go vet ./...

lint: ## Run lint checks (fmt-check + vet + golangci-lint if installed)
	$(MAKE) fmt-check
	$(MAKE) vet
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run ./...; \
	else \
		echo "golangci-lint not installed, skipping"; \
	fi

check: ## Run formatting, lint, and tests
	$(MAKE) lint
	$(MAKE) test

clean: ## Remove build artifacts and test cache
	go clean -testcache
