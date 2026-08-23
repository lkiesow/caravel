#!/usr/bin/env python3
"""Generates the app's PWA/favicon icon set from the brand mark. Run manually
when the icon design changes; output is committed, this script is not part of
the build.

    python3 scripts/gen_icons.py

The mark's geometry lives here rather than in a companion SVG, so there is one
source for every raster size and for web/icons/favicon.svg — the same reason
scripts/gen_icon_sprite.py owns its ICONS list. It is the folded sail from
docs/assets/brand/ (direction 2d); if that ever changes, the two paths below
are what to replace.

Needs cairosvg (`pip install cairosvg`), which is not otherwise a dependency
of anything here.
"""

import os

import cairosvg

# The palette from docs/assets/brand/README.md. Navy is the tile, cream the
# sail on top of it; the app blue (#2563EB, --color-accent in base.css) is
# deliberately not used here — it is the UI accent, not the mark's colour.
NAVY = "#23304F"
CREAM = "#FAF7F2"

# The mark, in a 64x64 viewBox: the sail, then the boom below it at 72%
# opacity. Ink spans x 14.6-60.8, y 4.0-60.8 — which is what the safe-area
# arithmetic in mark_transform() is derived from.
SAIL = "M43.2 4C31.4 14.8 21.4 31 14.6 50.4L39 40.6C42.2 28.6 43.6 15.4 43.2 4Z"
BOOM = "M27.4 46.6 60.8 37.9 41.2 60.8 27.4 46.6Z"

# The mark's ink bounding box in viewBox units, and the radius of the circle
# that encloses it about its own centre. Used to place the mark inside a
# maskable icon's safe zone, where "fits in the box" is not enough: the OS may
# crop to a circle.
INK = (14.6, 4.0, 60.8, 60.8)
INK_CX = (INK[0] + INK[2]) / 2
INK_CY = (INK[1] + INK[3]) / 2
INK_RADIUS = (((INK[2] - INK[0]) / 2) ** 2 + ((INK[3] - INK[1]) / 2) ** 2) ** 0.5


def mark_transform(size, radius):
    """Scale and centre the 64x64 mark so its ink fits a circle of `radius`
    pixels about the centre of a `size` square."""
    scale = radius / INK_RADIUS
    dx = round(size / 2 - INK_CX * scale, 3)
    dy = round(size / 2 - INK_CY * scale, 3)
    return f"translate({dx} {dy}) scale({round(scale, 4)})"


def svg_source(size, *, corner_ratio, ink_radius_ratio, background=NAVY, mark=CREAM):
    """A navy tile with the cream sail on it, as SVG text.

    `ink_radius_ratio` is the enclosing radius of the mark's ink as a fraction
    of the icon's width — the one number that decides how much clearance the
    mark gets, and the only difference between the ordinary and maskable icons.
    """
    corner = size * corner_ratio
    transform = mark_transform(size, size * ink_radius_ratio)
    return (
        f'<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 {size} {size}" '
        f'width="{size}" height="{size}" role="img" aria-label="Caravel">'
        f'<rect width="{size}" height="{size}" rx="{round(corner, 3)}" fill="{background}"/>'
        f'<g transform="{transform}">'
        f'<path d="{SAIL}" fill="{mark}"/>'
        f'<path d="{BOOM}" fill="{mark}" opacity=".72"/>'
        f"</g></svg>"
    )


def write_png(path, size, **kwargs):
    cairosvg.svg2png(
        bytestring=svg_source(64, **kwargs).encode(),
        write_to=path,
        output_width=size,
        output_height=size,
    )


if __name__ == "__main__":
    out = os.path.join(os.path.dirname(__file__), "..", "web", "icons")
    os.makedirs(out, exist_ok=True)

    # The ordinary tile: rounded corners of our own, mark filling most of it.
    # 0.30 puts the ink's enclosing circle at 30% of the width, which is the
    # clearance the brand's own app icon uses.
    tile = dict(corner_ratio=112 / 512, ink_radius_ratio=0.30)

    write_png(os.path.join(out, "icon-192.png"), 192, **tile)
    write_png(os.path.join(out, "icon-512.png"), 512, **tile)

    # Maskable: no corners of our own (the OS mask provides the shape) and the
    # ink pulled inside the safe zone. Android may crop to a circle of 80% of
    # the width, so the safe radius is 0.40 — 0.29 leaves visible margin even
    # under the most aggressive mask. This is the icon that looks wrong if the
    # ordinary tile is shipped in its place: the sail's tip gets clipped.
    write_png(
        os.path.join(out, "icon-maskable-512.png"), 512, corner_ratio=0, ink_radius_ratio=0.29
    )

    # apple-touch-icon and the favicons: square, since the OS applies its own
    # mask (iOS) or renders at a size where a radius is noise (16/32).
    write_png(os.path.join(out, "apple-touch-icon.png"), 180, corner_ratio=0, ink_radius_ratio=0.30)
    for size in (32, 16):
        # Slightly more clearance than the tile: at 16px the mark needs air
        # around it or it reads as a smudge against the tab's edge.
        write_png(
            os.path.join(out, f"favicon-{size}.png"), size, corner_ratio=0.22, ink_radius_ratio=0.27
        )

    # An SVG favicon too, which every current browser prefers over the PNGs and
    # which stays sharp on a hidpi tab strip. Same geometry, same source.
    with open(os.path.join(out, "favicon.svg"), "w") as fh:
        fh.write(svg_source(64, corner_ratio=0.22, ink_radius_ratio=0.27) + "\n")

    print("icons written to", os.path.normpath(out))
