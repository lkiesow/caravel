# OpenFreeMap styles (vendored)

Two MapLibre style documents, fetched verbatim from OpenFreeMap:

```sh
curl -sS -o positron.json https://tiles.openfreemap.org/styles/positron
curl -sS -o dark.json     https://tiles.openfreemap.org/styles/dark
```

```
a0f5b8487480a47ba5a5eaf25e165a19789dfa422f9ef9442f04da79c7d216db  positron.json
4ba4a1990dc5e1b72b38483dfbb92ffdd945bcc13e9f1235466dae900bc51631  dark.json
```

Note the dark style is served at `/styles/dark`. There is no `/styles/dark-matter`
on OpenFreeMap -- that path 404s, whatever the OpenMapTiles project calls the
cartography upstream.

## Why vendored rather than fetched at runtime

The app patches every label layer's `text-field` to follow the reader's own
locale, which means owning the document rather than mutating one fetched from
a third party -- and an upstream edit cannot silently restyle the app. They are
also small (~25 KB and ~21 KB) and cacheable by the service worker, which
cross-origin requests are not.

## Shape, as of vendoring

|          | layers | with `text-field` |
|----------|--------|-------------------|
| positron | 55     | 19                |
| dark     | 47     | 13                |

Both reference only `tiles.openfreemap.org` -- vector tiles via the
`openmaptiles` source's TileJSON at `/planet`, a `ne2_shaded` raster source,
plus glyphs and sprites. Nothing else has to be vendored for them to render.

Their stock `text-field` is
`["case", ["has","name:nonlatin"], ["concat", ["get","name:latin"], …], ["coalesce", ["get","name_en"], ["get","name"]]]`
-- one language for everyone, which is the thing this stage set out to fix.
`buildStyle()` in the map component rewrites it.
