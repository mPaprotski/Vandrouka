.PHONY: up down test lint migrate-up migrate-down migrate-reset migrate-status import

# Docker
COMPOSE_ENV_FILE ?= backend/.env
COMPOSE := docker compose --env-file "$(COMPOSE_ENV_FILE)"
up:
	$(COMPOSE) up --build

down:
	$(COMPOSE) down

# Backend
test:
	cd backend && go test ./... -v

lint:
	cd backend && go vet ./...
	@test -z "$$(cd backend && gofmt -l .)"

# Migrations
DSN ?= postgres://postgres:postgres@localhost:25432/vandrouka?sslmode=disable
GOOSE := cd backend && POSTGRES_DSN='$(DSN)' go run ./cmd/migrate

migrate-up:
	$(GOOSE) up

migrate-down:
	$(GOOSE) down

migrate-reset:
	$(GOOSE) reset

migrate-status:
	$(GOOSE) status

# Data import (host path, relative to repository root)
DATA_DIR ?= ../data
import:
	cd backend && POSTGRES_DSN='$(DSN)' go run ./cmd/import "$(abspath $(DATA_DIR))/places/minsk"
