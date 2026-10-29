SHELL := /bin/sh
VERSION ?= 1.0.0
GO ?= go

.PHONY: help tidy fmt vet build run test test-race integration docker-up docker-down docker-reset logs ps seed openapi smoke

help:
	@echo "LoveService backend"
	@echo "  make run             - run API locally (requires .env + infra)"
	@echo "  make test            - unit tests"
	@echo "  make integration     - end-to-end tests against http://localhost:8080"
	@echo "  make docker-up       - start Postgres, Redis, Kafka, MinIO and API"
	@echo "  make docker-down     - stop the stack"
	@echo "  make docker-reset    - stop and delete volumes"
	@echo "  make logs            - follow API logs"
	@echo "  make smoke           - curl-based smoke test"

tidy:
	$(GO) mod tidy

fmt:
	gofmt -w ./cmd ./internal ./tests

vet:
	$(GO) vet ./...

build:
	CGO_ENABLED=0 $(GO) build -trimpath -ldflags="-s -w -X main.version=$(VERSION)" -o bin/loveservice ./cmd/api

run:
	$(GO) run ./cmd/api

test:
	$(GO) test ./...

test-race:
	$(GO) test -race ./...

integration:
	LOVESERVICE_BASE_URL=http://localhost:8080 $(GO) test -tags=integration ./tests/integration/... -v

docker-up:
	docker compose up -d --build

docker-down:
	docker compose down

docker-reset:
	docker compose down -v

logs:
	docker compose logs -f api

ps:
	docker compose ps

seed:
	docker compose restart api

smoke:
	sh ./scripts/smoke.sh
