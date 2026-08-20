#!/usr/bin/env bash
# Syntax-checks the app's JavaScript in two passes, because the tree contains
# both module kinds and the parse mode has to match how the browser loads each:
#
#   web/js/**.js  — ES modules (see below)
#   web/*.js      — classic scripts. Today that is only the service worker,
#                   which app.js registers as navigator.serviceWorker
#                   .register("/sw.js") with no {type: "module"}. It lived
#                   outside web/js and so was checked by nothing at all: a
#                   syntax error in it reached the browser with `make ci`
#                   green, the same hole Stage 08 Milestone 1 closed one
#                   directory over.
#
# The parse mode is the whole point. `node --check <file>` treats the file as a
# CommonJS script, where `<!--` legally opens an HTML-like comment (Annex B) and
# swallows the rest of the line. The app loads all of these files as ES modules,
# where an HTML comment is a syntax error — so the old check could pass on a
# file the browser refuses to load. That happened for real: a comment containing
# a stray backtick broke the Documents tab while CI stayed green.
#
# Passing the file on stdin with --input-type=module makes the mode explicit.
# The cost is that Node then reports errors against "[stdin]" with no filename,
# which is why each failure is echoed with its path below.
#
# Run via `make check-js`, and from .github/workflows/ci.yml.
set -euo pipefail

cd "$(dirname "$0")/.."

count=0
failed=0

# -print0 / read -d '' so paths containing spaces or newlines can't split. The
# per-command `<"$file"` redirect gives node its own stdin, so it doesn't
# consume the file list the loop is reading.
while IFS= read -r -d '' file; do
	count=$((count + 1))
	if ! node --input-type=module --check <"$file"; then
		echo "check-js: $file is not a valid ES module (see the [stdin] trace above)" >&2
		failed=$((failed + 1))
	fi
done < <(find web/js -name '*.js' -print0)

if [ "$count" -eq 0 ]; then
	echo "check-js: found no .js files under web/js — wrong directory?" >&2
	exit 1
fi

# Second pass, script mode — stdin again, and for the same reason the module
# pass uses it: the mode has to be stated, not inferred. `node --check <path>`
# looks like the script-mode form but is not one on Node 22+, which detects
# module syntax and silently re-parses as ESM: an `import` statement in sw.js
# passed that check while the browser, loading it as a classic script, would
# refuse it outright. --input-type=commonjs rejects it (verified both ways).
# Its own counter and its own zero guard, so moving sw.js under web/js — where
# the module pass would cover it — fails loudly here instead of silently
# checking nothing.
script_count=0
script_failed=0

while IFS= read -r -d '' file; do
	script_count=$((script_count + 1))
	if ! node --input-type=commonjs --check <"$file"; then
		echo "check-js: $file is not a valid classic script (see the [stdin] trace above)" >&2
		script_failed=$((script_failed + 1))
	fi
done < <(find web -maxdepth 1 -name '*.js' -print0)

if [ "$script_count" -eq 0 ]; then
	echo "check-js: found no top-level .js files in web/ — has sw.js moved? Point this pass at it." >&2
	exit 1
fi

if [ "$failed" -ne 0 ] || [ "$script_failed" -ne 0 ]; then
	echo "check-js: $failed of $count module(s) and $script_failed of $script_count script(s) failed" >&2
	exit 1
fi

echo "check-js: $count files checked as ES modules and $script_count as classic scripts, all valid"
