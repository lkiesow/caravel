#!/usr/bin/env python3
"""Builds web/fonts/ — the self-hosted faces the app is set in. Run manually
when the families, the weights or the character coverage change; output is
committed, this script is not part of the build.

    python3 scripts/gen_brand_fonts.py

Two families, and the split between them is the point. **Montserrat** is the
brand face: the wordmark, the hero titles, the tracked small caps — geometric,
wide, and deliberately not what a paragraph is set in. **Inter** is the UI
face: body text, labels, form fields, every table row. It is drawn for
interfaces at small sizes and carries the tabular figures the expense columns
ask for.

Why self-hosted at all: the app is self-hosted, has to work offline, and should
not make every instance announce every page load to a font CDN. Why a subset:
the full families cover Cyrillic, Greek and Vietnamese, which nothing here
needs — latin plus latin-ext is a fraction of the bytes and covers both shipped
locales.

Input is the OFL build packaged by the distribution, so no download is needed:

    sudo dnf install julietaula-montserrat-fonts rsms-inter-fonts

Requires fonttools with brotli (`pip install 'fonttools[woff]'`).
"""

import os
import shutil
import sys

from fontTools import subset

# Latin + Latin-1 Supplement + Latin Extended-A, plus the punctuation the copy
# uses (en/em dashes, curly quotes, the ellipsis). Covers English and German,
# which is every locale under web/locales today.
UNICODES = "U+0020-007E,U+00A0-00FF,U+0100-017F,U+2013-2014,U+2018-201A,U+201C-201E,U+2026,U+20AC"

# Destinations are per family, and carry the weights that destination actually
# uses. The app serves everything from web/fonts/ (embedded into the binary);
# the documentation site can only reach files under its own docs_dir, so it
# needs its own copy -- but it only sets the brand lockups in Montserrat and
# takes its body text from Material, so it gets neither the UI face nor the
# weights it never asks for. Writing the site copy here is what stops it
# drifting to an older subset than the app.
#
# A destination is (path parts, weights) where weights of None means all of
# them. docs/ takes 700 alone because every Montserrat rule in
# docs/assets/stylesheets/brand.css is font-weight: 700 -- the 500 shipped
# there for two stages without a single rule matching it.
WEB = ("web", "fonts")
DOCS = ("docs", "assets", "fonts")

FAMILIES = [
    {
        "slug": "montserrat",
        "package": "julietaula-montserrat-fonts",
        "source_dir": "/usr/share/fonts/julietaula-montserrat-fonts",
        "license_file": "/usr/share/licenses/julietaula-montserrat-fonts/OFL.txt",
        # Only what the design actually asks for: 700 for the wordmark and
        # headings, 500 for the tagline and the tracked small caps. Every extra
        # weight is another file every visitor downloads.
        "weights": {
            500: "Montserrat-Medium.otf",
            700: "Montserrat-Bold.otf",
        },
        "destinations": [(WEB, None), (DOCS, (700,))],
    },
    {
        "slug": "inter",
        "package": "rsms-inter-fonts",
        "source_dir": "/usr/share/fonts/rsms-inter-fonts",
        "license_file": "/usr/share/licenses/rsms-inter-fonts/LICENSE.txt",
        # 400 is the body default, 600 is every emphasised label and amount,
        # 500 is the three rules between them. No 700: both bold rules in
        # base.css are on var(--font-brand), so Montserrat covers bold. No
        # italic: two rules use it, both small muted text, and synthetic
        # oblique is adequate there -- a real face would be another 20 KiB.
        "weights": {
            400: "Inter-Regular.ttf",
            500: "Inter-Medium.ttf",
            600: "Inter-SemiBold.ttf",
        },
        "destinations": [(WEB, None)],
    },
]


def build(family, out_dir, weights=None):
    os.makedirs(out_dir, exist_ok=True)
    slug = family["slug"]
    wanted = {w: f for w, f in family["weights"].items() if weights is None or w in weights}
    missing = set(weights or ()) - set(family["weights"])
    if missing:
        sys.exit(f"{slug}: destination asks for weight(s) {sorted(missing)}, which WEIGHTS does not define")
    for weight, filename in sorted(wanted.items()):
        source = os.path.join(family["source_dir"], filename)
        if not os.path.exists(source):
            sys.exit(f"missing {source} — is {family['package']} installed?")
        target = os.path.join(out_dir, f"{slug}-{weight}.woff2")
        subset.main(
            [
                source,
                f"--unicodes={UNICODES}",
                "--layout-features=kern,liga",
                "--flavor=woff2",
                # The subsetter keeps the name table by default, which carries
                # the family name @font-face matching does not use but a font
                # inspector shows - worth keeping so a stray file is
                # identifiable.
                f"--output-file={target}",
            ]
        )
        print(f"{target}  {os.path.getsize(target) / 1024:.1f} KiB  (from {filename})")

    # A shipped font ships its licence, and two families from two copyright
    # holders means two files rather than one merged one -- hence the slug in
    # the name. OFL section 4 also forbids using the reserved font name for a
    # modified version, which is why the subsets keep the name and change
    # nothing but coverage.
    license_file = family["license_file"]
    if not os.path.exists(license_file):
        sys.exit(f"missing {license_file} — the licence must ship with the fonts")
    shutil.copyfile(license_file, os.path.join(out_dir, f"{slug}-OFL.txt"))
    print(os.path.join(out_dir, f"{slug}-OFL.txt"))


if __name__ == "__main__":
    root = os.path.join(os.path.dirname(__file__), "..")
    for family in FAMILIES:
        for parts, weights in family["destinations"]:
            out = os.path.join(root, *parts)
            build(family, out, weights)
            print(f"{family['slug']} written to", os.path.normpath(out))
