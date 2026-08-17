#!/usr/bin/env python3
"""Edits the locale files under web/locales/ — the write side of the job
check_i18n.py checks.

check_i18n.py verifies every locale defines the same keys. This script is how
you get there: add, update or remove a key across *every* locale in one call,
so the files can't drift out of parity in the first place. Locale files are
discovered by glob, so a third language needs no change here.

    # add a new key to every locale, next to a related one
    scripts/i18n.py set trips.empty.hint --after trips.empty \\
        en="Create your first trip." de="Erstelle deine erste Reise."

    # update just the German copy of an existing key
    scripts/i18n.py set trips.empty de="Noch keine Reisen."

    # remove a key from every locale
    scripts/i18n.py rm trips.empty.hint

    # list keys nothing in web/js references any more
    scripts/i18n.py unused

Formatting is a hard requirement: key order is preserved and files are written
back as 2-space-indented, ensure_ascii=False JSON with a trailing newline, so
changing one key is a one-line diff. That format round-trips the current files
byte-identically, which the round-trip guard below re-checks on every run.
"""

import argparse
import json
import re
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
LOCALES_DIR = ROOT / "web" / "locales"
JS_DIR = ROOT / "web" / "js"

EXAMPLES = """\
examples:
  # add a new key to every locale, next to a related one
  scripts/i18n.py set trips.empty.hint --after trips.empty \\
      en="Create your first trip." de="Erstelle deine erste Reise."

  # update just the German copy of an existing key
  scripts/i18n.py set trips.empty de="Noch keine Reisen."

  # remove a key from every locale
  scripts/i18n.py rm trips.empty.hint

  # list keys nothing in web/js references any more
  scripts/i18n.py unused
"""

# Keys reach t() by more routes than a single pattern covers, and every one of
# these is load-bearing — the first draft of this scanner missed three of them
# and reported 16 live keys as orphans.
#
# 1. Directly: t("key") / t('key') / t(`key`).
LITERAL_CALL_RE = re.compile(r"""\bt\(\s*(?P<q>["'`])(?P<key>[^"'`${}]+?)(?P=q)""")
# 2. Declaratively, via the data-i18n[-placeholder|-aria-label] attributes
#    translatePage() walks (see web/js/i18n.js).
#    The closing quote is required: without it, data-i18n="trip.tabs.${key}"
#    matched up to the `$` and invented a phantom "trip.tabs." key.
LITERAL_ATTR_RE = re.compile(r"""data-i18n(?:-placeholder|-aria-label)?=\\?(?P<q>["'])(?P<key>[^"'${}\\]+)(?P=q)""")
# 3. Composed at runtime, in a t() call — t(`item.category.${c}`). Unresolvable,
#    but the literal prefix is recoverable, and nothing under it may be called
#    unused.
DYNAMIC_CALL_RE = re.compile(r"""\bt\(\s*`(?P<prefix>[^`${}]*)\$\{""")
# 4. Composed at runtime, in an attribute — data-i18n="trip.tabs.${key}" in
#    trip-detail-page.js. Same treatment; missing this made all six tab keys
#    look unused *and* invented a phantom "trip.tabs." key.
DYNAMIC_ATTR_RE = re.compile(r"""data-i18n(?:-placeholder|-aria-label)?=\\?["'](?P<prefix>[^"'${}\\]*)\$\{""")
# 5. As a bare string, resolved somewhere else entirely: a ternary inside an
#    attribute (data-i18n="${mode === "login" ? "auth.login.title" : ...}"), or
#    a key handed to a component as data (ariaLabel: "locations.filter.label" in
#    locations-tab.js, which menu.js later renders into data-i18n-aria-label).
#    Chasing those properly needs a JS parser, so instead any quoted string that
#    exactly equals a known key counts as a reference. Keys are dotted
#    namespaces, so a coincidental collision is not a practical worry, and
#    erring toward "used" is the safe direction for a delete-suggesting tool.
ANY_STRING_RE = re.compile(r"""(?P<q>["'`])(?P<text>[^"'`${}\n]*)(?P=q)""")


def locale_files():
    files = sorted(LOCALES_DIR.glob("*.json"))
    if not files:
        sys.exit(f"i18n: no locale files found in {LOCALES_DIR}")
    return files


def locale_code(path):
    return path.stem


def load(path):
    """Loads a locale file, preserving key order (dicts keep insertion order)."""
    with path.open(encoding="utf-8") as f:
        data = json.load(f)
    if not isinstance(data, dict):
        sys.exit(f"i18n: {path.name} is not a JSON object")
    return data


def dump(path, data):
    """Writes a locale file in the repo's exact format."""
    path.write_text(json.dumps(data, indent=2, ensure_ascii=False) + "\n", encoding="utf-8")


def reinsert_after(data, key, value, anchor):
    """Returns a new dict with `key` placed directly after `anchor`.

    Python dicts have no insert-at-position, so this rebuilds in order. Called
    only for new keys; updating an existing key leaves its position alone.
    """
    out = {}
    for existing, existing_value in data.items():
        if existing == key:
            continue  # will be re-placed at the anchor
        out[existing] = existing_value
        if existing == anchor:
            out[key] = value
    return out


def cmd_set(args):
    values = {}
    for pair in args.assignments:
        if "=" not in pair:
            sys.exit(f"i18n: expected <locale>=<value>, got {pair!r}")
        code, value = pair.split("=", 1)
        if code in values:
            sys.exit(f"i18n: locale {code!r} given twice")
        values[code] = value

    files = locale_files()
    known = {locale_code(p) for p in files}
    for code in values:
        if code not in known:
            sys.exit(f"i18n: unknown locale {code!r} (have: {', '.join(sorted(known))})")

    loaded = {locale_code(p): (p, load(p)) for p in files}

    # Every locale that *lacks* the key must be given a value, or we'd write the
    # parity violation check_i18n.py exists to catch. Locales that already have
    # it can be left out — that's how you update one language's copy alone, and
    # how you backfill a key into just the locale that's missing it.
    unspecified = sorted(
        code for code, (_, data) in loaded.items()
        if args.key not in data and code not in values
    )
    if unspecified:
        sys.exit(
            f"i18n: {args.key!r} would be missing from {', '.join(unspecified)} — "
            f"give a value for every locale that doesn't have it yet, or check_i18n.py will fail"
        )

    # Validate the anchor in every locale we're about to touch *before* writing
    # any of them. Checking it per-file mid-loop could write en, then fail on de
    # and leave behind the parity break this script exists to prevent.
    if args.after:
        anchorless = sorted(
            code for code in values
            if args.key not in loaded[code][1] and args.after not in loaded[code][1]
        )
        if anchorless:
            sys.exit(
                f"i18n: anchor key {args.after!r} does not exist in "
                f"{', '.join(anchorless)}, so --after has nothing to place next to"
            )

    changed = []
    for code, value in values.items():
        path, data = loaded[code]
        if data.get(args.key) == value:
            continue
        was_new = args.key not in data
        if was_new and args.after:
            data = reinsert_after(data, args.key, value, args.after)
        else:
            data[args.key] = value
        dump(path, data)
        changed.append(f"{code} ({'added' if was_new else 'updated'})")

    if not changed:
        print(f"i18n: {args.key} already had these values, nothing written")
        return 0
    print(f"i18n: set {args.key} in {', '.join(changed)}")
    return 0


def cmd_rm(args):
    removed = []
    for path in locale_files():
        data = load(path)
        if args.key not in data:
            continue
        del data[args.key]
        dump(path, data)
        removed.append(locale_code(path))

    if not removed:
        sys.exit(f"i18n: {args.key!r} is not present in any locale file")
    print(f"i18n: removed {args.key} from {', '.join(removed)}")
    return 0


def scan_references(known_keys):
    """Returns (referenced keys, dynamic key prefixes) found under web/js.

    `known_keys` is needed for the bare-string pass (route 5 above), which can
    only recognise a key by matching one that already exists.
    """
    referenced, prefixes = set(), set()
    for path in sorted(JS_DIR.rglob("*.js")):
        if "vendor" in path.parts:
            continue
        text = path.read_text(encoding="utf-8")
        referenced.update(m.group("key") for m in LITERAL_CALL_RE.finditer(text))
        referenced.update(m.group("key") for m in LITERAL_ATTR_RE.finditer(text))
        referenced.update(m.group("text") for m in ANY_STRING_RE.finditer(text)
                          if m.group("text") in known_keys)
        for pattern in (DYNAMIC_CALL_RE, DYNAMIC_ATTR_RE):
            prefixes.update(m.group("prefix") for m in pattern.finditer(text) if m.group("prefix"))
    return referenced, prefixes


def cmd_unused(args):
    files = locale_files()
    all_keys = {}
    for path in files:
        for key in load(path):
            all_keys.setdefault(key, []).append(locale_code(path))

    referenced, prefixes = scan_references(set(all_keys))

    unused, dynamic = [], []
    for key in all_keys:
        if key in referenced:
            continue
        # t(key, params, count) picks "<key>_plural" itself, so a plural variant
        # is used whenever its base key is (see t() in web/js/i18n.js).
        base = key[: -len("_plural")] if key.endswith("_plural") else None
        if base and base in referenced:
            continue
        matched = sorted(p for p in prefixes if key.startswith(p))
        if matched:
            dynamic.append((key, matched))
        else:
            unused.append(key)

    missing = sorted(k for k in referenced if k not in all_keys)

    print(f"i18n: {len(all_keys)} keys across {len(files)} locale file(s); "
          f"{len(referenced)} referenced, {len(prefixes)} dynamic prefix(es)")

    if dynamic:
        print(f"\n{len(dynamic)} key(s) matched only by a runtime-composed key — NOT safe to delete:")
        for key, matched in dynamic:
            print(f"  ? {key}  (matches the composed key `{matched[0]}${{...}}`)")

    if missing:
        print(f"\n{len(missing)} key(s) referenced in web/js but defined in NO locale "
              f"(t() would render the key itself):")
        for key in missing:
            print(f"  ! {key}")

    if unused:
        print(f"\n{len(unused)} key(s) with no reference found in web/js:")
        for key in unused:
            print(f"  - {key}")
        print("\nReview before deleting — a key referenced from Go, HTML or a "
              "pattern this scan doesn't know would look identical.")
    else:
        print("\nNo unused keys.")

    # Reporting only: --strict is opt-in so this can't start failing `make ci`
    # on a key that is actually reachable in a way the scan can't see.
    if args.strict and (unused or missing):
        return 1
    return 0


def check_roundtrip():
    """Guards the formatting contract: our writer must reproduce the current
    files byte-for-byte, so an edit shows up as a one-line diff and nothing
    else. If this ever fails, the files were reformatted by hand and every
    later write would silently rewrite the whole file."""
    for path in locale_files():
        raw = path.read_text(encoding="utf-8")
        if json.dumps(json.loads(raw), indent=2, ensure_ascii=False) + "\n" != raw:
            sys.exit(
                f"i18n: {path.name} is not in the canonical format (2-space indent, "
                f"ensure_ascii=False, trailing newline), so writing it would reformat "
                f"the whole file — normalise it in its own commit first"
            )


def main():
    parser = argparse.ArgumentParser(
        description="Add, update, remove or audit keys across every web/locales/*.json file.",
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog=EXAMPLES,
    )
    sub = parser.add_subparsers(dest="command", required=True)

    p_set = sub.add_parser("set", help="create or update a key (one or more locales)")
    p_set.add_argument("key", help="dotted key, e.g. trips.empty.hint")
    p_set.add_argument("assignments", nargs="+", metavar="LOCALE=VALUE",
                       help="e.g. en=\"No trips yet.\" de=\"Noch keine Reisen.\"")
    p_set.add_argument("--after", metavar="ANCHOR_KEY",
                       help="place a NEW key directly after this one instead of at end of file")
    p_set.set_defaults(func=cmd_set)

    p_rm = sub.add_parser("rm", help="delete a key from every locale")
    p_rm.add_argument("key")
    p_rm.set_defaults(func=cmd_rm)

    p_unused = sub.add_parser("unused", help="list keys web/js no longer references")
    p_unused.add_argument("--strict", action="store_true",
                          help="exit non-zero if anything is unused or undefined (for CI)")
    p_unused.set_defaults(func=cmd_unused)

    args = parser.parse_args()
    check_roundtrip()
    return args.func(args)


if __name__ == "__main__":
    sys.exit(main())
