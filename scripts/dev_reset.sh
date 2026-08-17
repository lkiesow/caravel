#!/usr/bin/env bash
# Wipes the local dev database and uploads, then reseeds every scenario.
#
# Stage 07 built each milestone's test data through ad-hoc fetch calls and then
# hand-deleted the results imperfectly — stray test trips outlived the stage.
# This removes the cleanup half of that problem: reset instead of tidy up.
#
# Guards, because this deletes data:
#   - refuses unless the driver is sqlite (there's no file to delete otherwise);
#   - refuses unless the database file resolves INSIDE this repo, so a DSN
#     pointing at a real deployment can't be wiped by a stray `make dev-reset`;
#   - prompts for confirmation unless FORCE=1.
#
# It also stops the dev server first if one is running, and restarts it
# afterwards — deleting a SQLite file out from under a live server leaves it
# holding the deleted inode, which looks like the reset silently did nothing.
set -euo pipefail

cd "$(dirname "$0")/.."
REPO_ROOT="$(pwd -P)"

DRIVER="${CARAVEL_DB_DRIVER:-sqlite}"
DSN="${CARAVEL_DB_DSN:-data/caravel.db}"
UPLOAD_DIR="${CARAVEL_UPLOAD_DIR:-uploads}"

if [ "$DRIVER" != "sqlite" ]; then
	echo "dev-reset: CARAVEL_DB_DRIVER is ${DRIVER}, not sqlite — refusing to guess how to wipe it." >&2
	exit 1
fi

# Resolve without requiring the file to exist yet (realpath -m), then confirm it
# is inside the repo. This is the guard that stops a misconfigured environment
# from deleting something that matters.
db_path="$(realpath -m -- "$DSN")"
uploads_path="$(realpath -m -- "$UPLOAD_DIR")"

for target in "$db_path" "$uploads_path"; do
	case "$target" in
		"$REPO_ROOT"/*) ;;
		*)
			echo "dev-reset: ${target} is outside the repo (${REPO_ROOT}) — refusing to delete it." >&2
			echo "dev-reset: this guard exists so a DSN pointing at a real deployment can't be wiped." >&2
			exit 1
			;;
	esac
done

echo "dev-reset: about to DELETE"
echo "  database: ${db_path}"
echo "  uploads:  ${uploads_path}"
echo "and reseed every scenario."

if [ "${FORCE:-0}" != "1" ]; then
	if [ ! -t 0 ]; then
		echo "dev-reset: not a terminal and FORCE=1 not set — refusing to delete unattended." >&2
		exit 1
	fi
	printf 'Type "yes" to continue: '
	read -r reply
	if [ "$reply" != "yes" ]; then
		echo "dev-reset: aborted, nothing deleted."
		exit 1
	fi
fi

# Stop the server before touching the files (see the header), remembering
# whether it was up so the machine is left as it was found.
server_was_running=0
if ! scripts/dev_server.sh check-port >/dev/null 2>&1; then
	server_was_running=1
	scripts/dev_server.sh stop
fi

# SQLite leaves -wal and -shm siblings behind; deleting only the main file can
# resurrect committed rows from the write-ahead log.
rm -f -- "$db_path" "${db_path}-wal" "${db_path}-shm"
rm -rf -- "$uploads_path"
echo "dev-reset: deleted database and uploads"

# db.Open() runs pending migrations before returning, so the seed binary builds
# the schema itself — no server needs to be up for this.
echo "dev-reset: seeding"
go run ./cmd/seed "$@"

if [ "$server_was_running" = "1" ]; then
	echo "dev-reset: restarting the dev server that was running before"
	scripts/dev_server.sh restart >/dev/null
	echo "dev-reset: server back up"
fi
echo "dev-reset: done"
