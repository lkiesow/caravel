# Address search

Typing a place name into a location and getting coordinates back is address
search, and it works out of the box: `CARAVEL_GEOCODER_URL` defaults to
OpenStreetMap's Nominatim, the same project the map tiles come from.

| Variable | Default |
|---|---|
| `CARAVEL_GEOCODER_URL` | `https://nominatim.openstreetmap.org/search` |

## It is called from the server, not the browser

Caravel proxies address search through `/api/geocode` rather than letting the
browser call Nominatim directly, for two reasons. OSM's usage policy wants an
identifying User-Agent and no more than one request a second, neither of which a
browser can promise. And a self-hosted app should not hand a user's typing to a
third party without a single place to turn that off.

That proxy is rate limited to 20 requests a minute per client address —
deliberately stricter than it needs to be for Caravel's own protection, because
what it is protecting is somebody else's service.

## Turning it off

Set it empty:

```sh
CARAVEL_GEOCODER_URL=
```

The API then reports address search as unavailable and the UI hides the control
rather than offering one that cannot work. Coordinates can still be entered by
hand, and locations without coordinates simply do not appear on the map.

## Running your own

Any endpoint with Nominatim's response shape works, so a self-hosted
[Nominatim](https://nominatim.org/) — or anything that imitates it — can be
pointed at instead:

```sh
CARAVEL_GEOCODER_URL=https://nominatim.example.org/search
```

Worth doing if you use address search heavily. The public instance is a
volunteer-funded service with a usage policy that asks bulk users to host their
own, and it is entitled to refuse an instance that leans on it.

!!! note "The assistant depends on this"

    [The assistant](assistant.md) never takes coordinates from the model — it
    proposes an address and this endpoint resolves it. With address search off,
    the assistant can still suggest everything else, but it cannot place a
    location on the map.
