#!/usr/bin/env python3
"""Compares two SQLite databases structurally: tables, columns (name, type,
NOT NULL, default, primary-key position), indexes and their columns, and foreign
keys with their actions.

    python3 scripts/compare_sqlite_schema.py old.db new.db

Written for the Stage 18 migration squash, where the question was "does one
0001_init produce exactly what the twelve-file chain produced?" and a text diff
of `.schema` cannot answer it: the squashed file carries its comments inside the
CREATE TABLE bodies, so the dumps differ everywhere while the schema does not.

Kept because the same question comes back the next time migrations are folded.
"""

import sqlite3
import sys


def schema(path):
    conn = sqlite3.connect(path)
    conn.row_factory = sqlite3.Row
    out = {}
    tables = [
        r["name"]
        for r in conn.execute(
            "SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%'"
        )
        # schema_migrations is the migrator's bookkeeping, and its *contents*
        # differ by design (version 12 versus version 1). The table itself is
        # compared; the row is not.
    ]
    for table in sorted(tables):
        columns = [
            (r["name"], r["type"].upper(), bool(r["notnull"]), r["dflt_value"], r["pk"])
            for r in conn.execute(f"PRAGMA table_info({table})")
        ]
        indexes = {}
        for idx in conn.execute(f"PRAGMA index_list({table})"):
            cols = [r["name"] for r in conn.execute(f"PRAGMA index_info({idx['name']})")]
            # Auto-indexes are named sqlite_autoindex_* and follow from UNIQUE
            # declarations, so they are compared by shape rather than by name.
            key = "auto" if idx["name"].startswith("sqlite_autoindex") else idx["name"]
            indexes.setdefault(key, []).append((bool(idx["unique"]), tuple(cols)))
        fks = sorted(
            (r["table"], r["from"], r["to"], r["on_update"], r["on_delete"])
            for r in conn.execute(f"PRAGMA foreign_key_list({table})")
        )
        out[table] = {"columns": columns, "indexes": indexes, "foreign_keys": fks}
    conn.close()
    return out


def main():
    if len(sys.argv) != 3:
        sys.exit(__doc__)
    old, new = schema(sys.argv[1]), schema(sys.argv[2])

    problems = []
    for table in sorted(set(old) | set(new)):
        if table not in new:
            problems.append(f"table {table} is missing from {sys.argv[2]}")
            continue
        if table not in old:
            problems.append(f"table {table} is new in {sys.argv[2]}")
            continue
        for aspect in ("columns", "indexes", "foreign_keys"):
            if old[table][aspect] != new[table][aspect]:
                problems.append(f"{table}.{aspect} differs:\n    old: {old[table][aspect]}\n    new: {new[table][aspect]}")

    if problems:
        print(f"schemas differ ({len(problems)} difference(s)):")
        for p in problems:
            print("  - " + p)
        sys.exit(1)
    print(f"identical: {len(old)} tables, same columns, indexes and foreign keys")


if __name__ == "__main__":
    main()
