# OpenFreeMap styles (vendored)

Two MapLibre style documents, fetched from OpenFreeMap:

```sh
curl -sS -o liberty.json https://tiles.openfreemap.org/styles/liberty
curl -sS -o dark.json    https://tiles.openfreemap.org/styles/dark
```

```
6010998863b4876911ac9a2d62c9a28d97c8877f6d20cd158b74808572257b60  liberty.json   (upstream, unmodified)
4ba4a1990dc5e1b72b38483dfbb92ffdd945bcc13e9f1235466dae900bc51631  dark.json      (upstream, before our patch)
d84a6872ddb4559455fd17e20ccacba39b61a726f518b151ba940956489fc500  dark.json      (as committed -- see below)
```

Note the dark style is served at `/styles/dark`. There is no
`/styles/dark-matter` on OpenFreeMap -- that path 404s, whatever the
cartography is called upstream.

## Why vendored rather than fetched at runtime

The app patches every label layer's `text-field` to follow the reader's own
locale, which means owning the document rather than mutating one fetched from a
third party -- and an upstream edit cannot silently restyle the app. They are
also small and cacheable by the service worker, which cross-origin requests are
not.

## Why liberty and not positron

Positron was the light style until the Stage 30 follow-up. It is an *overlay*
basemap -- deliberately desaturated so that data drawn on top of it stands out
-- and Caravel uses it as a map in its own right, which is not what it is for.
Measured against its own land colour, positron drew:

- forests and parks as neutral grey at dE 3.8-7.0, and `landcover_wood` only
  from zoom 10 up, so at trip-planning zooms there was no green anywhere;
- buildings at dE 3.3, which is close to invisible;
- roads at 1.08-1.32:1.

Liberty is the full-detail style from the same project: parks and forests
green, water blue (dE 43 against land rather than 16), buildings at dE 9.6
with 3D extrusion above zoom 14, and a road hierarchy you can read. Its labels
are no worse -- every real label layer is at or above 5.25:1.

`bright` was the other candidate and was rejected on measurement: its forest
green is `#6a4` at **0.1 alpha**, which composites to dE 6.8 -- no better than
positron -- and its buildings to dE 4.1.

## The dark style is patched, and this is what changed

`dark.json` is **not** byte-identical to upstream. It is the only dark style
OpenFreeMap serves, and as shipped it failed WCAG AA for text: every place
name was `rgb(101,101,101)` on `rgb(12,12,12)`, which is 3.36:1 against a
threshold of 4.5, and road names were 2.37:1. Water sat at dE 6.7 from land,
so a coastline did not read at all.

The patch is a list of per-layer colour substitutions -- 32 layers -- chosen
per layer rather than as a blanket transform, because the same literal means
different things in different places (`rgb(27,27,29)` is water in one layer and
a footpath in another). What it does:

| | before | after |
|---|---|---|
| place labels | 3.36:1 | 7.75:1 |
| road labels | 2.37:1 | 5.67:1 |
| water labels | black on dark water | on-colour, with a dark blue halo |
| water | dE 6.7 | dE 20.0, and blue |
| forest / park | dE 1.2, grey | dE 18.0, green |
| buildings | 1.01:1, darker than land | dE 8.9, lighter than land |
| road casings | 1.77:1 | 3.56:1 |

All 13 label layers now pass WCAG AA.

**Re-vendoring the dark style will drop this.** If you refetch it, re-apply the
patch; the substitutions are recoverable from this file's git history, in the
commit that introduced them. Liberty can be refetched freely.

## Shape, as of vendoring

|          | layers | with `text-field` | localised | left alone |
|----------|--------|-------------------|-----------|------------|
| liberty  | 111    | 23                | 20        | 3          |
| dark     | 47     | 13                | 12        | 1          |

The layers left alone in both are road shields, whose text is
`["to-string", ["get","ref"]]` -- a motorway number, not a name, with no
translation. `localiseLabels()` in the map component only rewrites expressions
that actually read a name field, which is what keeps those intact.

Both reference only `tiles.openfreemap.org` -- vector tiles via the
`openmaptiles` source's TileJSON, a `ne2_shaded` raster source, plus glyphs and
sprites. Nothing else has to be vendored for them to render.
