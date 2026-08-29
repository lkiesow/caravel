#!/usr/bin/env bash
# Runs a command against a throwaway Caravel instance of its own.
#
#   scripts/with_server.sh npx playwright test
#   scripts/with_server.sh node tests/ui/contrast.js --route /trips
#
# Own port, own database, own upload directory, own seed, own saved sessions,
# all under one mktemp -d removed on exit. The command is run with
# CARAVEL_TEST_URL pointing at it -- the same handoff playwright.config.js,
# contrast.js and gen_screenshots.mjs already read.
#
# Why this exists rather than each caller starting its own server: the UI suite
# used to drive whatever was listening on :8080 and read the scenarios
# `make dev-reset` seeds, which made the shared dev database part of the test
# fixture. todo.md recorded that failing three times -- a location added by hand
# while trying the assistant out, stray members on the `cascade` trip, and two
# concurrent runs restoring the same password over each other. Each time the
# named failure passed in isolation and the investigation went into code that
# was fine.
#
# The escape hatch: if CARAVEL_TEST_URL is already set, nothing is started and
# the command runs against that server instead -- the same idiom
# scripts/test_postgres.sh uses when CARAVEL_TEST_DB_DSN is set. Mind that this
# is the arrangement whose hazards are described above.
#
# One thing is still shared between concurrent runs: Playwright's test-results/
# directory, which it empties at startup. That costs a concurrent run its trace
# files on failure; it cannot make a passing run fail, because nothing is
# written there unless a test fails.
set -euo pipefail

cd "$(dirname "$0")/.."

if [ "$#" -eq 0 ]; then
	echo "usage: scripts/with_server.sh <command> [args...]" >&2
	exit 2
fi

# Run against a server somebody else is running, and start nothing.
if [ -n "${CARAVEL_TEST_URL:-}" ]; then
	echo "with-server: using CARAVEL_TEST_URL ($CARAVEL_TEST_URL), not starting a server"
	exec "$@"
fi

# Ports are probed rather than fixed so that two runs never collide. 8090 is
# clear of the dev server (8080), the docs server (8000) and the screenshot
# script (8099).
PORT_FIRST="${PORT:-8090}"
PORT_LAST=$((PORT_FIRST + 30))

work="$(mktemp -d)"
server_pid=""

# Idempotent, because the signal traps and the EXIT trap both reach it: a TERM
# runs cleanup and then exiting runs it again.
cleaned=""
cmd_pid=""
cleanup() {
	[ -n "$cleaned" ] && return 0
	cleaned=1
	if [ -n "$cmd_pid" ] && kill -0 "$cmd_pid" 2>/dev/null; then
		kill "$cmd_pid" 2>/dev/null || true
		wait "$cmd_pid" 2>/dev/null || true
	fi
	if [ -n "$server_pid" ] && kill -0 "$server_pid" 2>/dev/null; then
		kill "$server_pid" 2>/dev/null || true
		wait "$server_pid" 2>/dev/null || true
	fi
	rm -rf "$work"
}
# EXIT alone is not enough, and used to not run at all: this script ended in
# `exec "$@"`, which replaces the shell, so from the moment the tests started
# there was no trap left in the process. That is how runs killed mid-flight
# leaked their server and their temp directory -- 31 of them once, holding the
# whole port range. The command is now a child (see the end of the file) and
# these traps outlive it.
trap cleanup EXIT
trap 'cleanup; exit 130' INT
trap 'cleanup; exit 143' TERM
trap 'cleanup; exit 129' HUP
trap 'cleanup; exit 131' QUIT

export CARAVEL_DB_DSN="$work/ui.db"
export CARAVEL_UPLOAD_DIR="$work/uploads"
# Served from disk, matching `make dev`: the suite should test the working tree,
# not the last build.
export CARAVEL_WEB_DIR=web
# The assistant, pointed at its in-process fakes. assist.spec.js skips itself
# without these, and a skip reads as a pass -- so they are set here rather than
# only in CI, and the check further down refuses to run the suite if they did
# not take effect.
export CARAVEL_LLM_URL=stub
export CARAVEL_LLM_MODEL=stub
export CARAVEL_SEARCH_PROVIDER=stub
# The image picker, against an in-process fixture encyclopaedia that serves
# real PNGs -- including one deliberately dead thumbnail. See
# internal/wikimedia/stub.go.
export CARAVEL_WIKIMEDIA_URL=stub
# Saved sessions go in the temp dir too. They are cookies for *this* run's
# server, and cookies are not scoped by port -- so two runs sharing
# tests/ui/.auth/ would hand each other a token their own server has never
# issued, and every spec would fail as if logged out.
export CARAVEL_TEST_AUTH_DIR="$work/auth"

echo "with-server: building"
go build -o "$work/caravel" ./cmd/caravel

# Seed before starting, the way CI does: db.Open() runs the migrations, so this
# creates the schema and the users the suite logs in as. All seven scenarios --
# buildRoutes() in tests/ui/helpers/scenarios.js sweeps every one of them.
echo "with-server: seeding"
go run ./cmd/seed > "$work/seed.log" 2>&1 || {
	echo "with-server: seeding failed:" >&2
	cat "$work/seed.log" >&2
	exit 1
}

# Finding a port is done by *binding* one, not by probing and hoping. Probing
# with `ss` and then starting is racy when two runs begin together, and the race
# is not benign: both pick the same free port, one server fails to bind, and the
# health check on that port is answered by the other run's server. The losing
# run then drives an instance it does not own and its tests fail when the winner
# tears that instance down -- which is the very hazard this script exists to
# remove, reintroduced.
#
# So readiness means: the socket on this port is held by *my* process. `ss -ltnp`
# reports the owning pid for one of our own processes, which makes that check
# authoritative rather than a guess. A port somebody else holds is not an error
# here, it is the next iteration.
port=""
for candidate in $(seq "$PORT_FIRST" "$PORT_LAST"); do
	if ss -ltn 2>/dev/null | grep -q ":${candidate} "; then
		continue
	fi

	CARAVEL_PORT="$candidate" "$work/caravel" > "$work/server.log" 2>&1 &
	server_pid=$!

	ready=0
	for _ in $(seq 1 100); do
		# Liveness first: a server that died has nothing to say, and asking the
		# port would only get somebody else's answer.
		if ! kill -0 "$server_pid" 2>/dev/null; then
			break
		fi
		if ss -ltnp 2>/dev/null | grep ":${candidate} " | grep -q "pid=${server_pid},"; then
			if curl -fsS -o /dev/null "http://127.0.0.1:$candidate/api/health" 2>/dev/null; then
				ready=1
				break
			fi
		fi
		sleep 0.2
	done

	if [ "$ready" = 1 ]; then
		port="$candidate"
		break
	fi

	kill "$server_pid" 2>/dev/null || true
	wait "$server_pid" 2>/dev/null || true
	server_pid=""

	if grep -qi "address already in use\|address in use" "$work/server.log"; then
		continue
	fi

	echo "with-server: the server did not come up on :$candidate; log follows:" >&2
	cat "$work/server.log" >&2
	exit 1
done

if [ -z "$port" ]; then
	{
		echo "with-server: no free port in ${PORT_FIRST}-${PORT_LAST} — set PORT=… to pick another range"
		echo
		echo "If that is the whole range, it is probably servers abandoned by earlier"
		echo "runs rather than anything of yours. They are the processes whose"
		echo "/proc/PID/exe resolves under /tmp/tmp.*/caravel — match on the exe path,"
		echo "not the process name, which your own \`make dev\` server shares. To list"
		echo "them, and then to clear them:"
		echo
		echo "  for p in /proc/[0-9]*; do case \"\$(readlink -f \$p/exe 2>/dev/null)\" in"
		echo "    /tmp/tmp.*/caravel) echo \"\${p#/proc/}\";; esac; done"
		echo
		echo "  ... | xargs -r kill"
	} >&2
	exit 1
fi

echo "with-server: an isolated instance is up on :$port"

# The capability check CI used to do, moved here so it guards local runs too. A
# typo in the env block above would otherwise turn assist.spec.js's three tests
# into three silent skips, and the run would still be green.
curl -fsS -c "$work/cookies" -X POST "http://127.0.0.1:$port/api/auth/login" \
	-H 'Content-Type: application/json' \
	-d '{"username":"demo","password":"demo1234"}' > /dev/null
if ! curl -fsS -b "$work/cookies" "http://127.0.0.1:$port/api/auth/me" | grep -q '"assist":true'; then
	echo "with-server: the server came up without the assistant enabled; assist.spec.js would skip" >&2
	exit 1
fi

export CARAVEL_TEST_URL="http://127.0.0.1:$port"

# Deliberately not `exec`: this shell has to survive the command so that the
# traps above still exist while the tests run, and so cleanup happens when they
# finish or are killed. Nothing defends against `kill -9` of this process
# itself, which is what the no-free-port message above is written for.
# Backgrounded and waited on, rather than run in the foreground: bash does not
# run a trap while a foreground child is running, so a TERM arriving mid-run
# would sit unhandled until the tests finished on their own -- which is exactly
# the case that used to leak. `wait` is interruptible, so the traps fire at
# once. `|| status=$?` because set -e would otherwise exit before the status
# could be captured.
status=0
"$@" &
cmd_pid=$!
wait "$cmd_pid" || status=$?
cmd_pid=""
cleanup
exit "$status"
