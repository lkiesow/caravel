#!/usr/bin/env python3
"""Checks that every CARAVEL_* variable the compose files and the README name is
one the app actually reads, and that every one it reads is documented.

Run by `make ci`.

This exists because of a real mistake, twice over. Stage 14 Milestone 5 removed
CARAVEL_OPEN_SIGNUP - registration became a runtime setting - and the README
kept documenting it for four stages, describing a first-run sequence that could
not work. Stage 18 Milestone 4 then nearly copied that variable into both
compose files, where it would have looked authoritative and done nothing. A
setting that is silently ignored is worse than one that fails loudly, and
neither the compiler nor any test can see it.

Deliberately dumb: a regex for `"CARAVEL_..."` in Go source is the set the app
reads, and the same pattern in the docs and the compose files is the set it
promises. No parsing of Go or YAML - the names are what matter.
"""

import pathlib
import re
import sys

REPO = pathlib.Path(__file__).resolve().parent.parent
VAR = re.compile(r"CARAVEL_[A-Z_]+")


def read_by_the_app():
    """Every CARAVEL_* name that appears as a Go string literal."""
    found = set()
    for directory in ("internal", "cmd"):
        for path in (REPO / directory).rglob("*.go"):
            for match in re.finditer(r'"(CARAVEL_[A-Z_]+)"', path.read_text()):
                found.add(match.group(1))
    return found


def named_in(path, pattern):
    text = (REPO / path).read_text()
    return {m.group(1) for m in re.finditer(pattern, text)}


def main():
    app = read_by_the_app()
    if not app:
        sys.exit("check_env_vars: found no CARAVEL_* variables in the Go source at all — this script is broken, not the tree")

    problems = []

    # Compose files: a variable set here that nothing reads is a setting the
    # operator believes in and the server ignores. Comments are stripped first,
    # since both files discuss the removed variable on purpose.
    for compose in ("docker-compose.yml", "docker-compose.postgres.yml"):
        text = (REPO / compose).read_text()
        uncommented = "\n".join(line.split("#", 1)[0] for line in text.splitlines())
        for name in sorted({m.group(0) for m in VAR.finditer(uncommented)}):
            if name not in app:
                problems.append(f"{compose} sets {name}, which the app never reads")

    # The README's configuration tables.
    documented = named_in("README.md", r"`(CARAVEL_[A-Z_]+)`")
    for name in sorted(documented - app):
        problems.append(f"README.md documents {name}, which the app never reads")
    for name in sorted(app - documented):
        problems.append(f"{name} is read by the app but not documented in README.md")

    if problems:
        print("check_env_vars: found problems:", file=sys.stderr)
        for problem in problems:
            print(f"  - {problem}", file=sys.stderr)
        sys.exit(1)

    print(f"env vars checked: {len(app)} read by the app, all documented, none invented by the compose files")


if __name__ == "__main__":
    main()
