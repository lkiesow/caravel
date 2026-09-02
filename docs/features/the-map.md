# The map

Every location with coordinates, on one map.

![The trip map](../assets/screenshots/map.png)

Pins are coloured by category — site, stay, transport — and each category can be
switched off, so "show me only where we are sleeping" is one click. The map
frames itself to fit whatever is currently shown.

The map is drawn in your browser from OpenStreetMap data, which means the
labels follow **your** language: the same trip reads Tokyo for an English
reader and Tokio for a German one, at the same time on the same instance. It
also comes in light and dark, independently of the rest of the app — including
a day/night mode that follows the actual position of the sun rather than your
operating system's idea of evening. Both live in **Settings → Appearance**; the
provider itself is configurable too, see [Map
style](../configuration/map-style.md). Clicking a pin opens the location, so
the map doubles as a way of navigating the trip rather than only a way of
looking at it.

## Getting coordinates onto a location

Three ways, in the order most people use them:

1. **Search for the address.** The location editor looks it up and fills in the
   coordinates. This runs through Caravel's own server rather than from your
   browser — see [Address search](../configuration/address-search.md).
2. **Type them.** Paste a latitude and longitude from wherever you found them.
3. **Let the assistant propose an address**, then accept it. It never invents
   coordinates: it suggests an address and the geocoder resolves it. See [The
   assistant](the-assistant.md).

A location can also be deliberately kept off the map even when it has
coordinates, for something like a home airport you do not want framing the view.

## On a phone

![The map on a phone](../assets/screenshots/mobile-map.png){ .screenshot-phone }

The tab row collapses to icons with a **More** menu, and the category filter
moves above the map so it does not cover it. The map itself gets the rest of the
screen.
