#!/usr/bin/env python3
"""Builds web/icons/lucide-sprite.svg from the lucide-static package. Run
manually when the icon list changes (extend ICONS below, then re-run);
output is committed, this script is not part of the build.

Requires `lucide-static` installed somewhere node can resolve it from, e.g.:
    npm install lucide-static --prefix /tmp/lucide-scratch
    python3 scripts/gen_icon_sprite.py /tmp/lucide-scratch/node_modules/lucide-static/icons
"""

import os
import re
import sys

ICONS = [
    "pencil",
    "trash-2",
    "plus",
    "x",
    "arrow-left",
    "upload",
    "image",
    "map-pin",
    "link",
    "calendar",
    "file-text",
    "log-out",
    "chevron-down",
    "map",
    "list",
    "check",
    "log-in",
    "info",
    "list-checks",
    "settings",
    "search",
    "funnel",
    "ellipsis",
    "file",
    "ellipsis-vertical",
    "locate-fixed",
    "users",
    "user-plus",
    "shield-user",
    "key-round",
    "lock",
    "eye",
]


def strip_svg_wrapper(svg_source):
    """Extracts the viewBox and inner markup from a lucide-static SVG file,
    dropping the license comment and the outer <svg ...> attributes (the
    sprite's <use> consumers apply their own width/height/stroke via CSS)."""
    viewbox_match = re.search(r'viewBox="([^"]+)"', svg_source)
    viewbox = viewbox_match.group(1) if viewbox_match else "0 0 24 24"
    inner_match = re.search(r"<svg[^>]*>(.*)</svg>", svg_source, re.DOTALL)
    inner = inner_match.group(1).strip() if inner_match else ""
    return viewbox, inner


def build_sprite(icons_dir, icons):
    symbols = []
    for name in icons:
        path = os.path.join(icons_dir, f"{name}.svg")
        with open(path, encoding="utf-8") as f:
            viewbox, inner = strip_svg_wrapper(f.read())
        symbols.append(f'  <symbol id="lucide-{name}" viewBox="{viewbox}">\n    {inner}\n  </symbol>')
    return (
        '<svg xmlns="http://www.w3.org/2000/svg" style="display:none">\n'
        + "\n".join(symbols)
        + "\n</svg>\n"
    )


if __name__ == "__main__":
    if len(sys.argv) != 2:
        print(f"usage: {sys.argv[0]} <path-to-lucide-static/icons>", file=sys.stderr)
        sys.exit(1)

    icons_dir = sys.argv[1]
    sprite = build_sprite(icons_dir, ICONS)

    out_path = os.path.join(os.path.dirname(__file__), "..", "web", "icons", "lucide-sprite.svg")
    with open(out_path, "w", encoding="utf-8") as f:
        f.write(sprite)
    print(f"wrote {len(ICONS)} icons to {out_path}")
