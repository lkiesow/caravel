# Map style

The map draws itself in the browser from a **vector style** — a JSON document
naming where the map data comes from and how to paint it. Out of the box that
style is served by Caravel itself and the data comes from
[OpenFreeMap](https://openfreemap.org/): no key, no account, no request limits.

| Variable | Default |
|---|---|
| `CARAVEL_MAP_STYLE_URL` | `/js/vendor/map-styles/liberty.json` |
| `CARAVEL_MAP_STYLE_DARK_URL` | `/js/vendor/map-styles/dark.json` |

Both accept an absolute path or an `http(s)` URL; anything else stops the
server at startup rather than producing an empty map. Two variables because
light and dark are a per-reader preference, so the instance has to be able to
answer both.

## You probably do not need to change this

Until Caravel drew its own maps, the tile provider was configuration for one
reason: **place names in a script you cannot read.** The old raster tiles
labelled everything in the local language — a trip to Japan showed 東京, not
Tokyo — and there was no setting that changed it, because the labels were
pixels in a pre-rendered image. The language was decided when the image was
drawn, and every provider you could switch to picked *one* language for
everybody looking at the instance.

That is fixed, and not by configuration. The labels are drawn in each reader's
browser now, so they follow **that reader's own language setting**: the same
trip reads Tokyo for an English reader and Tokio for a German one, on the same
instance at the same time. Nothing to set.

So what is left here is not about labels. It is about **who serves your map
data**.

!!! note "Map data is fetched by the browser"

    Unlike [address search](address-search.md), which Caravel proxies through
    its own server, the style's requests for map data go straight from each
    reader's browser to whoever serves them. The default points at
    OpenFreeMap's public instance, so a stock deployment sends its readers
    there.

    Attribution is handled for you: a style carries its own credit, and the map
    renders it in the corner. There is no attribution variable to keep in step
    any more — the old one was a trap, because changing the provider and
    forgetting the credit left the instance out of compliance with a map that
    still looked right.

## Reasons you might change it

### Self-hosting the map data

OpenFreeMap's public instance is free and unmetered, and it is also somebody
else's server. If you would rather not depend on it — or your deployment has no
outbound internet at all — you can run your own. OpenFreeMap
[documents self-hosting](https://openfreemap.org/), and
[TileServer GL](https://github.com/maptiler/tileserver-gl) serves the same
OpenMapTiles schema from a downloaded extract.

Point the style at your own server and the rest of Caravel is unchanged:

```sh
CARAVEL_MAP_STYLE_URL='https://tiles.example.org/styles/liberty.json'
CARAVEL_MAP_STYLE_DARK_URL='https://tiles.example.org/styles/dark.json'
```

### A commercial provider

[MapTiler](https://www.maptiler.com/), [Stadia Maps](https://stadiamaps.com/)
and others serve vector styles with a key appended to the URL. Worth it if you
want a cartography theirs does better, or an SLA. Check that the style you pick
carries `name:en` and `name:de` in its data, or the per-reader labels above
quietly stop following anyone: they fall back to the local name.

### A cartography of your own

A style is a document you can edit. If you build one — with
[Maputnik](https://maputnik.github.io/), say — serve it from anywhere and name
it here.

If you set only `CARAVEL_MAP_STYLE_URL`, that style is used for **both** light
and dark. That is deliberate: a style has no obligation to have a dark
counterpart, and showing the shipped dark map to a reader who chose your custom
one would drop them onto a different instance's cartography halfway through a
trip. Supply `CARAVEL_MAP_STYLE_DARK_URL` as well when you have one.

## What the reader controls

Nothing on this page changes per-reader behaviour, which is worth knowing before
you go looking for a setting:

- **Language.** Follows the reader's app language automatically.
- **Light or dark.** A per-browser choice in **Settings → Appearance**, with
  four options: follow the app, day/night by the actual position of the sun,
  always light, always dark.

## Upgrading from the tile variables

Earlier versions configured a raster tile layer with three variables — a URL
template, an attribution string and a maximum zoom. They are gone, because the
only reason to touch them was the label-language problem described above.
Delete them and let the defaults apply; if you were pointing them at your own
tile server, use `CARAVEL_MAP_STYLE_URL` instead.
