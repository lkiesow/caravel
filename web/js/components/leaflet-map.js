import { api } from "../api.js";
import { t } from "../i18n.js";
import { icon } from "../icon.js";
import { getCurrentPosition, locateErrorKey, locateUnavailableReason } from "../geolocation.js";
import { googleMapsUrl } from "../url.js";

// The tile layer, as the instance has it configured. Defaults duplicated from
// internal/httpapi/map.go on purpose: they are what the map falls back to when
// the request fails, and a Map tab of grey squares is a worse answer to "the
// config endpoint is briefly unreachable" than the tiles Caravel shipped with.
const DEFAULT_TILE_CONFIG = {
  style_url: "/js/vendor/map-styles/positron.json",
  tile_url: "https://{s}.tile.openstreetmap.org/{z}/{x}/{y}.png",
  tile_attribution:
    '&copy; <a href="https://www.openstreetmap.org/copyright">OpenStreetMap</a> contributors',
  max_zoom: 19,
};

// Memoised at module scope, not per instance: three routes mount a map and a
// trip page can swap between them, but the answer is instance-wide and cannot
// change without a server restart. One request per page load, not per map.
let tileConfigPromise = null;

function loadTileConfig() {
  if (!tileConfigPromise) {
    tileConfigPromise = api.get("/map/config").catch(() => DEFAULT_TILE_CONFIG);
  }
  return tileConfigPromise;
}

// MapLibre draws from a *style*, not from a tile layer, so there is no longer
// a one-line "here is the tile URL" call. Two shapes come out of here:
//
//   - the vector style the instance points at, vendored under
//     web/js/vendor/map-styles/ and served from our own origin;
//   - a raster style synthesised from CARAVEL_TILE_URL, for an operator who
//     has pinned a raster provider. The server decides which by leaving
//     style_url empty when the tile URL is not the shipped default, so the
//     choice lives with the person who configured it rather than being
//     guessed from a string comparison in the browser.
//
// Memoised per style URL for the same reason loadTileConfig is: three mounts,
// one answer, and re-parsing 25KB of JSON per map is waste.
const styleCache = new Map();

async function buildStyle(tiles) {
  if (!tiles.style_url) return rasterStyle(tiles);
  if (!styleCache.has(tiles.style_url)) {
    styleCache.set(
      tiles.style_url,
      fetch(tiles.style_url).then((r) => {
        if (!r.ok) throw new Error(`style ${r.status}`);
        return r.json();
      })
    );
  }
  try {
    // Structured-cloned rather than handed out: MapLibre mutates the style
    // object it is given, so two maps sharing one parsed document would
    // corrupt each other -- and the trip page mounts a second map without
    // tearing the first one down.
    return structuredClone(await styleCache.get(tiles.style_url));
  } catch {
    // A missing or malformed style should not leave a blank rectangle when
    // there is still a tile URL to fall back to. Same reasoning as
    // DEFAULT_TILE_CONFIG above: a working map from the shipped defaults
    // beats an honest void.
    styleCache.delete(tiles.style_url);
    return rasterStyle(tiles);
  }
}

// A raster provider expressed as a style document.
//
// Two of Leaflet's tile-layer conveniences have no MapLibre counterpart and
// are handled here instead:
//
//   - {s}. MapLibre round-robins the `tiles` array, which is exactly what
//     Leaflet's subdomains option did, so expanding the placeholder into three
//     URLs reproduces it -- and reproduces it *correctly*, since the list is
//     written out rather than inferred. The old "abcd" bug (a quarter of all
//     tiles sent to d.tile.openstreetmap.org, a host that does not resolve)
//     cannot recur in this shape.
//   - {r}, the @2x retina suffix, which MapLibre does not substitute. Stripped
//     rather than faked: a URL asking for @2x tiles still resolves without it,
//     where leaving the literal "{r}" in would 404 every tile.
function rasterStyle(tiles) {
  const url = tiles.tile_url.replace("{r}", "");
  const urls = url.includes("{s}")
    ? ["a", "b", "c"].map((s) => url.replace("{s}", s))
    : [url];
  return {
    version: 8,
    sources: {
      tiles: {
        type: "raster",
        tiles: urls,
        tileSize: 256,
        maxzoom: tiles.max_zoom,
      },
    },
    layers: [{ id: "tiles", type: "raster", source: "tiles" }],
  };
}

const CATEGORY_COLORS = {
  site: "#16a34a",
  stay: "#7c3aed",
  transport: "#2563eb",
};

// Zoom used when the view can't be derived from spread-out markers - a
// single marker, or a set of markers that all sit in the same place. Close
// enough to read street names, and shallow enough that any tile provider has
// something to serve - the layer's own maxZoom is configurable and sits well
// past what most providers actually render.
const SINGLE_MARKER_ZOOM = 14;

// How long the gesture hint stays up. Long enough to read six words, short
// enough that it is gone before it becomes the thing you are looking at.
const GESTURE_HINT_MS = 1500;

// Wheel pixels per zoom level, and the ceiling on what a single flick may
// bank. 60 is Leaflet's own wheelPxPerZoomLevel, so one mouse notch (which
// Firefox reports as 3 lines, i.e. 60px) is exactly one level.
const WHEEL_PX_PER_ZOOM = 60;
const WHEEL_ACCUM_CAP = 240;

// Category colour for a marker, for items whose category is unknown or not
// one of the three the app defines (the single-marker mode gets it from an
// attribute, so it can legitimately be absent).
const FALLBACK_MARKER_COLOR = "#71717a";

// The marker being *placed* in pick mode. Amber deliberately: none of the
// three category colours above, and not --color-accent either, which is the
// same #2563eb transport already uses. It is also drawn as a ring with a
// centre dot rather than as a plain disc, so it reads as "the point you are
// setting" rather than as a fourth category.
const PICK_MARKER_COLOR = "#ea580c";

// Coordinates are emitted at 6 decimals (~11cm). Without this a map click
// writes a 17-digit float straight into a number input.
const PICK_PRECISION = 1e6;

// "You are here". Not a category colour and not the pick amber either - this
// marker means something different from both, and the accuracy ring is the
// same hue at low opacity so the two read as one thing.
const HERE_MARKER_COLOR = "#0891b2";

// The GeoJSON source and layer prefix for the accuracy ring.
const ACCURACY_SOURCE = "here-accuracy";

// Zoom used when centring on the device's position. Closer than
// SINGLE_MARKER_ZOOM: you know roughly where you are, so the useful question
// is what is on the next street, not which region this is.
const HERE_ZOOM = 15;

// Every marker in this component is drawn as a CSS dot rather than an image,
// and under MapLibre that is simply what a marker *is*: `new Marker({element})`
// takes a DOM node and positions it. The library's own default marker is an
// inline SVG, so there are no icon assets to vendor either way -- which is one
// of the small things that got easier in the swap. Leaflet's default marker
// was an <img> whose src resolved against the *page* URL, meaning
// /trips/<id>/locations/marker-icon.png in an SPA, answered with the app's own
// HTML and rendered as a broken image; sidestepping that is no longer a
// consideration.
//
// MapLibre adds `maplibregl-marker` and an anchor class to whatever element it
// is handed, and writes `transform` on it to place it, leaving everything else
// alone -- so the inline styles below survive and the tests can still select a
// marker by class.
function markerElement(category) {
  const color = CATEGORY_COLORS[category] || FALLBACK_MARKER_COLOR;
  return styled(
    `display:block;width:1rem;height:1rem;border-radius:50%;background:${color};` +
      `border:2px solid white;box-shadow:0 0 2px rgba(0,0,0,.5)`
  );
}

function pickMarkerElement() {
  return styled(
    `display:block;width:1.5rem;height:1.5rem;border-radius:50%;box-sizing:border-box;` +
      `border:4px solid ${PICK_MARKER_COLOR};background:rgba(255,255,255,.85);` +
      `box-shadow:0 0 3px rgba(0,0,0,.6)`
  );
}

function hereMarkerElement() {
  return styled(
    `display:block;width:1rem;height:1rem;border-radius:50%;box-sizing:border-box;` +
      `background:${HERE_MARKER_COLOR};border:3px solid white;` +
      `box-shadow:0 0 4px rgba(0,0,0,.6)`
  );
}

function styled(css) {
  const el = document.createElement("span");
  el.style.cssText = css;
  return el;
}

// The accuracy ring, as a polygon.
//
// MapLibre has no metre-radius circle: a `circle` layer's radius is in screen
// pixels, so it would be a fixed dot that means a different distance at every
// zoom -- the opposite of what this ring is for. Approximating the circle in
// degrees instead keeps it correct on the ground at any zoom, with no
// expression arithmetic to re-derive when the position changes.
//
// 111320 is metres per degree of latitude; longitude is that scaled by
// cos(latitude), which is what keeps the ring circular rather than an ellipse
// away from the equator.
const RING_VERTICES = 64;

function accuracyRing(lat, lng, radiusMetres) {
  const dLat = radiusMetres / 111320;
  const dLng = radiusMetres / (111320 * Math.cos((lat * Math.PI) / 180));
  const ring = [];
  for (let i = 0; i <= RING_VERTICES; i++) {
    const theta = (i / RING_VERTICES) * 2 * Math.PI;
    ring.push([lng + dLng * Math.sin(theta), lat + dLat * Math.cos(theta)]);
  }
  return { type: "Feature", geometry: { type: "Polygon", coordinates: [ring] }, properties: {} };
}

const styles = `
  /* A column flex box, not a plain block: the map fills the height and the
     two-finger hint below it takes its own, so adding that line can't push
     the map past :host's height at any width. */
  :host {
    display: flex;
    flex-direction: column;
    height: 60vh;
    min-height: 24rem;
    --map-height: 100%;
  }
  :host([lat]) {
    height: 16rem;
    min-height: 0;
  }
  /* After :host([lat]) on purpose - equal specificity, so source order wins.
     A picker inside an editor card is the same size whether or not it has
     coordinates yet; without this it would be 16rem once a point exists and
     60vh before that, which is a form card that jumps on first click. */
  :host([pick]) {
    height: 20rem;
    min-height: 0;
  }
  .map-wrap {
    position: relative;
    flex: 1;
    /* A flex item's default min-height: auto would let the map's content
       floor the box instead of the flex basis doing it. */
    min-height: 0;
    /* Kept from the Leaflet era, for a weaker reason than it had then.
       Leaflet parked internal helpers at very large offsets (measured at
       right=1825757) and briefly widened the document by 1636px mid-animation,
       which the UI suite caught as a page-level overflow. MapLibre draws into
       one canvas and does no such thing. This stays because the component
       still should not be able to widen the document whatever the library does
       -- the popup and the attribution control both live in here and are sized
       by their own content. */
    overflow: hidden;
  }
  /* --map-height is what the map box measures, and it exists so the gesture
     overlay can be exactly that tall without restating the expression. At wide
     widths the legend is absolutely positioned, so .map-wrap *is* the map and
     100% is right; the mobile block below puts the legend into the flow above
     the map, at which point "the whole wrapper" and "the map" stop being the
     same box - and an overlay pinned to inset: 0 would dim the legend too. */
  #map {
    height: var(--map-height);
    border-radius: 0.5rem;
  }
  .legend {
    /* base.css's global box-sizing reset doesn't pierce this shadow root,
       and the mobile override below adds width: 100% - left at the browser
       default (content-box), that width plus this padding and border would
       push past the container's right edge. */
    box-sizing: border-box;
    position: absolute;
    top: 0.5rem;
    right: 0.5rem;
    z-index: 1000;
    background: var(--color-surface, #fff);
    border: 1px solid var(--color-border, #ccc);
    border-radius: 0.375rem;
    padding: 0.5rem 0.75rem;
    font-size: 0.8rem;
    display: flex;
    flex-direction: column;
    gap: 0.25rem;
  }
  .legend label {
    display: flex;
    align-items: center;
    gap: 0.4rem;
    cursor: pointer;
  }
  .dot {
    width: 0.6rem;
    height: 0.6rem;
    border-radius: 50%;
    display: inline-block;
  }
  /* The "nothing has a location yet" message, laid over the map.
     
     pointer-events: none is the whole point of this rule existing rather than
     the positioning living in an inline style, as it did from Caravel v1 until
     Stage 23: absolutely positioned across the entire wrapper and hit-testable,
     it swallowed every mouse event the map should have had, so a trip with no
     locations had a map that could not be dragged, clicked or interacted with
     at all. Nothing pointed at the message as the cause -- it reads as a label,
     not as a sheet of glass over the map. */
  .empty {
    position: absolute;
    inset: 0;
    margin: 0;
    padding: 1rem;
    color: var(--color-text-muted, #666);
    pointer-events: none;
  }
  /* The popup's two destinations - this location in Caravel, and Google Maps.
     Blocks rather than inline, so each is its own row under the title and the
     tap-target height below has something to apply to. */
  .popup-link {
    display: block;
  }
  /* The locate control, overlaid on the map like a map control should be.
     Bottom left: the zoom control sits top left, the legend top right and the
     attribution bottom right, so this is the one free corner. Unchanged by the
     MapLibre swap -- it puts its controls in the same corners. */
  .locate {
    position: absolute;
    bottom: 0.5rem;
    left: 0.5rem;
    z-index: 1000;
    display: flex;
    align-items: center;
    justify-content: center;
    gap: 0.4rem;
    /* A map control is a tap target like any other, at every width - not just
       under 640px. Nothing here is ever reachable with a mouse only. */
    min-width: var(--tap-min, 2.75rem);
    min-height: var(--tap-min, 2.75rem);
    padding: 0 0.6rem;
    border: 1px solid var(--color-border, #ccc);
    border-radius: 0.375rem;
    background: var(--color-surface, #fff);
    color: var(--color-text, #111);
    font: inherit;
    font-size: 0.8rem;
    cursor: pointer;
  }
  .locate[disabled] {
    cursor: not-allowed;
    opacity: 0.6;
  }
  .locate .icon {
    width: 1.1rem;
    height: 1.1rem;
    /* The sprite's symbols are strokes with no fill, so they are invisible
       without this - base.css says the same for .icon outside the shadow. */
    fill: none;
    stroke: currentColor;
    stroke-width: 2;
    stroke-linecap: round;
    stroke-linejoin: round;
  }
  .locate-status {
    margin: 0.5rem 0 0;
    font-size: 0.8rem;
    color: var(--color-text-muted, #666);
  }
  .locate-status[hidden] {
    display: none;
  }

  /* Rendered only on coarse pointers (see render()), where one-finger drag
     deliberately no longer pans the map. */
  /* The gesture hint, shown *when the gesture happens* rather than standing
     permanently under the map (Stage 23 Milestone 6). Two things it must never
     do: cover the map for longer than it takes to read, and eat the gesture it
     is describing - hence pointer-events: none, so a second wheel or a
     two-finger pan goes straight through it to the map underneath.

     role="status" and aria-live="polite" rather than an alert: it is an
     explanation of what just did not happen, and interrupting a screen reader
     mid-sentence for it would be worse than useless. */
  .gesture-hint {
    position: absolute;
    /* Pinned to the bottom of the wrapper and given the map's own height,
       rather than inset: 0 - see --map-height above. */
    inset: auto 0 0 0;
    height: var(--map-height);
    /* base.css's global border-box reset does not pierce this shadow root -
       the same trap the legend rule below records. Without this the 1rem
       padding is added to the height and the overlay stands 32px taller than
       the map, dimming the legend above it. */
    box-sizing: border-box;
    z-index: 500;
    margin: 0;
    display: flex;
    align-items: center;
    justify-content: center;
    padding: 1rem;
    text-align: center;
    font-size: 0.9rem;
    font-weight: 500;
    color: #fff;
    background: rgba(0, 0, 0, 0.55);
    /* The overlay is not a control, so it does not take focus and does not
       need a focus ring; it is announced instead. */
    pointer-events: none;
    opacity: 1;
    transition: opacity 150ms ease-out;
  }
  /* MapLibre's own cooperative-gesture overlay, suppressed in favour of
     .gesture-hint above.
     Not a styling preference. Its screen is aria-hidden="true" with no
     role="status", so it announces nothing; it picks its wording from the user
     agent (Windows vs Mac help text) rather than from which modifier was
     actually pressed; and it runs on its own ~2s opacity transition rather
     than GESTURE_HINT_MS. The hint above is the app's accessibility contract
     and is what both gesture specs assert on, so having two overlays saying
     the same thing differently would be strictly worse than one. The handler
     itself stays on -- it is what makes a one-finger drag scroll the page. */
  .maplibregl-cooperative-gesture-screen {
    display: none;
  }
  .gesture-hint[hidden] {
    /* [hidden] alone would be display:none, which cannot transition. Keeping
       it in the layout and fading it out is what makes the appearance and the
       disappearance both readable. */
    display: flex;
    opacity: 0;
  }
  @media (prefers-reduced-motion: reduce) {
    .gesture-hint {
      transition: none;
    }
  }

  /* On narrow viewports the legend, absolutely positioned over the map's
     top-right corner at wider widths, covered over half the map's width. It
     comes out of the overlay and into the flow instead. :host([lat]) (the
     single-marker mode used on the location view page, no legend there) is
     more specific than a plain :host, so it's unaffected by this at any
     width.
     Two things here are the fix for Stage 07's "the map swallows the page
     scroll" bug (stage-13.md Milestone 1), not styling preference:
     - the map is capped rather than taking a flat 50vh. At 324x756 the old
       rule left ~67px of page below a 424px map, so a drag starting anywhere
       in the lower half had nowhere to go but the map;
     - the legend sits *above* the map (order: -1) rather than after it. Below
       it, it landed at y=769 - just past the fold, with nothing suggesting it
       existed - and it doubles as a strip of non-map page to start a drag in. */
  @media (max-width: 640px) {
    :host {
      height: auto;
    }
    .map-wrap {
      display: flex;
      flex-direction: column;
      height: auto;
      flex: none;
    }
    /* One rule used to set the height for all three mounts, which is why the
       trip map was capped at 20rem: raising the number would have put a
       full-height map inside the editor's form card. Mode by mode instead.

       The cap on the trip map is gone. This entry has carried a warning since
       Stage 21 that the cap was the Stage 13 fix for the map swallowing the
       page scroll -- but that reasoning predates the coarse-pointer drag fix
       landing in the same milestone, and a one-finger drag over the map is
       never consumed at any height (Stage 30 replaced the mechanism with
       MapLibre's cooperativeGestures, which keeps that property). The legend
       above the map stays because that is where the filters live, not because
       the page needs somewhere to be dragged from.

       85vh rather than 100vh: enough that the map is the screen, with a strip
       of page left at the bottom so it is visible that there is more below,
       and so the scroll position after a page load does not look like a
       full-bleed map with no context. */
    :host {
      --map-height: 85vh;
    }
    /* The other two mounts sit inside a page of other content and keep the
       heights their own desktop rules give them -- which is also a small
       correction: the single blanket rule was *inflating* both of them to
       320px on a phone. [pick] after [lat] on purpose, the same equal-
       specificity source-order point the desktop rules make: a picker with
       coordinates set matches both. */
    :host([lat]) {
      --map-height: 16rem;
    }
    :host([pick]) {
      --map-height: 20rem;
    }
    .legend {
      position: static;
      order: -1;
      flex-direction: row;
      flex-wrap: wrap;
      width: 100%;
      margin-bottom: 0.5rem;
    }
    /* The legend's category toggles are tap targets like any other, and at
       20px they were among the smallest in the app (Stage 09 Milestone 6).
       The label carries the height rather than the checkbox inside it, for
       the reason base.css's mobile block gives: a native checkbox blown up
       to 44px looks wrong, and clicking the label toggles it anyway.
       --tap-min is inherited through the shadow boundary (custom properties
       do pierce it, unlike the box-sizing reset noted above), with a literal
       fallback in case this component is ever used without base.css. */
    .legend label {
      min-height: var(--tap-min, 2.75rem);
    }
    /* Same reasoning, and it needs stating here rather than being caught by
       the sweep: routes.spec.js's tap-target check skips anything under a
       maplibregl-* class, and popup content is rendered inside
       .maplibregl-popup-content - so these links are invisible to it. They are
       asserted directly in map.spec.js instead. */
    .popup-link {
      min-height: var(--tap-min, 2.75rem);
      display: flex;
      align-items: center;
    }
  }
`;

class LeafletMap extends HTMLElement {
  static get observedAttributes() {
    return ["trip-id", "lat", "lng", "marker-title", "marker-address", "marker-category", "pick", "locate"];
  }

  connectedCallback() {
    if (!this.shadowRoot) this.attachShadow({ mode: "open" });
    this._activeCategories = new Set(["site", "stay", "transport"]);
    this._markers = [];
    this.load();
  }

  attributeChangedCallback() {
    if (this.isConnected) this.load();
  }

  // Leaflet needed no teardown worth the name. MapLibre holds a WebGL context
  // and a worker, and a browser will start killing the oldest contexts once
  // enough are live - so a map that is navigated away from has to say so.
  disconnectedCallback() {
    this._generation = (this._generation || 0) + 1;
    this.destroyMap();
  }

  destroyMap() {
    this._map?.remove();
    this._map = null;
    this._markers = [];
    this._pickMarker = null;
    this._hereMarker = null;
  }

  // Shown instead of a map when a context cannot be had at all. Reuses the
  // .empty overlay the trip map already has for "nothing has a location yet":
  // both are the same situation from the reader's side - a rectangle where a
  // map should be, with a sentence saying why.
  showMapUnavailable(err) {
    console.warn("map unavailable", err);
    const wrap = this.shadowRoot.querySelector(".map-wrap");
    if (!wrap) return;
    wrap.querySelector(".empty")?.remove();
    const p = document.createElement("p");
    p.className = "empty";
    p.textContent = t("map.unavailable");
    wrap.appendChild(p);
  }

  async load() {
    // For a parser-inserted element with preset attributes, attributeChangedCallback
    // can fire before connectedCallback attaches the shadow root (the node is
    // already isConnected, but the reaction that calls attachShadow hasn't run
    // yet) - bail out here and let connectedCallback's own load() call (which
    // runs after the shadow root exists) handle it instead.
    if (!this.shadowRoot) return;

    // Pick mode is settled before anything else, because it also reads
    // lat/lng - but as a starting point that is meant to be moved, rather
    // than as the fixed embed the single-marker branch below renders. It
    // never fetches anything.
    this._pick = this.hasAttribute("pick");
    if (this._pick) {
      // Once the map exists, a lat/lng change must only move the marker.
      // The editor rewrites those attributes as its coordinate fields are
      // typed in, and the inherited behaviour - a full load() and a fresh
      // innerHTML - would tear the map down and refetch tiles per keystroke.
      if (this._map) {
        this.syncPickMarker();
        return;
      }
      this._singleMarker = null;
      this._items = [];
      await this.render((this._generation = (this._generation || 0) + 1));
      return;
    }

    // connectedCallback and attributeChangedCallback both fire for the
    // initial attributes, so two loads can race; only the most recent one
    // is allowed to touch the DOM once its awaits resolve.
    const generation = (this._generation = (this._generation || 0) + 1);

    // A render builds a new map, so whatever the person did to the previous
    // one does not carry over.
    this._userMovedMap = false;

    const lat = this.getAttribute("lat");
    const lng = this.getAttribute("lng");
    if (lat != null && lng != null) {
      // Single-marker mode: an item's own location page embeds one point,
      // driven directly by attributes - no trip-wide fetch, no legend.
      this._singleMarker = {
        lat: Number(lat),
        lng: Number(lng),
        title: this.getAttribute("marker-title") || "",
        // Only for the outbound Google Maps link, which names the place rather
        // than dropping a pin on a coordinate. Nothing is drawn from it.
        address: this.getAttribute("marker-address") || "",
        category: this.getAttribute("marker-category") || "",
      };
      this._items = [];
      await this.render(generation);
      return;
    }
    this._singleMarker = null;

    const tripId = this.getAttribute("trip-id");
    if (!tripId) return;

    const items = await api.get(`/trips/${tripId}/map`);
    if (generation !== this._generation) return;
    this._items = items;
    await this.render(generation);
  }

  async render(generation) {
    // Before the innerHTML below detaches its container, not after: a map torn
    // down with its container already gone still releases the GL context, but
    // only by the library's good manners. Doing it in the right order means
    // not depending on them.
    this.destroyMap();

    // Cleared up front and set again on the map's own `load`: the library is
    // lazily imported inside this method, so between "the route's fetches have
    // settled" and "the map has laid itself out" there is a window in which
    // the component is on the page but half-built. The UI sweeps used to
    // measure that window under load and report un-sized map controls as
    // content overflowing .map-wrap. This is the component stating its own
    // readiness rather than the suite guessing - the small version of the
    // "ready signal" todo.md asks for app-wide.
    this.removeAttribute("data-ready");
    const single = this._singleMarker;
    // The legend filters trip-wide markers, of which pick mode has none - and
    // so does the "nothing has a location yet" line below.
    const chromeless = single || this._pick;
    this.shadowRoot.innerHTML = `
      <link rel="stylesheet" href="/js/vendor/maplibre/maplibre-gl.css" />
      <style>${styles}</style>
      <div class="map-wrap">
        <div id="map"></div>
        <p class="gesture-hint" role="status" aria-live="polite" hidden></p>
        ${
          chromeless
            ? ""
            : `<div class="legend">
          ${["site", "stay", "transport"]
            .map(
              (cat) => `
              <label>
                <input type="checkbox" data-category="${cat}" checked />
                <span class="dot" style="background:${CATEGORY_COLORS[cat]}"></span>
                ${t(`item.category.${cat}`)}
              </label>`
            )
            .join("")}
        </div>`
        }
        ${
          this.hasAttribute("locate")
            ? `<button type="button" class="locate" data-action="locate">${icon("locate-fixed")}<span>${t("map.locate.label")}</span></button>`
            : ""
        }
      </div>
      ${this.hasAttribute("locate") ? `<p class="locate-status" role="status" hidden></p>` : ""}
    `;

    if (!chromeless && !this._items.length) {
      this.shadowRoot.querySelector(".map-wrap").insertAdjacentHTML(
        "beforeend",
        `<p class="empty">${t("map.empty")}</p>`
      );
    }

    // Lazy-load MapLibre only when the map is actually shown, keeping it out
    // of the initial page weight for users who never open the Map tab. That
    // mattered with Leaflet and matters more now: the vendored library is
    // ~1.1MB across three modules against Leaflet's 440KB.
    //
    // The tile config is fetched alongside it rather than after: both are
    // needed before the first tile can be requested, so serialising them would
    // cost a round trip, and awaiting the config *after* constructing the map
    // would leave a constructed, tile-less map behind whenever this render is
    // superseded mid-fetch.
    const [maplibre, tiles] = await Promise.all([
      import("../vendor/maplibre/maplibre-gl.mjs"),
      loadTileConfig(),
    ]);
    if (generation !== this._generation) return;
    this._maplibre = maplibre;

    const style = await buildStyle(tiles);
    if (generation !== this._generation) return;

    const mapEl = this.shadowRoot.getElementById("map");

    let map;
    try {
      map = new maplibre.Map({
        container: mapEl,
        style,
        // MapLibre needs a view up front, where Leaflet tolerated a map with
        // none until setView. This is the same world view the empty trip map
        // and the coordinate-less picker settle on anyway.
        center: [0, 20],
        zoom: 2,
        // The default attribution control credits MapLibre itself and hides
        // behind a toggle under 640px. compact: false keeps the operator's
        // configured credit visible, which is what every provider's terms
        // actually ask for; customAttribution is how that credit gets in,
        // since it no longer travels with a tile layer. It is rendered as
        // markup - MapLibre strips <script>, on* and javascript:/data: URLs
        // and leaves the rest - so the "attribution is HTML" contract that
        // internal/httpapi/map.go documents survives the swap unchanged.
        attributionControl: { compact: false, customAttribution: tiles.tile_attribution },
        // The wheel is handled entirely in bindGestureGate below - see the
        // reasoning there. The library's own handler being off is what makes
        // the gesture deterministic: there is exactly one piece of code
        // deciding what a wheel does. This also sidesteps MapLibre's
        // cooperative-gesture bypass key, which is metaKey only on a Mac user
        // agent and so would drop Meta support everywhere else.
        scrollZoom: false,
        // "One finger scrolls the page, two fingers work the map" - the other
        // half of Stage 07's "the map swallows the page scroll" fix, and now a
        // supported option rather than something assembled from handler flags.
        // Note it is NOT dragPan: false, which was the obvious translation of
        // Leaflet's dragging: false and is wrong: MapLibre routes touch
        // panning of *any* finger count through dragPan, so turning it off
        // would take the two-finger pan with it. cooperativeGestures raises
        // the handler's minimum to two touches and puts touch-action on the
        // canvas container so the browser scrolls the page for the first one.
        // It applies on every pointer type, so the matchMedia check the old
        // code needed is gone; mouse dragging is unaffected.
        cooperativeGestures: true,
        // Rotation and pitch are capabilities Leaflet never had, on by default
        // here, and nothing in this app wants them: a tilted map with north
        // somewhere other than up is a new way to be lost, and two fingers on
        // a phone would reach for it constantly.
        dragRotate: false,
        touchPitch: false,
        pitchWithRotate: false,
        rollEnabled: false,
      });
    } catch (err) {
      // MapLibre v6 requires WebGL2 and throws when it cannot get a context.
      // Failing here must still finish the render: data-ready is what every
      // route sweep in the UI suite blocks on, so leaving it off would turn a
      // missing GPU into a 15s timeout on every page with a map rather than
      // into a message.
      this.showMapUnavailable(err);
      this.setAttribute("data-ready", "");
      return;
    }
    this._map = map;
    map.touchZoomRotate.disableRotation();
    map.keyboard.disableRotation();

    // Has the person moved this map themselves? syncPickMarker needs to know,
    // so that typing coordinates into the form can zoom to them without
    // yanking a map somebody has already positioned.
    //
    // Real input events rather than the map's own move/zoom events, and that
    // is the point: jumpTo fires those too, so a flag fed by them would be set
    // by our *own* recentring - including the world view this very component
    // takes when it opens with no coordinates, which would disable the
    // feature outright. mousedown covers dragging, the zoom buttons and
    // double-click zoom; wheel covers wheel zoom; touchstart covers pinch;
    // keydown covers the arrow and +/- keys. None of them can be raised by
    // jumpTo.
    const noteUserMovedMap = () => {
      this._userMovedMap = true;
    };
    mapEl.addEventListener("mousedown", noteUserMovedMap);
    mapEl.addEventListener("touchstart", noteUserMovedMap, { passive: true });
    mapEl.addEventListener("keydown", noteUserMovedMap);
    // A wheel counts only when it is the zoom gesture. Since Milestone 6 a
    // plain wheel scrolls the *page* and deliberately leaves the map alone,
    // so treating it as "the person positioned this map" would be wrong -- and
    // would quietly undo Milestone 5, by stopping typed coordinates from
    // zooming for anyone who had scrolled the page past the map first.
    mapEl.addEventListener(
      "wheel",
      (e) => {
        if (e.ctrlKey || e.metaKey) noteUserMovedMap();
      },
      { passive: true }
    );

    this.bindGestureGate(mapEl);

    this.plotMarkers();

    // Delegated, because popup DOM is built and destroyed on demand - there is
    // nothing to bind to until a marker is clicked.
    //
    // The router's own [data-link] interception cannot reach this. It is a
    // listener on `document` doing e.target.closest("[data-link]"), and a
    // click inside a shadow root retargets e.target to the <leaflet-map>
    // host, so the link would never be found and the click would fall
    // through to a full page load. Hence a listener on this side of the
    // boundary, dispatching the same "item-open" contract location-card.js
    // uses, which trip-detail-page.js turns into a navigation.
    mapEl.addEventListener("click", (e) => {
      const link = e.target.closest?.("[data-item-id]");
      if (!link) return;
      // A real <a href> on purpose (the reason itinerary-tab.js gives for its
      // entry links): middle-click, open-in-new-tab, "copy link address" and
      // the status bar all come free with a link and none of them with a
      // button. So only a plain left-click is intercepted - a modified one
      // falls through to the browser, which resolves the route fine.
      if (e.button !== 0 || e.metaKey || e.ctrlKey || e.shiftKey || e.altKey) return;
      e.preventDefault();
      this.dispatchEvent(
        new CustomEvent("item-open", {
          bubbles: true,
          composed: true,
          detail: { itemId: link.dataset.itemId },
        })
      );
    });

    if (this._pick) {
      // Click anywhere to place or move the point. The map's own click event
      // rather than a DOM listener, because it hands over the coordinate
      // already projected - and it does not fire on a marker drag, which has
      // its own handler in syncPickMarker.
      map.on("click", (e) => this.emitPick(e.lngLat));
    }

    // The touch half of the gesture hint, now driven by the handler that
    // actually made the decision rather than by a touchmove listener guessing
    // at it: MapLibre raises this whenever cooperative gestures swallow a
    // one-finger pan. The wheel half stays in bindGestureGate, because
    // scrollZoom is off and so no wheel_zoom event is ever emitted.
    map.on("cooperativegestureprevented", (e) => {
      if (e.gestureType === "touch_pan") this.showGestureHint(t("map.twoFingerHint"));
    });

    if (this.hasAttribute("locate")) this.bindLocate();

    // Ready means "the style is loaded and the first frame is on screen",
    // which is what `load` reports. That is a stronger claim than this
    // attribute used to make: it was set as soon as the map object existed,
    // and the honest version is what the UI suite wanted all along.
    map.on("load", () => {
      if (generation !== this._generation) return;
      this.setAttribute("data-ready", "");
    });

    if (!chromeless) {
      this.shadowRoot.querySelectorAll("[data-category]").forEach((cb) => {
        cb.addEventListener("change", () => {
          const cat = cb.getAttribute("data-category");
          if (cb.checked) this._activeCategories.add(cat);
          else this._activeCategories.delete(cat);
          this.plotMarkers();
        });
      });
    }
  }

  plotMarkers() {
    const maplibre = this._maplibre;
    if (!maplibre || !this._map) return;
    this._markers.forEach((m) => m.remove());
    this._markers = [];

    if (this._pick) {
      this.syncPickMarker({ initial: true });
      return;
    }

    if (this._singleMarker) {
      const { lat, lng, title, address, category } = this._singleMarker;
      const mapsUrl = googleMapsUrl(lat, lng, title, address);
      const marker = new maplibre.Marker({ element: markerElement(category) })
        .setLngLat([lng, lat])
        .setPopup(
          this.popup(
            `<strong>${escapeHtml(title)}</strong><br/><a href="${escapeAttr(mapsUrl)}" target="_blank" rel="noopener">${t("map.viewOnGoogleMaps")}</a>`
          )
        )
        .addTo(this._map);
      this._markers.push(marker);
      // jumpTo, not a flyTo - this is the first and only view this embed ever
      // takes, so there is nothing to animate *from*; the animation only
      // produced a transient wide layout (see .map-wrap's overflow note).
      this._map.jumpTo({ center: [lng, lat], zoom: SINGLE_MARKER_ZOOM });
      return;
    }

    const visible = this._items.filter((item) => this._activeCategories.has(item.category));

    // The trip-wide popup's primary action is opening the location *in
    // Caravel*; Google Maps is the secondary one. Until Stage 13 Milestone 2
    // only the latter existed, so a marker was a dead end - the payload has
    // carried item.id all along (mapItemResponse in internal/httpapi/map.go),
    // the popup just never used it.
    //
    // The single-marker branch above deliberately gets no such link: it is
    // embedded on that location's own page, so it would link to itself.
    const tripId = this.getAttribute("trip-id");

    for (const item of visible) {
      const marker = new maplibre.Marker({ element: markerElement(item.category) })
        .setLngLat([item.lng, item.lat])
        .setPopup(
          this.popup(
            `<strong>${escapeHtml(item.title)}</strong>` +
              `<a class="popup-link" data-item-id="${escapeAttr(item.id)}" href="${escapeAttr(`/trips/${tripId}/locations/${item.id}`)}">${t("map.openLocation")}</a>` +
              `<a class="popup-link" href="${escapeAttr(item.google_maps_url)}" target="_blank" rel="noopener">${t("map.viewOnGoogleMaps")}</a>`
          )
        )
        .addTo(this._map);
      this._markers.push(marker);
    }

    if (visible.length) {
      const bounds = visible.reduce(
        (b, i) => b.extend([i.lng, i.lat]),
        new maplibre.LngLatBounds([visible[0].lng, visible[0].lat], [visible[0].lng, visible[0].lat])
      );
      // maxZoom matters for a single marker (or several at the same spot):
      // the bounds are then zero-size and fitBounds would zoom all the way to
      // the tile layer's own maxZoom, past where most providers serve tiles
      // at all - a grey rectangle with one dot on it. 14 is the same zoom the
      // single-marker branch above picks deliberately.
      // padding is a number here, not Leaflet's [x, y] pair. maxZoom still
      // matters for a single marker (or several at the same spot): the bounds
      // are then zero-size and an unbounded fit would zoom past where any
      // provider serves anything.
      this._map.fitBounds(bounds, { padding: 32, maxZoom: SINGLE_MARKER_ZOOM, animate: false });
    } else {
      this._map.jumpTo({ center: [0, 20], zoom: 2 });
    }
  }

  // Popups, with the two defaults this app disagrees with.
  //
  // focusAfterOpen: false because moving focus into a popup on open scrolls
  // the page to it, and these open from a marker the person just clicked -
  // they are already looking at it. setHTML does no sanitising, so escapeHtml
  // and escapeAttr at the call sites remain the only escaping, exactly as they
  // were under Leaflet's bindPopup.
  popup(html) {
    return new this._maplibre.Popup({ offset: 12, focusAfterOpen: false }).setHTML(html);
  }

  // Pick mode's one marker. Deliberately not part of plotMarkers' other two
  // branches: it is the only marker that can be moved, and it has to survive
  // a lat/lng change instead of being torn down and rebuilt with the map.
  syncPickMarker({ initial = false } = {}) {
    const maplibre = this._maplibre;
    if (!maplibre || !this._map) return;

    const lat = readCoordinate(this, "lat");
    const lng = readCoordinate(this, "lng");

    if (lat === null || lng === null) {
      // Nothing chosen yet, or the field was cleared. The world view is the
      // same "we don't know where you mean" view the empty trip map takes.
      this._pickMarker?.remove();
      this._pickMarker = null;
      if (initial) this._map.jumpTo({ center: [0, 20], zoom: 2 });
      return;
    }

    const creating = !this._pickMarker;
    if (this._pickMarker) {
      this._pickMarker.setLngLat([lng, lat]);
    } else {
      // The marker is a control, so it has to be reachable and describable
      // without a mouse. Leaflet had keyboard/title/alt options for that;
      // MapLibre adds no accessibility affordances to an element it is handed,
      // so they go on the element directly.
      const el = pickMarkerElement();
      el.tabIndex = 0;
      el.setAttribute("role", "button");
      el.setAttribute("aria-label", t("map.pickMarkerLabel"));
      el.title = t("map.pickMarkerLabel");
      this._pickMarker = new maplibre.Marker({ element: el, draggable: true })
        .setLngLat([lng, lat])
        .addTo(this._map);
      this._pickMarker.on("dragend", () => this.emitPick(this._pickMarker.getLngLat()));
    }

    // Three cases, and the middle one is the fix for a real complaint.
    //
    // The old rule was "recentre on the first render, and afterwards only if
    // the point has moved out of sight". That left a hole exactly where it
    // mattered: an editor opened with no coordinates sits at the world view,
    // zoom 2, where *every* point on Earth is inside the bounds. So filling in
    // a latitude and longitude moved a pin the person could not see, on a map
    // that never zoomed in.
    //
    // So the marker first appearing on a map the person has not moved
    // themselves is treated like a first render. Not a zoom-level test: a
    // deliberate zoom-out to 2 would look identical to the untouched world
    // view. And not on every update either -- once the marker exists, the
    // out-of-sight rule takes over, so typing does not yank the map with each
    // keystroke.
    //
    // Coordinates that came *from* the map - a click, a marker drag - never
    // reach the first branch, because placing them required a mousedown on
    // the map, which is exactly what noteUserMovedMap watches for. That is
    // deliberate: somebody who clicks a spot at the zoom they chose should
    // keep that zoom.
    if (initial || (creating && !this._userMovedMap)) {
      this._map.jumpTo({ center: [lng, lat], zoom: SINGLE_MARKER_ZOOM });
    } else if (!this._map.getBounds().contains([lng, lat])) {
      this._map.jumpTo({ center: [lng, lat], zoom: this._map.getZoom() });
    }
  }

  // The locate control. Failing honestly is the requirement here, not an
  // edge case: over plain HTTP on a phone the geolocation API exists and
  // simply never calls back, so an unguarded button would spin forever with
  // nothing to show. locateUnavailableReason() answers that up front and the
  // button is disabled with the reason spelled out instead.
  bindLocate() {
    const button = this.shadowRoot.querySelector('[data-action="locate"]');
    const status = this.shadowRoot.querySelector(".locate-status");

    const say = (key) => {
      status.textContent = key ? t(key) : "";
      status.hidden = !key;
    };

    const blocked = locateUnavailableReason();
    if (blocked) {
      button.disabled = true;
      say(locateErrorKey(blocked));
      return;
    }

    button.addEventListener("click", async () => {
      button.disabled = true;
      say("map.locate.searching");
      try {
        const position = await getCurrentPosition();
        say(null);
        this.showPosition(position.lat, position.lng, position.accuracy);
        // The page decides what a position *means*: the trip map only shows
        // it, while the editor's picker takes it as the point being set. Same
        // control, one event, no second button to keep in step.
        this.dispatchEvent(
          new CustomEvent("position-found", {
            bubbles: true,
            composed: true,
            detail: position,
          })
        );
      } catch (err) {
        // Denied, unavailable and timed out are three different situations
        // and get three different sentences; anything else would tell the
        // user nothing about what to try next.
        say(locateErrorKey(err.reason || "unavailable"));
      } finally {
        button.disabled = false;
      }
    });
  }

  // "You are here": the point plus the accuracy the browser reported, drawn
  // as a ring. The ring is not decoration - a 2km fix and a 5m fix look
  // identical without it, and only one of them is worth acting on.
  showPosition(lat, lng, accuracy) {
    const maplibre = this._maplibre;
    if (!maplibre || !this._map) return;

    this._hereMarker?.remove();

    // Not draggable, unlike the pick marker: this is a reading, not a choice,
    // so dragging it would claim to move the device. A plain element takes no
    // focus and no pointer events of its own, so Leaflet's interactive: false
    // and keyboard: false have nothing to translate to.
    this._hereMarker = new maplibre.Marker({ element: hereMarkerElement() })
      .setLngLat([lng, lat])
      .addTo(this._map);

    this._hereAccuracy = Number.isFinite(accuracy) && accuracy > 0 ? accuracy : null;
    this.drawAccuracyRing(lat, lng);

    this._map.jumpTo({ center: [lng, lat], zoom: HERE_ZOOM });
  }

  // The ring, as a source and two layers rather than as an object with a
  // radius. Split out from showPosition because layers, unlike markers, do not
  // survive a style change - so this has to be callable again on its own.
  drawAccuracyRing(lat, lng) {
    const map = this._map;
    if (!map) return;
    const radius = this._hereAccuracy;
    if (!radius) {
      this._hereRing = null;
      this.removeAccuracyRing();
      return;
    }
    this._hereRingAt = { lat, lng };
    // Kept on the component as well as handed to the source. MapLibre offers
    // no public read-back of a GeoJSON source's data, and both the accuracy
    // test and a later re-add after a style change need the geometry that is
    // actually on the map rather than the number it was derived from.
    const data = (this._hereRing = accuracyRing(lat, lng, radius));
    const existing = map.getSource(ACCURACY_SOURCE);
    if (existing) {
      existing.setData(data);
      return;
    }
    map.addSource(ACCURACY_SOURCE, { type: "geojson", data });
    map.addLayer({
      id: `${ACCURACY_SOURCE}-fill`,
      type: "fill",
      source: ACCURACY_SOURCE,
      paint: { "fill-color": HERE_MARKER_COLOR, "fill-opacity": 0.12 },
    });
    map.addLayer({
      id: `${ACCURACY_SOURCE}-line`,
      type: "line",
      source: ACCURACY_SOURCE,
      paint: { "line-color": HERE_MARKER_COLOR, "line-width": 1 },
    });
  }

  removeAccuracyRing() {
    const map = this._map;
    if (!map || !map.getSource(ACCURACY_SOURCE)) return;
    for (const suffix of ["-fill", "-line"]) {
      const id = `${ACCURACY_SOURCE}${suffix}`;
      if (map.getLayer(id)) map.removeLayer(id);
    }
    map.removeSource(ACCURACY_SOURCE);
  }

  // A scroll that happens to pass under the cursor must not zoom the map.
  //
  // A stock scroll-wheel handler zooms on *any* wheel event, so a page scroll
  // that crossed the map turned into a zoom - and on a map that is most of the
  // screen, that is most scrolls. The embedded-Google-Maps
  // convention is the fix: the wheel zooms only while Ctrl (or Meta, for the
  // Mac) is held, and a plain wheel says so and scrolls the page.
  //
  // MapLibre has its own version of this (cooperativeGestures also gates the
  // wheel), and it is deliberately not used for the wheel half. Its bypass key
  // is metaKey only when the user agent says Mac, so Meta would stop working
  // everywhere else; and handing the wheel back to a library is re-opening the
  // failure that produced zoomByWheel below. cooperativeGestures stays on for
  // the *touch* half, where it is the only supported way to get one-finger
  // page scroll with two-finger pan.
  //
  // The listener sits on .map-wrap, in the capture phase, and that placement
  // is load-bearing. The library registers its own listeners on the map
  // container itself, and on a shared target the capture/bubble distinction
  // does not decide the order - registration order does. Capturing on the
  // *parent* means this runs first whatever the library did.
  bindGestureGate(mapEl) {
    const wrap = this.shadowRoot.querySelector(".map-wrap");
    if (!wrap) return;

    wrap.addEventListener(
      "wheel",
      (e) => {
        if (e.ctrlKey || e.metaKey) {
          // Ctrl + wheel is bound to page zoom in every browser, so this has
          // to be cancelled here, in the capture phase, before anything else
          // looks at it.
          e.preventDefault();
          this.zoomByWheel(e);
          return;
        }
        // A plain wheel is deliberately *not* prevented: the whole point is
        // that the page scrolls the way it would anywhere else. Only the map
        // is kept from seeing it.
        e.stopPropagation();
        this.showGestureHint(t("map.ctrlZoomHint"));
      },
      // passive: false explicitly. A wheel listener is one of the types
      // browsers may treat as passive by default, and a passive listener's
      // preventDefault is ignored - silently, apart from a console warning.
      { capture: true, passive: false }
    );

    // The touch half used to live here as a touchmove listener guarded by a
    // matchMedia check. It now hangs off the map's own
    // cooperativegestureprevented event in render(), which fires from the code
    // that actually swallowed the pan - no guessing at pointer type, and no
    // risk of the hint and the behaviour disagreeing.
  }

  // Zoom the map for a Ctrl-held wheel.
  //
  // The library's own scroll-wheel handler is switched off and this replaces
  // it, after two rounds of the gesture not working on the reporter's machine
  // while working everywhere it could be measured. Carried over to MapLibre
  // unchanged, deliberately: its wheel handler uses the same
  // accumulate-normalise-sigmoid shape Leaflet's did, so the swap is no reason
  // to believe the original problem would not come back.
  //
  // Be honest about what is and is not known here. The failure was real and
  // reproducible for them - Ctrl + wheel zoomed nothing, in both the version
  // that passed the event to Leaflet untouched and the version that only
  // cancelled the browser default first - and it could not be reproduced here
  // at all: driven through Playwright, Leaflet zooms correctly for line,
  // pixel and page deltas, with or without a horizontal component. So the
  // mechanism inside Leaflet is *unknown*. Two theories were checked against
  // the running code and both were wrong: getWheelDelta does not discard an
  // event carrying deltaX (the deltaY branches are tested first), and
  // _performZoom's sigmoid cannot round a nonzero delta to no zoom while
  // zoomSnap is 1. Do not repeat either as the explanation.
  //
  // What this does instead is shrink the surface. Leaflet decides how far to
  // zoom from an accumulated, normalised, sigmoid-shaped magnitude; this uses
  // only the *direction*, which every device and every deltaMode agrees on.
  // Magnitude decides pacing and nothing else.
  // Deltas are normalised to pixels with Leaflet's own factors, accumulated,
  // and every 60px is one zoom level -- so one notch of a mouse wheel is one
  // level, and a trackpad glides rather than leaping. The accumulator is
  // clamped so a flick cannot bank a dozen levels, and reset after each step
  // so the next notch starts fresh.
  zoomByWheel(e) {
    const map = this._map;
    if (!map) return;

    // Lines and pages converted with Leaflet's own factors, so the feel is
    // unchanged for the devices that were already working.
    const px = e.deltaMode === 1 ? e.deltaY * 20 : e.deltaMode === 2 ? e.deltaY * 60 : e.deltaY;
    if (!px) return;

    const banked = (this._wheelAccum || 0) + px;
    this._wheelAccum = Math.max(-WHEEL_ACCUM_CAP, Math.min(WHEEL_ACCUM_CAP, banked));
    if (Math.abs(this._wheelAccum) < WHEEL_PX_PER_ZOOM) return;

    // deltaY is positive scrolling *down*, which is zooming out.
    const step = this._wheelAccum > 0 ? -1 : 1;
    this._wheelAccum = 0;

    // `around` rather than a plain zoom: the point under the cursor stays
    // under the cursor, which is what makes wheel zoom feel like a map rather
    // than a slideshow. It is Leaflet's setZoomAround under another name, with
    // the container-relative point worked out by hand because MapLibre has no
    // mouseEventToContainerPoint.
    //
    // duration must stay well under the 350ms the wheel specs wait before
    // measuring; the library's default ease is 300ms, which would race them.
    const rect = map.getCanvasContainer().getBoundingClientRect();
    map.easeTo({
      zoom: map.getZoom() + step,
      around: map.unproject([e.clientX - rect.left, e.clientY - rect.top]),
      duration: 150,
    });
  }

  // Show the overlay, and take it away again. Re-triggering while it is up
  // restarts the clock rather than stacking timers, so holding a scroll does
  // not leave it flickering.
  showGestureHint(message) {
    const hint = this.shadowRoot.querySelector(".gesture-hint");
    if (!hint) return;
    hint.textContent = message;
    hint.hidden = false;
    clearTimeout(this._hintTimer);
    this._hintTimer = setTimeout(() => {
      hint.hidden = true;
    }, GESTURE_HINT_MS);
  }

  // The component's one output in pick mode. Composed, because it has to
  // cross the shadow boundary to reach the page that mounted it.
  emitPick(lngLat) {
    // Panning the world sideways gives longitudes past +/-180; wrap() folds
    // them back, so a click three worlds to the right still stores a
    // coordinate a database and a map tile server both accept.
    const { lat, lng } = new this._maplibre.LngLat(lngLat.lng, lngLat.lat).wrap();
    const round = (n) => Math.round(n * PICK_PRECISION) / PICK_PRECISION;
    this.dispatchEvent(
      new CustomEvent("location-picked", {
        bubbles: true,
        composed: true,
        detail: { lat: round(lat), lng: round(lng) },
      })
    );
  }
}

// A coordinate attribute as a number, or null when it is absent, blank or
// unparseable. Number("") and Number(null) are both 0 - a real place in the
// Gulf of Guinea - so the emptiness check cannot be skipped.
function readCoordinate(el, name) {
  const raw = el.getAttribute(name);
  if (raw == null || raw.trim() === "") return null;
  const n = Number(raw);
  return Number.isFinite(n) ? n : null;
}

function escapeHtml(s) {
  return String(s).replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[c]);
}

function escapeAttr(s) {
  return escapeHtml(s);
}

customElements.define("leaflet-map", LeafletMap);
