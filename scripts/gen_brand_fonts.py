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

Input is a pinned upstream release of each family, downloaded and checked
against the sha256 recorded below, so the faces can be rebuilt on any machine
rather than only one with the right distribution packages installed. Downloads
are cached in $XDG_CACHE_HOME/caravel/fonts (~/.cache/caravel/fonts), keyed by
their hash and re-checked on every run, so iterating on the subset does not
fetch Inter's 34 MB archive each time.

To move to a newer release: change the URL, run the script, and take the new
hash from the mismatch it reports -- after checking the release is the one you
meant. Then diff the output; a new version is a new set of glyphs.

Requires fonttools with brotli (`pip install 'fonttools[woff]'`).
"""

import hashlib
import io
import os
import sys
import tempfile
import urllib.request
import zipfile

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

# A source is (url, sha256, member): member is a path inside a zip archive, or
# None when the URL is the file itself. Montserrat publishes no release assets,
# so its files are fetched from the repository at the release tag; Inter's
# release is a single archive.
MONTSERRAT = "https://raw.githubusercontent.com/JulietaUla/Montserrat/v9.000/"
INTER_ZIP = (
    "https://github.com/rsms/inter/releases/download/v4.1/Inter-4.1.zip",
    "9883fdd4a49d4fb66bd8177ba6625ef9a64aa45899767dde3d36aa425756b11e",
)

FAMILIES = [
    {
        "slug": "montserrat",
        "license": (MONTSERRAT + "OFL.txt", "8b7141c03fa4f8d44e6345d5d4931709290f0f67875e452e95ac1fd3a027802e", None),
        # Only what the design actually asks for: 700 for the wordmark and
        # headings, 500 for the tagline and the tracked small caps. Every extra
        # weight is another file every visitor downloads.
        "weights": {
            500: (
                MONTSERRAT + "fonts/otf/Montserrat-Medium.otf",
                "9e2bff7923aaf42c5db116a1811f35ff41aa978abf091aed97ab5e1d0f052669",
                None,
            ),
            700: (
                MONTSERRAT + "fonts/otf/Montserrat-Bold.otf",
                "7869c1657888d7d9ba60fa243a37ffbc6b0eb316b1a93044bb1e07ba6ec169a8",
                None,
            ),
        },
        "destinations": [(WEB, None), (DOCS, (700,))],
    },
    {
        "slug": "inter",
        "license": (*INTER_ZIP, "LICENSE.txt"),
        # 400 is the body default, 600 is every emphasised label and amount,
        # 500 is the three rules between them. No 700: both bold rules in
        # base.css are on var(--font-brand), so Montserrat covers bold. No
        # italic: two rules use it, both small muted text, and synthetic
        # oblique is adequate there -- a real face would be another 20 KiB.
        "weights": {
            400: (*INTER_ZIP, "extras/ttf/Inter-Regular.ttf"),
            500: (*INTER_ZIP, "extras/ttf/Inter-Medium.ttf"),
            600: (*INTER_ZIP, "extras/ttf/Inter-SemiBold.ttf"),
        },
        "destinations": [(WEB, None)],
    },
]


def fetch(url, sha256):
    """The downloaded file at url, from the cache when it is already there.

    The hash is checked on every use, not only after a download, so a cache
    file that changed under us is caught the same way a changed upstream is.
    """
    cache = os.path.join(os.environ.get("XDG_CACHE_HOME") or os.path.expanduser("~/.cache"), "caravel", "fonts")
    os.makedirs(cache, exist_ok=True)
    path = os.path.join(cache, f"{sha256}-{os.path.basename(url)}")
    if not os.path.exists(path):
        print(f"downloading {url}")
        with urllib.request.urlopen(url) as response, open(path + ".part", "wb") as out:
            out.write(response.read())
        os.replace(path + ".part", path)
    with open(path, "rb") as f:
        data = f.read()
    actual = hashlib.sha256(data).hexdigest()
    if actual != sha256:
        os.remove(path)
        sys.exit(f"sha256 mismatch for {url}\n  expected {sha256}\n  actual   {actual}")
    return data


def read_source(source):
    url, sha256, member = source
    data = fetch(url, sha256)
    if member is None:
        return data
    with zipfile.ZipFile(io.BytesIO(data)) as archive:
        return archive.read(member)


def build(family, out_dir, weights=None):
    os.makedirs(out_dir, exist_ok=True)
    slug = family["slug"]
    wanted = {w: f for w, f in family["weights"].items() if weights is None or w in weights}
    missing = set(weights or ()) - set(family["weights"])
    if missing:
        sys.exit(f"{slug}: destination asks for weight(s) {sorted(missing)}, which WEIGHTS does not define")
    for weight, source in sorted(wanted.items()):
        filename = os.path.basename(source[2] or source[0])
        target = os.path.join(out_dir, f"{slug}-{weight}.woff2")
        # The subsetter takes a path, so the source goes through a temporary
        # file named like the original.
        with tempfile.TemporaryDirectory() as tmp:
            source_path = os.path.join(tmp, filename)
            with open(source_path, "wb") as f:
                f.write(read_source(source))
            subset.main(
                [
                    source_path,
                    f"--unicodes={UNICODES}",
                    "--layout-features=kern,liga",
                    "--flavor=woff2",
                    # The subsetter keeps the name table by default, which
                    # carries the family name @font-face matching does not use
                    # but a font inspector shows - worth keeping so a stray
                    # file is identifiable.
                    f"--output-file={target}",
                ]
            )
        print(f"{target}  {os.path.getsize(target) / 1024:.1f} KiB  (from {filename})")

    # A shipped font ships its licence, and two families from two copyright
    # holders means two files rather than one merged one -- hence the slug in
    # the name. OFL section 4 also forbids using the reserved font name for a
    # modified version, which is why the subsets keep the name and change
    # nothing but coverage.
    with open(os.path.join(out_dir, f"{slug}-OFL.txt"), "wb") as f:
        f.write(read_source(family["license"]))
    print(os.path.join(out_dir, f"{slug}-OFL.txt"))


if __name__ == "__main__":
    root = os.path.join(os.path.dirname(__file__), "..")
    # Fetch and check every source before writing anything, so a bad hash
    # cannot leave one family regenerated and the other not.
    for family in FAMILIES:
        for url, sha256, _ in [family["license"], *family["weights"].values()]:
            fetch(url, sha256)
    for family in FAMILIES:
        for parts, weights in family["destinations"]:
            out = os.path.join(root, *parts)
            build(family, out, weights)
            print(f"{family['slug']} written to", os.path.normpath(out))
