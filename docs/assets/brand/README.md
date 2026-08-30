---
# These are the rules for using the brand set, kept beside the files they
# describe rather than in the documentation nav -- the audience is somebody
# reaching for an asset, not somebody installing the app. It is not in
# zensical.toml's nav for that reason, and excluded from search so it does not
# surface above the pages that answer a reader's actual question.
search:
  exclude: true
---

# Caravel brand assets (direction 2d — folded sail)

Palette: navy `#23304F` (ink on light grounds), lightened navy `#5470A8` (ink on dark grounds),
cream `#FAF7F2`, app blue `#2563EB` (UI accent only).

Direction 2d shipped in Stage 18. The icon set the app serves is *generated*
from these paths rather than copied — see `scripts/gen_icons.py`, which owns the
geometry so every raster size and `web/icons/favicon.svg` come from one source.

## Where the assets live

| Location | Holds | Used by |
| --- | --- | --- |
| `web/brand/` | `mark.svg` (inherits CSS `color`), the two horizontal lockups, `og-card.png` | the app, inline and by `<img>`; the card by scrapers |
| `web/icons/` | favicons, apple-touch, PWA and maskable icons | the browser and the installed app; **generated**, do not hand-edit |
| `docs/assets/brand/` | lockups, banner, OG cards, the navy/cream marks | the documentation site and the README |

Social cards must be PNG: scrapers reject SVG, and the URL must be absolute --
Facebook, LinkedIn and Discord drop an image they cannot resolve on their own.
The app substitutes its own origin into the shell for that; see
`internal/httpapi/staticassets.go`.

There are two OG cards, and the difference is the audience:

| Card | Says | For |
| --- | --- | --- |
| `og-card.png` | mark, headline, tagline | **an instance** -- what `web/index.html` serves. Someone sharing a link to their own trip planner should not be unfurling an advert to go install one |
| `og-card-cta.png` | the above plus a "Deploy Caravel" button and the project URL | **the project** -- the README and the documentation site, where the reader has not got one yet |

Both have a `-light` pair.

## Type


Wordmark: **Montserrat 700**, uppercase, tracking ~0.17em. Tagline: Montserrat 500, ~0.24em.

The PNG lockups, banner and OG cards are rendered with Montserrat baked in — use them wherever the
image is consumed as an image (README, social, docs, slides).

The **SVG** lockups/banner/OG files keep the wordmark as live `<text>` so you can edit the words.
That text renders in Montserrat only where the font is available (inlined in HTML, or with the font
installed); loaded via `<img src>` or by GitHub, external font loading is blocked and it falls back
to the platform sans. If you need an SVG that is typographically exact everywhere, open one in a
vector editor and convert the text to outlines — or just use the PNG.

The app and the documentation site render the wordmark as live text instead, in a
**self-hosted** Montserrat subset (`web/fonts/`, built by
`scripts/gen_brand_fonts.py`). Neither ever loads a font from a third party: a
self-hosted trip planner that phones out to Google on every page load would be
the wrong trade, and the app has to work offline.

## Rules

Clear space: at least the height of the wing (≈0.35× mark height) on all sides.
Minimum sizes: mark 16px, vertical lockup 120px wide, horizontal lockup 180px wide.
Don't recolor the mark outside the palette above, don't add effects, don't stretch.

## `src/`

The editable SVG originals, kept because they are not regenerable: their
wordmarks are live `<text>`, so this is where the words get changed. The PNGs
beside them are renders of these with Montserrat baked in. `app-icon-blue.*` is
the alternate blue tile from the original set — unused, kept as the option it
was drawn to be.

`web/icons/` is *not* in this list: it comes out of `scripts/gen_icons.py`.

## Re-rendering a PNG from its SVG

There is no generator for these -- unlike `web/icons/`, the `src/` SVGs are the
hand-maintained originals. Edit one, then render it at its own dimensions:

```
python3 -c "import cairosvg; cairosvg.svg2png(
    url='docs/assets/brand/src/og-card.svg',
    write_to='docs/assets/brand/og-card.png',
    output_width=1200, output_height=630)"
```

Needs `cairosvg`, and Montserrat installed system-wide for the `<text>` to come
out in the right face (`julietaula-montserrat-fonts` on Fedora) -- cairosvg
resolves it through fontconfig, and silently substitutes the platform sans if it
is missing, so check the render rather than trusting the exit code.

Not quantised with `pngquant`, deliberately: the grounds are diagonal gradients
and 256 colours bands them visibly. These files are well under any scraper size
limit as they are.

The app serves its own copy of the instance card at `web/brand/og-card.png` --
copy it across after re-rendering. No cache version to bump: `assetTreeFingerprint`
notices the file changed and re-keys the service worker on its own.
