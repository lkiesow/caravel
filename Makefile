.PHONY: run build test dev dev-restart dev-marker dev-version dev-seed dev-reset vet check-js check-i18n check-env test-ui test-postgres ci docs docs-serve

# The build's identity, stamped into the binary at link time and reported by the
# startup banner and GET /api/health — so "which build is this server running?"
# is answerable without a marker string invented per test (see dev-marker below,
# and internal/buildinfo). scripts/version.sh owns the string so this and
# scripts/dev_server.sh can't drift apart.
VERSION := $(shell scripts/version.sh)
LDFLAGS := -X caravel/internal/buildinfo.Version=$(VERSION)

run:
	go run -ldflags "$(LDFLAGS)" ./cmd/caravel

# check-port first: without it a busy port makes go run die with "address already
# in use", which is easy to miss when dev is started in the background — and then
# every request is answered by the *stale* server. See scripts/dev_server.sh.
dev:
	@scripts/dev_server.sh check-port
	CARAVEL_WEB_DIR=web go run -ldflags "$(LDFLAGS)" ./cmd/caravel

# Replace the running dev server, killing by port rather than process name.
# Optionally assert the new binary contains a string: make dev-restart MARKER=foo
dev-restart:
	@scripts/dev_server.sh restart $(if $(MARKER),MARKER=$(MARKER),)

# Assert the *already running* server contains a string, without restarting it —
# "am I testing my change, or a stale binary?": make dev-marker MARKER=foo
dev-marker:
	@test -n "$(MARKER)" || { echo "usage: make dev-marker MARKER=somestring" >&2; exit 2; }
	@scripts/dev_server.sh check-marker $(MARKER)

# The marker-free version of the same question: ask the running server what build
# it is and compare with this tree. Answers over HTTP rather than by grepping
# /proc, so it works against any instance, not only a local one.
#
# A "-dirty" version on both sides is a weak match by nature — it says "same
# commit, both dirty", not "same code" — so uncommitted edits still want
# dev-marker with a real string from the change.
dev-version:
	@running=$$(curl -fsS http://localhost:$${CARAVEL_PORT:-8080}/api/health | sed -n 's/.*"version":"\([^"]*\)".*/\1/p'); \
	expected=$$(scripts/version.sh); \
	if [ -z "$$running" ]; then \
		echo "dev-version: no server answered on :$${CARAVEL_PORT:-8080}" >&2; exit 1; \
	elif [ "$$running" = "$$expected" ]; then \
		echo "dev-version: running $$running, matches this tree"; \
	else \
		echo "dev-version: running $$running, but this tree is $$expected — the server is stale" >&2; exit 1; \
	fi

# Seed every scenario, or one: make dev-seed SCENARIO=one-pin
dev-seed:
	go run ./cmd/seed $(if $(SCENARIO),-scenario=$(SCENARIO),)

# Wipe the dev database and uploads, then reseed. Guarded: refuses a DSN outside
# the repo, and prompts unless FORCE=1.
dev-reset:
	@scripts/dev_reset.sh $(if $(SCENARIO),-scenario=$(SCENARIO),)

build:
	go build -ldflags "$(LDFLAGS)" -o bin/caravel ./cmd/caravel

test:
	go test ./...

vet:
	go vet ./...

# The same Go tests against Postgres instead of SQLite. Not part of `make ci`:
# it needs a container (or a Postgres you point CARAVEL_TEST_DB_DSN at), so CI
# runs it as its own job, the same split as test-ui.
#
#   make test-postgres                     bring the container up, test, stop it
#   make test-postgres KEEP=1              leave it running for the next run
#   make test-postgres ARGS="-run Search"  pass flags through to go test
test-postgres:
	@scripts/test_postgres.sh

check-js:
	scripts/check_js.sh

check-i18n:
	python3 scripts/check_i18n.py

# Every CARAVEL_* variable the compose files and the README name has to be one
# the app actually reads, and vice versa. See the script: a setting that is
# silently ignored is worse than one that fails loudly.
check-env:
	python3 scripts/check_env_vars.py

# Playwright UI suite (Firefox), headless by default. Drives a *running* server —
# start one with `make dev-restart` and seed it with `make dev-reset FORCE=1`
# first. Not part of `make ci`: it needs a browser and a live server, so CI runs
# it as its own job.
#
#   make test-ui                        headless, all specs
#   make test-ui GREP="heading outline"  one spec (a regex — mind the parens)
#   make test-ui HEADED=1               watch it in a real browser window
#   make test-ui HEADED=1 SLOWMO=300    ...slowed to 300ms/step so it's followable
#   make test-ui UI=1                   Playwright's interactive UI mode
#
# Headed runs force a single worker: four browser windows fighting for focus is
# unwatchable, which defeats the point of asking to see it.
PW_ENV = $(if $(HEADED),CARAVEL_TEST_HEADED=1)$(if $(SLOWMO), CARAVEL_TEST_SLOWMO=$(SLOWMO))
PW_ARGS = $(if $(GREP),--grep "$(GREP)")$(if $(UI), --ui)$(if $(HEADED), --headed --workers=1)

test-ui:
	$(PW_ENV) npx playwright test $(PW_ARGS)

ci: build vet check-js check-i18n check-env test

# The project website and documentation (docs/, zensical.toml, overrides/).
#
# Deliberately not part of `make ci`: that gate is the app, and it should not
# need a Python site generator installed to tell you whether the Go code is
# broken. The equivalent check runs as its own job in .github/workflows/ci.yml,
# so a dead link still fails a pull request -- but if you changed anything under
# docs/, run this before committing rather than finding out from CI.
#
# Strict mode, matching CI: a dead link or an unresolved reference is an error,
# not a line of output nobody reads.
#
#   pip install 'zensical==0.0.57'   (the version the deploy workflow pins)
docs:
	zensical build --clean --strict

# Serves the site and rebuilds on change, at http://localhost:8000. Note this
# is a different port from the app's 8080, so both can run at once.
docs-serve:
	zensical serve
