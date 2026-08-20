#!/usr/bin/env bash
# Prints the build's identity: the short git SHA, plus "-dirty" when the tree has
# uncommitted changes.
#
# One script rather than the same two git commands in both the Makefile and
# scripts/dev_server.sh, because those two would then be free to disagree — and
# a version string is only useful if every way of starting the server produces
# it the same way. Stamped into the binary via -ldflags; see internal/buildinfo.
#
# Never fails the caller: outside a git checkout (a source tarball, a container
# build with no .git) it prints "unknown", which is the honest answer and still
# builds.
set -uo pipefail

sha="$(git rev-parse --short HEAD 2>/dev/null)" || sha=""
if [ -z "$sha" ]; then
	echo unknown
	exit 0
fi

dirty=""
if [ -n "$(git status --porcelain 2>/dev/null)" ]; then
	dirty="-dirty"
fi

echo "${sha}${dirty}"
