#!/usr/bin/env python3
"""Generates the app's PWA/favicon icon set. Run manually when the icon
design changes; output is committed, this script is not part of the build."""

from PIL import Image, ImageDraw

ACCENT = (37, 99, 235)  # matches --color-accent in base.css
WHITE = (255, 255, 255)


def draw_glyph(draw, box):
    """A simple two-peak mountain + sun glyph, scaled to fit `box` (x0,y0,x1,y1)."""
    x0, y0, x1, y1 = box
    w, h = x1 - x0, y1 - y0

    # Sun
    sun_r = w * 0.11
    sun_cx, sun_cy = x0 + w * 0.28, y0 + h * 0.32
    draw.ellipse([sun_cx - sun_r, sun_cy - sun_r, sun_cx + sun_r, sun_cy + sun_r], fill=WHITE)

    # Back (smaller) peak
    draw.polygon(
        [
            (x0 + w * 0.42, y0 + h * 0.68),
            (x0 + w * 0.62, y0 + h * 0.36),
            (x0 + w * 0.80, y0 + h * 0.68),
        ],
        fill=WHITE,
    )
    # Front (larger) peak, overlapping
    draw.polygon(
        [
            (x0 + w * 0.16, y0 + h * 0.74),
            (x0 + w * 0.42, y0 + h * 0.30),
            (x0 + w * 0.70, y0 + h * 0.74),
        ],
        fill=WHITE,
    )


def make_icon(size, path, padding_ratio=0.0, background=ACCENT, corner_ratio=0.22):
    img = Image.new("RGBA", (size, size), (0, 0, 0, 0))
    draw = ImageDraw.Draw(img)
    corner = int(size * corner_ratio)
    draw.rounded_rectangle([0, 0, size - 1, size - 1], radius=corner, fill=background)

    pad = size * padding_ratio
    draw_glyph(draw, (pad, pad, size - pad, size - pad))
    img.save(path)


def make_square_icon(size, path, background=ACCENT):
    """No rounded corners / no transparency — for apple-touch-icon and favicon,
    where the OS applies its own mask and transparency isn't reliable."""
    img = Image.new("RGB", (size, size), background)
    draw = ImageDraw.Draw(img)
    draw_glyph(draw, (0, 0, size, size))
    img.save(path)


if __name__ == "__main__":
    import os

    out = os.path.join(os.path.dirname(__file__), "..", "web", "icons")
    os.makedirs(out, exist_ok=True)

    make_icon(192, os.path.join(out, "icon-192.png"))
    make_icon(512, os.path.join(out, "icon-512.png"))
    # Maskable icons need content within the safe zone (inner ~80%), since the
    # OS may crop to a circle/squircle — extra padding, no rounded corners of
    # our own since the mask provides the shape.
    make_icon(512, os.path.join(out, "icon-maskable-512.png"), padding_ratio=0.14, corner_ratio=0)
    make_square_icon(180, os.path.join(out, "apple-touch-icon.png"))
    make_square_icon(32, os.path.join(out, "favicon-32.png"))
    make_square_icon(16, os.path.join(out, "favicon-16.png"))
    print("icons written to", out)
