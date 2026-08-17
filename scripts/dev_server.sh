#!/usr/bin/env bash
# Dev-server lifecycle helper: find/kill whatever holds the port, restart, and
# prove the process now listening is actually the code you just wrote.
#
# Why this exists. Stage 07 Milestone 3 nearly recorded a false pass: the API
# kept returning 200 for input its new validation rejected, because a stale
# server from an earlier session still held :8080 and `make dev` had died behind
# it with "address already in use" — a background start whose failure is easy to
# miss. The obvious cleanup doesn't work either: `pkill -f "go run ./cmd/caravel"`
# does NOT find the process actually listening, because `go run` compiles to a
# temp binary and execs it, so the listener's command line is a
# ~/.cache/go-build/... path with no mention of ./cmd/caravel.
#
# So everything here works off the *port*, never a process name, and `restart`
# can assert a marker string is present in the running binary — which is the
# check that would have caught that false pass.
#
# Frontend edits are immune to all of this (web/ is served live from disk under
# CARAVEL_WEB_DIR), which is exactly why it's easy to forget for Go changes.
#
# Usage:
#   scripts/dev_server.sh check-port          # exit 1 if the port is taken (used by `make dev`)
#   scripts/dev_server.sh restart             # kill by port, start, wait for health
#   scripts/dev_server.sh restart MARKER=str  # ...and assert the new binary contains `str`
#   scripts/dev_server.sh check-marker str    # assert WITHOUT restarting — i.e. "is the
#                                             # server I'm testing against actually running
#                                             # my change?", the question Stage 07 got wrong
set -euo pipefail

cd "$(dirname "$0")/.."

PORT="${CARAVEL_PORT:-8080}"
HEALTH_URL="http://localhost:${PORT}/api/health"
LOG_DIR=".dev"
LOG_FILE="${LOG_DIR}/server.log"
STARTUP_TIMEOUT=45   # go run has to compile first, which dominates this
SHUTDOWN_TIMEOUT=10

# Every pid listening on PORT, empty if none. Deliberately port-based: see the
# header.
#
# The trailing `|| true` is load-bearing. When nothing is listening, grep matches
# nothing and exits 1; under `set -euo pipefail` that propagates out of the
# command substitution and kills the script — silently, with no output and a bare
# exit 1. So without it this helper breaks in exactly the case it most needs to
# work: a free port, i.e. a fresh machine with no server running.
listening_pids() {
	ss -lptnH "sport = :${PORT}" 2>/dev/null |
		grep -o 'pid=[0-9]*' | cut -d= -f2 | sort -u || true
}

# Separate from listening_pids(): the port can be held by a process whose pid ss
# won't reveal (another user's), and "busy but unattributable" must not be
# mistaken for "free".
port_is_busy() {
	[ -n "$(ss -lntH "sport = :${PORT}" 2>/dev/null)" ]
}

describe_pid() {
	local pid="$1"
	printf 'pid %s (%s)' "$pid" "$(readlink "/proc/$pid/exe" 2>/dev/null || echo 'exe unreadable')"
}

cmd_check_port() {
	local pids
	pids="$(listening_pids)"
	if [ -z "$pids" ] && ! port_is_busy; then
		return 0
	fi
	echo "dev: port ${PORT} is already in use — the server would fail to bind." >&2
	if [ -n "$pids" ]; then
		for pid in $pids; do
			echo "dev:   held by $(describe_pid "$pid")" >&2
		done
		echo "dev: run 'make dev-restart' to replace it (kills by port, not by name)." >&2
	else
		# Listener owned by another user, or ss withheld the pid.
		echo "dev:   could not identify the owning process (not ours?)." >&2
	fi
	return 1
}

stop_port() {
	local pids
	pids="$(listening_pids)"
	if [ -z "$pids" ]; then
		if port_is_busy; then
			echo "dev-restart: port ${PORT} is busy but its owner can't be identified — refusing to guess." >&2
			return 1
		fi
		echo "dev-restart: port ${PORT} was already free"
		return 0
	fi

	for pid in $pids; do
		echo "dev-restart: stopping $(describe_pid "$pid")"
	done

	# TERM first so the server can shut down cleanly, then escalate.
	kill -TERM $pids 2>/dev/null || true
	local waited=0
	while [ "$waited" -lt "$SHUTDOWN_TIMEOUT" ] && port_is_busy; do
		sleep 1
		waited=$((waited + 1))
	done

	if port_is_busy; then
		echo "dev-restart: port still busy after ${SHUTDOWN_TIMEOUT}s, sending KILL"
		kill -KILL $(listening_pids) 2>/dev/null || true
		waited=0
		while [ "$waited" -lt 5 ] && port_is_busy; do
			sleep 1
			waited=$((waited + 1))
		done
	fi

	if port_is_busy; then
		echo "dev-restart: could not free port ${PORT}" >&2
		return 1
	fi
	echo "dev-restart: port ${PORT} free"
}

start_server() {
	mkdir -p "$LOG_DIR"
	: > "$LOG_FILE"
	# setsid + nohup so the server outlives this script and make's process group.
	CARAVEL_WEB_DIR=web setsid nohup go run ./cmd/caravel >>"$LOG_FILE" 2>&1 &
	local runner=$!
	echo "dev-restart: starting (go run pid ${runner}, compiling…), logging to ${LOG_FILE}"

	local waited=0
	while [ "$waited" -lt "$STARTUP_TIMEOUT" ]; do
		if curl -fsS -o /dev/null "$HEALTH_URL" 2>/dev/null; then
			return 0
		fi
		# Fail fast if the runner died rather than burning the whole timeout.
		if ! kill -0 "$runner" 2>/dev/null && ! port_is_busy; then
			echo "dev-restart: server exited during startup. Last lines of ${LOG_FILE}:" >&2
			tail -n 20 "$LOG_FILE" >&2
			return 1
		fi
		sleep 1
		waited=$((waited + 1))
	done

	echo "dev-restart: ${HEALTH_URL} did not answer within ${STARTUP_TIMEOUT}s. Last lines of ${LOG_FILE}:" >&2
	tail -n 20 "$LOG_FILE" >&2
	return 1
}

# Asserts the binary now listening contains `marker`. Reads /proc/<pid>/exe with
# `grep -a` rather than `strings` so this needs no binutils.
#
# Pick a marker the code actually USES — a new error message, a changed response
# body, a log line. A synthetic `const devMarker = "..."` that nothing references
# does not survive into the binary (Go folds constants at compile time and the
# linker drops unreferenced data), so it reports "not found" against a server
# that genuinely does have your change. Verified the hard way while building
# this: an unused const failed even immediately after a rebuild, while a string
# added to the /api/health response body was found straight away.
assert_marker() {
	local marker="$1" pid pids
	pids="$(listening_pids)"
	pid="$(echo "$pids" | head -1)"
	if [ -z "$pid" ]; then
		echo "dev-marker: nothing listening on ${PORT}, cannot check marker" >&2
		return 1
	fi
	if ! grep -aq -- "$marker" "/proc/$pid/exe" 2>/dev/null; then
		echo "dev-marker: ${marker@Q} NOT found in the running binary ($(describe_pid "$pid"))." >&2
		echo "dev-marker: the process on ${PORT} is not running the code you just wrote." >&2
		return 1
	fi
	echo "dev-marker: ${marker@Q} confirmed present in $(describe_pid "$pid")"
}

cmd_restart() {
	local marker=""
	for arg in "$@"; do
		case "$arg" in
			MARKER=*) marker="${arg#MARKER=}" ;;
			*) echo "dev-restart: unknown argument ${arg@Q}" >&2; return 2 ;;
		esac
	done

	stop_port
	start_server

	for pid in $(listening_pids); do
		echo "dev-restart: healthy — now serving from $(describe_pid "$pid")"
	done

	if [ -n "$marker" ]; then
		assert_marker "$marker"
	fi
	echo "dev-restart: ${HEALTH_URL} OK"
}

cmd_check_marker() {
	if [ $# -ne 1 ] || [ -z "$1" ]; then
		echo "usage: $0 check-marker <string>" >&2
		return 2
	fi
	assert_marker "$1"
}

case "${1:-}" in
	check-port)   shift; cmd_check_port "$@" ;;
	restart)      shift; cmd_restart "$@" ;;
	check-marker) shift; cmd_check_marker "$@" ;;
	*)
		echo "usage: $0 {check-port|restart [MARKER=string]|check-marker <string>}" >&2
		exit 2
		;;
esac
