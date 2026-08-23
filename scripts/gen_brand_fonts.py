#!/usr/bin/env python3
"""Builds web/fonts/ — the self-hosted Montserrat subset the brand wordmark and
headings are set in. Run manually when the weights or the character coverage
change; output is committed, this script is not part of the build.

    python3 scripts/gen_brand_fonts.py

Why self-hosted at all: the app is self-hosted, has to work offline, and should
not make every instance announce every page load to a font CDN. Why a subset:
the full Montserrat covers Cyrillic and Vietnamese, which nothing here needs —
latin plus latin-ext is ~15% of the bytes and covers both shipped locales.

Input is the OFL build packaged by the distribution (Fedora:
`julietaula-montserrat-fonts`), so no download is needed:

    sudo dnf install julietaula-montserrat-fonts

Requires fonttools with brotli (`pip install 'fonttools[woff]'`).
"""

import os
import shutil
import sys

from fontTools import subset

SOURCE_DIR = "/usr/share/fonts/julietaula-montserrat-fonts"
LICENSE_FILE = "/usr/share/licenses/julietaula-montserrat-fonts/OFL.txt"

# Only what the design actually asks for: 700 for the wordmark and headings,
# 500 for the tagline and the tracked small caps. Every extra weight is another
# file every visitor downloads.
WEIGHTS = {
    500: "Montserrat-Medium.otf",
    700: "Montserrat-Bold.otf",
}

# Latin + Latin-1 Supplement + Latin Extended-A, plus the punctuation the copy
# uses (en/em dashes, curly quotes, the ellipsis). Covers English and German,
# which is every locale under web/locales today.
UNICODES = "U+0020-007E,U+00A0-00FF,U+0100-017F,U+2013-2014,U+2018-201A,U+201C-201E,U+2026,U+20AC"


def build(source_dir, out_dir):
    os.makedirs(out_dir, exist_ok=True)
    for weight, filename in sorted(WEIGHTS.items()):
        source = os.path.join(source_dir, filename)
        if not os.path.exists(source):
            sys.exit(f"missing {source} — is julietaula-montserrat-fonts installed?")
        target = os.path.join(out_dir, f"montserrat-{weight}.woff2")
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

    # A shipped font ships its licence. OFL section 4 also forbids using the
    # reserved font name for a modified version, which is why the subsets keep
    # the name and change nothing but coverage.
    if os.path.exists(LICENSE_FILE):
        shutil.copyfile(LICENSE_FILE, os.path.join(out_dir, "OFL.txt"))
        print(os.path.join(out_dir, "OFL.txt"))
    else:
        sys.exit(f"missing {LICENSE_FILE} — the licence must ship with the fonts")


# Two destinations, one generator. The app serves the faces from web/fonts/
# (embedded into the binary); the documentation site can only reach files under
# its own docs_dir, so it needs its own copy. Writing both here is what stops
# the site drifting to an older subset than the app -- the alternative, copying
# by hand after a coverage change, is exactly the step that gets forgotten.
DESTINATIONS = [
    ("web", "fonts"),
    ("docs", "assets", "fonts"),
]


if __name__ == "__main__":
    root = os.path.join(os.path.dirname(__file__), "..")
    for parts in DESTINATIONS:
        out = os.path.join(root, *parts)
        build(SOURCE_DIR, out)
        print("fonts written to", os.path.normpath(out))
