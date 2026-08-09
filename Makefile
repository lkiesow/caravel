.PHONY: run build test dev

run:
	go run ./cmd/caravel

dev:
	CARAVEL_WEB_DIR=web go run ./cmd/caravel

build:
	go build -o bin/caravel ./cmd/caravel

test:
	go test ./...
