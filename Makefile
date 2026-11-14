GO ?= go
GOOSE ?= go tool goose
SQLC ?= go tool sqlc

.PHONY: bootstrap fmt test test-race vet check infra-up infra-down migrate-up migrate-down generate dev

bootstrap:
	$(GO) mod download

fmt:
	$(GO) fmt ./...

test:
	$(GO) test -cover ./...

test-race:
	CGO_ENABLED=1 $(GO) test -race ./...

vet:
	$(GO) vet ./...

check: fmt vet test

infra-up:
	docker compose up -d postgres nats

infra-down:
	docker compose down

migrate-up:
	$(GOOSE) -dir migrations postgres "$(DATABASE_URL)" up

migrate-down:
	$(GOOSE) -dir migrations postgres "$(DATABASE_URL)" down

generate:
	$(SQLC) generate

dev:
	docker compose up --build

