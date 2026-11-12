GO  ?= go
BIN ?= bin/gocr
DSN ?= gocr:gocr@tcp(127.0.0.1:3306)/gocr?parseTime=true&charset=utf8mb4&loc=Local

.PHONY: build run serve test race integration lint ci up down tidy evals

build:
	$(GO) build -trimpath -ldflags "-s -w -X github.com/csx-3323730970/gocr/internal/cli.version=dev" -o $(BIN) ./cmd/gocr

run: build
	$(BIN) review --repo . --dry-run

serve:
	GOCR_DSN="$(DSN)" $(GO) run ./cmd/gocr serve

test:
	$(GO) test ./...

race:
	$(GO) test -race ./...

integration:
	GOCR_TEST_DSN="$(DSN)" $(GO) test -tags=integration ./internal/store/mysql/...

lint:
	$(GO) vet ./...

ci: lint race build

up:
	docker compose up -d --build

down:
	docker compose down

tidy:
	$(GO) mod tidy

evals:
	$(GO) run ./evals/runner
