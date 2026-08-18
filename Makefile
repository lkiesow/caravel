.PHONY: run build test dev dev-restart dev-marker dev-seed dev-reset vet check-js check-i18n test-ui ci

run:
	go run ./cmd/caravel

# check-port first: without it a busy port makes go run die with "address already
# in use", which is easy to miss when dev is started in the background — and then
# every request is answered by the *stale* server. See scripts/dev_server.sh.
dev:
	@scripts/dev_server.sh check-port
	CARAVEL_WEB_DIR=web go run ./cmd/caravel

# Replace the running dev server, killing by port rather than process name.
# Optionally assert the new binary contains a string: make dev-restart MARKER=foo
dev-restart:
	@scripts/dev_server.sh restart $(if $(MARKER),MARKER=$(MARKER),)

# Assert the *already running* server contains a string, without restarting it —
# "am I testing my change, or a stale binary?": make dev-marker MARKER=foo
dev-marker:
	@test -n "$(MARKER)" || { echo "usage: make dev-marker MARKER=somestring" >&2; exit 2; }
	@scripts/dev_server.sh check-marker $(MARKER)

# Seed every scenario, or one: make dev-seed SCENARIO=one-pin
dev-seed:
	go run ./cmd/seed $(if $(SCENARIO),-scenario=$(SCENARIO),)

# Wipe the dev database and uploads, then reseed. Guarded: refuses a DSN outside
# the repo, and prompts unless FORCE=1.
dev-reset:
	@scripts/dev_reset.sh $(if $(SCENARIO),-scenario=$(SCENARIO),)

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

# Playwright UI suite (Firefox). Drives a *running* server — start one with
# `make dev-restart` and seed it with `make dev-reset FORCE=1` first. Not part of
# `make ci`: it needs a browser and a live server, so CI runs it as its own job.
test-ui:
	npx playwright test $(if $(GREP),--grep "$(GREP)",)

ci: build vet check-js check-i18n test
