#!/usr/bin/env bash
# Runs `go test ./...` against Postgres instead of SQLite.
#
# Why this exists: every test opens its database through internal/dbtest, which
# takes the dialect from the environment. Without this the Postgres half of
# internal/db is verified only by compiling - see the package comment there.
#
#   make test-postgres                     # bring the container up, run tests
#   make test-postgres KEEP=1              # leave it running afterwards
#   make test-postgres ARGS="-run Search"  # pass flags through to go test
#
# Point it at a Postgres you already run with:
#   CARAVEL_TEST_DB_DSN=postgres://... make test-postgres
# in which case no container is started or stopped.
set -euo pipefail

cd "$(dirname "$0")/.."

COMPOSE_FILE=docker-compose.postgres.yml
DEFAULT_DSN="postgres://caravel:caravel@localhost:5432/caravel?sslmode=disable"

# Podman and Docker are both fine, and this box may have either: `docker
# compose` and `podman compose` take the same arguments. Checked in that order
# only because docker is the more common; neither is preferred.
compose() {
	if command -v docker >/dev/null 2>&1 && docker compose version >/dev/null 2>&1; then
		docker compose "$@"
	elif command -v podman >/dev/null 2>&1; then
		podman compose "$@"
	else
		echo "test-postgres: neither docker nor podman found." >&2
		echo "Install one, or point CARAVEL_TEST_DB_DSN at a Postgres you already run." >&2
		exit 2
	fi
}

started_container=0
if [ -n "${CARAVEL_TEST_DB_DSN:-}" ]; then
	echo "test-postgres: using CARAVEL_TEST_DB_DSN, not starting a container"
else
	export CARAVEL_TEST_DB_DSN="$DEFAULT_DSN"
	echo "test-postgres: starting the db service from $COMPOSE_FILE"
	compose -f "$COMPOSE_FILE" up -d db
	started_container=1

	# Wait for "accepting connections" rather than for the container to exist:
	# Postgres bounces itself once while initialising a fresh volume, so the
	# first successful connection can be to an instance that is about to go
	# away.
	ready=0
	for _ in $(seq 1 60); do
		if compose -f "$COMPOSE_FILE" exec -T db pg_isready -U caravel -d caravel >/dev/null 2>&1; then
			ready=1
			break
		fi
		sleep 1
	done
	if [ "$ready" != 1 ]; then
		echo "test-postgres: postgres did not become ready; recent logs follow:" >&2
		compose -f "$COMPOSE_FILE" logs --tail 30 db >&2 || true
		exit 1
	fi
fi

export CARAVEL_TEST_DB_DRIVER=postgres

# Sweep schemas left behind by a killed run. internal/dbtest deliberately does
# not do this itself: schema names are unique per process, so a test that
# dropped a name it did not create would be deleting a concurrent package's
# database - which is exactly the bug this sweep replaces. Here there is no
# concurrency to get wrong, because nothing is running yet.
if [ "$started_container" = 1 ]; then
	swept=$(compose -f "$COMPOSE_FILE" exec -T db psql -U caravel -d caravel -tAc "
		DO \$\$
		DECLARE s text; n int := 0;
		BEGIN
			FOR s IN SELECT schema_name FROM information_schema.schemata
				WHERE schema_name LIKE 'caravel\\_test\\_%' LOOP
				EXECUTE format('DROP SCHEMA %I CASCADE', s);
				n := n + 1;
			END LOOP;
			RAISE NOTICE 'swept % leftover schema(s)', n;
		END \$\$;" 2>&1 | grep -o 'swept .*' || true)
	[ -n "$swept" ] && echo "test-postgres: $swept"
fi

cleanup() {
	status=$?
	if [ "$started_container" = 1 ] && [ -z "${KEEP:-}" ]; then
		echo "test-postgres: stopping the db service (KEEP=1 to leave it up)"
		compose -f "$COMPOSE_FILE" stop db >/dev/null 2>&1 || true
	fi
	exit "$status"
}
trap cleanup EXIT

# -count=1 so a pass is never served from the cache: the point of this run is
# that it touches a different database than the cached one did.
echo "test-postgres: go test against $CARAVEL_TEST_DB_DRIVER"
go test -count=1 ${ARGS:-} ./...
