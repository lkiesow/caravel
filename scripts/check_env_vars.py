#!/usr/bin/env python3
"""Checks that every CARAVEL_* variable the compose files and the documentation
name is one the app actually reads, and that every one it reads is documented.

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

Stage 18 Milestone 10 moved the configuration tables out of the README and onto
the documentation site, so the documented set is now gathered from every page
under docs/ as well as from the README. Which page a variable is documented on
does not matter; being documented nowhere does.
"""

import pathlib
import re
import sys

REPO = pathlib.Path(__file__).resolve().parent.parent
VAR = re.compile(r"CARAVEL_[A-Z_]+")

# Names the documentation is allowed to mention *in order to say they are gone*.
# The first-run page tells the reader CARAVEL_OPEN_SIGNUP no longer exists, which
# is genuinely useful to anybody following an old guide -- and which this script
# would otherwise flag as documenting something the app never reads. Same
# exemption the compose files get by having their comments stripped.
#
# The cost is that for these names only, the "documented but never read" half of
# the check is off. Deleting an entry here is how it comes back on, so if one of
# these is ever reintroduced, remove it from this list.
DOCUMENTED_AS_REMOVED = {"CARAVEL_OPEN_SIGNUP"}


def read_by_the_app():
    """Every CARAVEL_* name that appears as a Go string literal."""
    found = set()
    for directory in ("internal", "cmd"):
        for path in (REPO / directory).rglob("*.go"):
            for match in re.finditer(r'"(CARAVEL_[A-Z_]+)"', path.read_text()):
                found.add(match.group(1))
    return found


def documented_names():
    """Every CARAVEL_* named in backticks in the README or anywhere under docs/.

    Backticks rather than a bare name, so prose mentioning a variable in passing
    does not count as documenting it -- the tables and the code spans are the
    promise.
    """
    pattern = r"`(CARAVEL_[A-Z_]+)`"
    paths = [REPO / "README.md", *sorted((REPO / "docs").rglob("*.md"))]
    found = set()
    for path in paths:
        found |= {m.group(1) for m in re.finditer(pattern, path.read_text())}
    return found


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

    # The configuration pages, plus whatever the README still names.
    documented = documented_names()
    for name in sorted(documented - app - DOCUMENTED_AS_REMOVED):
        problems.append(f"the docs document {name}, which the app never reads")
    for name in sorted(app - documented):
        problems.append(f"{name} is read by the app but documented nowhere under docs/ or in README.md")

    if problems:
        print("check_env_vars: found problems:", file=sys.stderr)
        for problem in problems:
            print(f"  - {problem}", file=sys.stderr)
        sys.exit(1)

    print(f"env vars checked: {len(app)} read by the app, all documented, none invented by the compose files")


if __name__ == "__main__":
    main()
