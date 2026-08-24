#!/usr/bin/env bash
# Runs the Playwright UI suite against a throwaway instance of its own.
#
#   scripts/ui_test.sh              # or: make test-ui
#   scripts/ui_test.sh --grep foo   # arguments are passed through to playwright
#
# The instance -- own port, own database, own uploads, own seed, own saved
# sessions, torn down on exit -- is scripts/with_server.sh's job, which
# make check-contrast uses too. That script's header explains why it exists.
#
# The escape hatch, for watching a failure against a database you can inspect:
#
#   CARAVEL_TEST_URL=http://localhost:8080 make test-ui
set -euo pipefail

cd "$(dirname "$0")/.."

exec scripts/with_server.sh npx playwright test "$@"
