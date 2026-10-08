.PHONY: up down test lint migrate-up migrate-down migrate-reset migrate-status import

# Docker
up:
	docker compose up --build

down:
	docker compose down

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

# Data import
import:
	cd backend && POSTGRES_DSN='$(DSN)' go run ./cmd/import ../data/places/minsk
