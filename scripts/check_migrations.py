#!/usr/bin/env python3
"""Guards the migration chain against the one mistake that cannot be undone.

Stage 18 squashed twelve migration pairs into a single `0001_init` per dialect.
That was safe because nobody had a deployed database; any database created
before it refuses to start with `no migration found for version 12`. The same
reasoning says it must never happen again now that images are published — a
second squash would silently brick every instance created since the first one,
and there was nothing stopping it.

Three checks, in increasing order of how much they need:

1. **Pairing and numbering.** Every version has both an `.up.sql` and a
   `.down.sql`, versions start at 1 and run contiguously with no gaps. A gap
   means a migration was deleted; golang-migrate would not notice until a
   database sat in the hole.
2. **Dialect parity.** `sqlite/` and `postgres/` define the same versions under
   the same names. A migration written for one dialect and forgotten for the
   other compiles, passes every SQLite test, and fails on the other dialect at
   startup — the exact shape of the bug Stage 18 Milestone 3 found.
3. **The chain only ever grows.** Compared against a base commit, no version
   that existed may have been removed or renamed. This is the squash guard.

Check 3 needs git history for the base. In a shallow clone there is none, and
the script says so and skips it rather than reporting a pass it did not earn —
the other two checks still run. Pass a base explicitly with
`--base <ref>`; the default tries origin/main, then main, then HEAD~1.
"""

import re
import subprocess
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
MIGRATIONS = ROOT / "internal" / "db" / "migrations"
DIALECTS = ("sqlite", "postgres")
NAME_RE = re.compile(r"^(\d+)_(.+)\.(up|down)\.sql$")


def read_dialect(root, dialect, names):
    """name -> {version: (name, {up, down})} from a list of filenames."""
    found = {}
    problems = []
    for name in names:
        m = NAME_RE.match(name)
        if not m:
            problems.append(f"{dialect}/{name}: not a <version>_<name>.<up|down>.sql file")
            continue
        version, label, direction = int(m.group(1)), m.group(2), m.group(3)
        entry = found.setdefault(version, {"label": label, "directions": set()})
        if entry["label"] != label:
            problems.append(
                f"{dialect}: version {version:04d} has two names, "
                f"{entry['label']!r} and {label!r}"
            )
        entry["directions"].add(direction)
    return found, problems


def check_pairing_and_numbering(found, dialect):
    problems = []
    for version, entry in sorted(found.items()):
        missing = {"up", "down"} - entry["directions"]
        for direction in sorted(missing):
            problems.append(
                f"{dialect}: {version:04d}_{entry['label']} has no .{direction}.sql — "
                "every migration needs both directions"
            )
    versions = sorted(found)
    expected = list(range(1, len(versions) + 1))
    if versions != expected:
        gaps = sorted(set(expected) - set(versions))
        problems.append(
            f"{dialect}: versions are {versions}, expected {expected}"
            + (f" — missing {gaps}, so a migration was deleted" if gaps else "")
        )
    return problems


def git(*args):
    return subprocess.run(
        ["git", *args], cwd=ROOT, capture_output=True, text=True, check=False
    )


def resolve_base(explicit):
    candidates = [explicit] if explicit else ["origin/main", "main", "HEAD~1"]
    for ref in candidates:
        if git("rev-parse", "--verify", "--quiet", f"{ref}^{{commit}}").returncode == 0:
            return ref
    return None


def base_versions(base, dialect):
    res = git("ls-tree", "--name-only", f"{base}:internal/db/migrations/{dialect}")
    if res.returncode != 0:
        return None
    found, _ = read_dialect(None, dialect, res.stdout.split())
    return {v: e["label"] for v, e in found.items()}


def main():
    explicit = None
    argv = sys.argv[1:]
    if argv and argv[0] == "--base":
        if len(argv) < 2:
            print("usage: check_migrations.py [--base <ref>]", file=sys.stderr)
            return 2
        explicit = argv[1]

    problems = []
    current = {}
    for dialect in DIALECTS:
        directory = MIGRATIONS / dialect
        if not directory.is_dir():
            problems.append(f"{dialect}: {directory} does not exist")
            continue
        names = sorted(p.name for p in directory.iterdir() if p.is_file())
        found, parse_problems = read_dialect(directory, dialect, names)
        problems.extend(parse_problems)
        problems.extend(check_pairing_and_numbering(found, dialect))
        current[dialect] = {v: e["label"] for v, e in found.items()}

    # 2. The two dialects must describe the same chain.
    if len(current) == len(DIALECTS):
        a, b = DIALECTS
        if current[a] != current[b]:
            only_a = set(current[a]) - set(current[b])
            only_b = set(current[b]) - set(current[a])
            for v in sorted(only_a):
                problems.append(f"version {v:04d}_{current[a][v]} exists for {a} but not {b}")
            for v in sorted(only_b):
                problems.append(f"version {v:04d}_{current[b][v]} exists for {b} but not {a}")
            for v in sorted(set(current[a]) & set(current[b])):
                if current[a][v] != current[b][v]:
                    problems.append(
                        f"version {v:04d} is {current[a][v]!r} for {a} "
                        f"but {current[b][v]!r} for {b}"
                    )

    # 3. The squash guard.
    base = resolve_base(explicit)
    if base is None:
        print(
            "check-migrations: no base commit available (shallow clone?), so the "
            "\"chain only grows\" check did not run"
        )
    else:
        checked = 0
        for dialect in DIALECTS:
            was = base_versions(base, dialect)
            if was is None:
                continue
            checked += 1
            now = current.get(dialect, {})
            for version, label in sorted(was.items()):
                if version not in now:
                    problems.append(
                        f"{dialect}: version {version:04d}_{label} existed at {base} and is gone. "
                        "Removing a migration bricks every database that has already run it — "
                        "add a new migration instead."
                    )
                elif now[version] != label:
                    problems.append(
                        f"{dialect}: version {version:04d} was {label!r} at {base} and is now "
                        f"{now[version]!r}. Renaming a migration changes nothing for a database "
                        "that already ran it, and changes the file for one that has not."
                    )
        if checked:
            print(f"check-migrations: compared against {base}")

    if problems:
        print(f"check-migrations: {len(problems)} problem(s):", file=sys.stderr)
        for p in problems:
            print(f"  - {p}", file=sys.stderr)
        return 1

    count = len(current.get(DIALECTS[0], {}))
    print(
        f"check-migrations: {count} migration(s) per dialect, both dialects agree, "
        "chain intact"
    )
    return 0


if __name__ == "__main__":
    sys.exit(main())
