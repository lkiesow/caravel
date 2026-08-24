#!/usr/bin/env bash
# Proves a test would actually have caught the bug.
#
#   scripts/without.sh <file>... -- <command>...
#
# Temporarily reverts the named files' uncommitted changes, runs the command,
# restores them, and EXITS NON-ZERO IF THE COMMAND PASSED without the change.
# That inversion is the whole point: it turns "the test passes" into "the test
# would have caught this".
#
#   scripts/without.sh internal/httpapi/itinerary.go -- go test ./internal/httpapi/
#   scripts/without.sh --restart internal/httpapi/items.go -- make test-ui
#
# Why it exists: this dance was hand-rolled five times in Stage 07 (date
# validation, delete-day tests, day ordering, the accessible-name sweep, the
# heading audit) and five more times in Stage 08 Milestone 5. It is also the step
# easiest to skip — which is exactly when a vacuous test slips through. Stage 08
# Milestone 5 nearly recorded one: stripping an aria-label from a button that also
# has text content changed nothing, and would have looked like proof if the "why"
# had gone unexamined.
#
# It distinguishes a command that FAILED from one that NEVER RAN — a compile
# error, a grep that matched no tests, a command not found. Because the exit
# code is inverted here, those would otherwise read as "OK, your test would have
# caught it" with nothing having executed; see the verdict section below for the
# two times that actually happened.
#
# --restart re-runs the dev server between stash and command. Needed whenever Go
# files are reverted and the command talks to the running server: web/ is served
# live from disk, but Go changes live in a compiled binary, so without a restart
# the command tests the OLD code and the result is meaningless. See
# scripts/dev_server.sh for the full trap.
set -euo pipefail

cd "$(dirname "$0")/.."

restart_server=0
files=()
command_argv=()
saw_separator=0

for arg in "$@"; do
	if [ "$saw_separator" = "1" ]; then
		command_argv+=("$arg")
	elif [ "$arg" = "--" ]; then
		saw_separator=1
	elif [ "$arg" = "--restart" ]; then
		restart_server=1
	elif [ "$arg" = "--help" ] || [ "$arg" = "-h" ]; then
		sed -n '2,25p' "$0" | sed 's|^# \{0,1\}||'
		exit 0
	else
		files+=("$arg")
	fi
done

usage() {
	echo "usage: scripts/without.sh [--restart] <file>... -- <command>..." >&2
	exit 2
}

[ "$saw_separator" = "1" ] || { echo "without: missing '--' separator" >&2; usage; }
[ "${#files[@]}" -gt 0 ] || { echo "without: no files named before '--'" >&2; usage; }
[ "${#command_argv[@]}" -gt 0 ] || { echo "without: no command given after '--'" >&2; usage; }

# --- guards ----------------------------------------------------------------
# Each of these prevents a *confident wrong answer*, which is worse than an
# error: a run that reverts nothing and reports "your test would have caught it"
# is precisely the false assurance this tool exists to eliminate.

for file in "${files[@]}"; do
	if [ ! -e "$file" ]; then
		echo "without: $file does not exist" >&2
		exit 2
	fi
	if ! git ls-files --error-unmatch -- "$file" >/dev/null 2>&1; then
		echo "without: $file is not tracked by git, so there is no committed state to revert to." >&2
		echo "without: commit it first, or name the tracked file whose change you want to test." >&2
		exit 2
	fi
done

# The critical guard: reverting a file with no changes is a no-op, so the command
# runs against the code exactly as it is. It would then "pass without the change"
# only in the trivial sense, and the tool would report a vacuous test that isn't.
unchanged=()
for file in "${files[@]}"; do
	if git diff --quiet -- "$file" && git diff --cached --quiet -- "$file"; then
		unchanged+=("$file")
	fi
done
if [ "${#unchanged[@]}" -gt 0 ]; then
	echo "without: no uncommitted changes in: ${unchanged[*]}" >&2
	echo "without: there would be nothing to revert, so the result would be meaningless." >&2
	echo "without: this tool tests an UNCOMMITTED change; for a committed one, revert it first." >&2
	exit 2
fi

# Refuse a dirty index anywhere: `git stash push -- <paths>` restores through
# `git stash pop`, which can rearrange staged state, and swallowing someone
# else's staged work is not a risk worth taking for a convenience script.
if ! git diff --cached --quiet; then
	echo "without: you have staged changes. Restoring goes through 'git stash pop', which can" >&2
	echo "without: disturb the index, so this refuses rather than risk your staged work." >&2
	echo "without: unstage first (git restore --staged .) and re-run." >&2
	exit 2
fi

# Reverting Go files without restarting is the one way this tool produces a
# confident WRONG answer rather than an error: web/ is served live from disk, but
# Go code lives in the compiled binary, so a running server keeps answering from
# the un-reverted build and the command passes for the wrong reason. Verified: the
# same check reports VACUOUS without --restart and OK with it.
if [ "$restart_server" = "0" ]; then
	go_files=()
	for file in "${files[@]}"; do
		case "$file" in *.go) go_files+=("$file") ;; esac
	done
	if [ "${#go_files[@]}" -gt 0 ] && ! scripts/dev_server.sh check-port >/dev/null 2>&1; then
		echo "without: WARNING — you are reverting Go files (${go_files[*]}) while a dev server is" >&2
		echo "without: running, and did not pass --restart. If the command talks to that server, it" >&2
		echo "without: will test the OLD binary and the verdict will be wrong. Re-run with --restart" >&2
		echo "without: if so; ignore this if the command compiles its own code (e.g. 'go test')." >&2
		echo >&2
	fi
fi

# --- snapshot, so the restore can be verified rather than assumed ----------
declare -A before_hash
for file in "${files[@]}"; do
	before_hash["$file"]="$(git hash-object -- "$file")"
done

stash_sha=""
restored=0

restore() {
	# Runs on every exit path, including Ctrl-C, so an interrupted run can never
	# leave the tree stashed.
	if [ -z "$stash_sha" ] || [ "$restored" = "1" ]; then
		return 0
	fi
	restored=1

	# Find the entry by SHA rather than assuming stash@{0}: the command just ran
	# arbitrary code, which may have created stashes of its own.
	local ref="" i=0
	while read -r line; do
		if [ "$line" = "$stash_sha" ]; then
			ref="stash@{$i}"
			break
		fi
		i=$((i + 1))
	done < <(git stash list --format="%H")

	if [ -z "$ref" ]; then
		echo "without: PANIC — cannot find the stash entry $stash_sha to restore." >&2
		echo "without: your changes are still in the stash; recover with:" >&2
		echo "without:   git stash list && git stash apply $stash_sha" >&2
		return 1
	fi

	if ! git stash pop --quiet "$ref"; then
		echo "without: PANIC — 'git stash pop $ref' failed. Your changes are safe in the stash:" >&2
		echo "without:   git stash apply $stash_sha" >&2
		return 1
	fi

	# Verify rather than trust: confirm every file is byte-identical to what it
	# was before we touched it.
	local bad=0
	for f in "${files[@]}"; do
		if [ "$(git hash-object -- "$f")" != "${before_hash[$f]}" ]; then
			echo "without: PANIC — $f did not come back identical after restore." >&2
			bad=1
		fi
	done
	if [ "$bad" = "1" ]; then
		echo "without: check 'git stash list' — the original content is at $stash_sha" >&2
		return 1
	fi

	echo "without: restored ${#files[@]} file(s)"
	if [ "$restart_server" = "1" ]; then
		scripts/dev_server.sh restart >/dev/null && echo "without: dev server restarted with your change back"
	fi
}
interrupted() {
	# A verdict must never be printed for a run that did not finish. Falling
	# through to the normal path after Ctrl-C reported "VACUOUS — the command
	# PASSED" for a command that was killed mid-flight, which is precisely the
	# kind of confident-but-wrong answer this tool exists to eliminate.
	echo >&2
	echo "without: INTERRUPTED — no verdict. Restoring your files." >&2
	restore
	exit 130
}
trap restore EXIT
trap interrupted INT TERM

# --- run --------------------------------------------------------------------
echo "without: reverting ${files[*]}"
git stash push --quiet --message "without.sh: temporary revert" -- "${files[@]}"
stash_sha="$(git rev-parse stash@{0})"

if [ "$restart_server" = "1" ]; then
	echo "without: restarting the dev server so it runs the reverted code"
	scripts/dev_server.sh restart >/dev/null
fi

echo "without: running: ${command_argv[*]}"
echo "---------------------------------------------------------------"
# Output is teed rather than just streamed: the verdict below has to read it to
# tell "the command failed" from "the command never ran", and the reader gets
# the tail either way.
output_log="$(mktemp)"
set +e
"${command_argv[@]}" 2>&1 | tee "$output_log"
command_status="${PIPESTATUS[0]}"
set -e
echo "---------------------------------------------------------------"

# restore() also runs via the EXIT trap; calling it here means the verdict below
# is printed after the tree is already back to normal.
restore
trap - EXIT INT TERM

# --- the verdict ------------------------------------------------------------
#
# The hard part is not "did it exit non-zero". It is telling a command that
# FAILED from one that NEVER RAN, because this tool inverts the exit code: a
# command that could not run at all exits non-zero and therefore reads as
# "OK — genuinely depends on your change", with not one assertion having
# executed. That has happened twice, and both times the answer was confidently
# wrong:
#
#   Stage 09 Milestone 6 — a typo in a GREP pattern meant Playwright ran no
#     tests at all, and the run was reported as proof.
#   Stage 10 Milestone 5 — reverting the file under test removed a struct the
#     *test* referenced, so `go test` failed to COMPILE. A compile error reads
#     as a test failure in the output, which is what makes this the least
#     likely case to be noticed.
#
# So the signatures of both are checked before the verdict is reached.

inconclusive() {
	echo "without: INCONCLUSIVE — $1" >&2
	echo "without: the command exited $command_status, but it did not run on its own terms," >&2
	echo "without: so it supports no verdict. Last lines of its output:" >&2
	tail -n 15 "$output_log" | sed 's/^/without:   /' >&2
	rm -f "$output_log"
	exit 2
}

# A command killed by a signal (128+n) neither passed nor failed on its merits.
if [ "$command_status" -gt 128 ]; then
	inconclusive "it was killed by a signal"
fi

# 127 is "command not found", 126 is "found but not executable". Neither ran.
if [ "$command_status" -eq 127 ] || [ "$command_status" -eq 126 ]; then
	inconclusive "the command could not be executed at all"
fi

# The Stage 10 case. `go test` prints "[build failed]" and exits 1 exactly like
# a failing test does, so only the output tells them apart.
if grep -qE '\[build failed\]|\[setup failed\]|^# .*\.test\]?$' "$output_log"; then
	inconclusive "the Go package failed to COMPILE, so no test ran"
fi

# The Stage 09 case, plus its go-test equivalent.
if grep -qE '^Error: No tests found|^testing: warning: no tests to run|no test files' "$output_log"; then
	inconclusive "the command selected no tests to run"
fi

rm -f "$output_log"

if [ "$command_status" -eq 0 ]; then
	echo "without: VACUOUS — the command PASSED without your change (exit 0)." >&2
	echo "without: it does not depend on ${files[*]}, so it would not have caught this." >&2
	exit 1
fi

echo "without: OK — the command failed (exit $command_status) without your change,"
echo "without: so it genuinely depends on ${files[*]} and would have caught a regression."
exit 0
