# Map tiles

The map draws itself from tiles fetched by the browser, and out of the box those
come from OpenStreetMap's own servers — no key, no account, nothing to set up.

| Variable | Default |
|---|---|
| `CARAVEL_TILE_URL` | `https://{s}.tile.openstreetmap.org/{z}/{x}/{y}.png` |
| `CARAVEL_TILE_ATTRIBUTION` | `&copy; <a href="https://www.openstreetmap.org/copyright">OpenStreetMap</a> contributors` |
| `CARAVEL_TILE_MAX_ZOOM` | `19` |

The URL is an XYZ template: `{z}`, `{x}` and `{y}` are required, `{s}` rotates
over subdomains if the provider uses them, and `{r}` becomes `@2x` on
high-resolution screens for providers that serve a retina variant. A template
missing one of the required three stops the server at startup rather than
producing a map of grey squares.

## Why you might change it

The usual reason is **place names in a script you cannot read**. The standard
OpenStreetMap tiles label everything in the local language: a trip to Japan
shows 東京, not Tokyo, and Greece, Russia, Israel, China and Thailand all behave
the same way.

There is no setting on those tiles that changes it. They are pre-rendered PNGs,
so the labels are pixels in an image — the language was decided when the image
was drawn. OpenStreetMap's data does carry `name:en` and `name:de` tags, but the
standard style does not use them. **Changing the provider is the only fix.**

!!! note "Tiles are fetched by the browser"

    Unlike [address search](address-search.md), which Caravel proxies through
    its own server, tile requests go straight from each user's browser to
    whoever serves them. Setting this points your users at that provider, so
    the choice is worth making deliberately — and whatever you pick, its
    attribution requirement is yours to meet through
    `CARAVEL_TILE_ATTRIBUTION`.

## Providers worth knowing about

### CARTO — latin script everywhere

```sh
CARAVEL_TILE_URL='https://basemaps.cartocdn.com/rastertiles/voyager/{z}/{x}/{y}{r}.png'
CARAVEL_TILE_ATTRIBUTION='&copy; <a href="https://www.openstreetmap.org/copyright">OpenStreetMap</a> contributors &copy; <a href="https://carto.com/attributions">CARTO</a>'
CARAVEL_TILE_MAX_ZOOM=20
```

Built on OpenMapTiles, which labels from the `name:latin` tag, so the tile that
reads 浦安市 on the default provider reads URAYASU here — worldwide, without a
key. Swap `voyager` for `light_all` or `dark_all` for the muted Positron and
Dark Matter styles. It serves an `@2x` variant, which is what the `{r}` in the
template above is for: sharper labels on a phone.

Two things to know before committing to it. CARTO asks that you request a free
API key (no account needed, fair use up to 5 million tiles a month) and append
`?key=...` to the template. And CARTO describes its raster tiles as being on a
retirement path in favour of vector ones — fine today, worth re-checking in a
year.

### OpenStreetMap France — latin script, no key at all

```sh
CARAVEL_TILE_URL='https://{s}.tile.openstreetmap.fr/osmfr/{z}/{x}/{y}.png'
CARAVEL_TILE_ATTRIBUTION='&copy; <a href="https://www.openstreetmap.org/copyright">OpenStreetMap</a> contributors, tiles by <a href="https://openstreetmap.fr/">OpenStreetMap France</a>'
```

The same map with latin labels and a French lean: Urayasu, Kōtō, "Préfecture de
Chiba". Nothing to register for, which makes it the shortest path out of
non-latin labels. It is a donation-funded community service like OSM's own, so
the same courtesy applies — this is not a service to point a large deployment at.

### Tracestrack — pick an actual language

```sh
CARAVEL_TILE_URL='https://tile.tracestrack.com/topo_en/{z}/{x}/{y}.png?key=YOUR_KEY'
CARAVEL_TILE_ATTRIBUTION='&copy; <a href="https://www.openstreetmap.org/copyright">OpenStreetMap</a> contributors &copy; <a href="https://www.tracestrack.com/">Tracestrack</a>'
```

The others give you latin *script* — "Tōkyō" rather than "Tokyo", and never
"Tokio". Tracestrack is the one raster provider that renders genuine language
variants, selected by the code in the path: `topo_en`, `topo_de`, and around
eighteen others. This is the option if you want the map to read the way your
users write.

It needs a free API key, and the free tier is for non-commercial use. Check
their site for the current language list and terms.

### OpenTopoMap and CyclOSM — a different question

Terrain contours and cycling infrastructure respectively, both keyless. Worth
knowing they exist, but they do **not** help here: OpenTopoMap labels the same
Tokyo tile 浦安富士 and ハリケーンポイント, exactly like the default.

## What none of them can do

Every option above picks **one** language for everyone looking at the instance.
A German user and a Japanese user see the same tiles, because the tiles were
drawn before either of them asked for one. Labels that follow each user's own
language setting need vector tiles, which the browser styles as it draws them —
a different rendering approach than the one Caravel uses today.
