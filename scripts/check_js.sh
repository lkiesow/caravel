#!/usr/bin/env bash
# Syntax-checks every file under web/js as an ES *module*.
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

if [ "$failed" -ne 0 ]; then
	echo "check-js: $failed of $count file(s) failed" >&2
	exit 1
fi

echo "check-js: $count files checked as ES modules, all valid"
