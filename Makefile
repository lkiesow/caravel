.PHONY: run build test dev dev-seed vet check-js check-i18n ci

run:
	go run ./cmd/caravel

dev:
	CARAVEL_WEB_DIR=web go run ./cmd/caravel

dev-seed:
	CARAVEL_WEB_DIR=web go run ./cmd/seed

build:
	go build -o bin/caravel ./cmd/caravel

test:
	go test ./...

vet:
	go vet ./...

check-js:
	scripts/check_js.sh

check-i18n:
	python3 scripts/check_i18n.py

ci: build vet check-js check-i18n test
