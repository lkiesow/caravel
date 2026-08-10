#!/usr/bin/env python3
"""Checks that every locale file in web/locales/ defines the same set of
keys. Locale files are flat, dotted-key JSON objects. New languages are
picked up automatically (no per-language list to maintain here) — just drop
a new web/locales/<code>.json file and this script checks it the same way.
Run manually or via CI; exits non-zero and prints a diff if any file is
missing keys another file has."""

import json
import sys
from pathlib import Path

LOCALES_DIR = Path(__file__).resolve().parent.parent / "web" / "locales"


def main():
    locale_files = sorted(LOCALES_DIR.glob("*.json"))
    if len(locale_files) < 2:
        print(f"found fewer than 2 locale files in {LOCALES_DIR}, nothing to compare")
        return 0

    keys_by_file = {}
    for path in locale_files:
        with path.open(encoding="utf-8") as f:
            keys_by_file[path.name] = set(json.load(f).keys())

    all_keys = set()
    for keys in keys_by_file.values():
        all_keys |= keys

    ok = True
    for name, keys in keys_by_file.items():
        missing = sorted(all_keys - keys)
        if missing:
            ok = False
            print(f"{name} is missing {len(missing)} key(s):")
            for key in missing:
                print(f"  - {key}")

    if ok:
        print(f"{len(locale_files)} locale files checked, {len(all_keys)} keys, all in sync")
        return 0
    return 1


if __name__ == "__main__":
    sys.exit(main())
