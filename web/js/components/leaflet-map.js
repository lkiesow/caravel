import { api } from "../api.js";
import { t } from "../i18n.js";
import { icon } from "../icon.js";
import { getCurrentPosition, locateErrorKey, locateUnavailableReason } from "../geolocation.js";

// The tile layer, as the instance has it configured. Defaults duplicated from
// internal/httpapi/map.go on purpose: they are what the map falls back to when
// the request fails, and a Map tab of grey squares is a worse answer to "the
// config endpoint is briefly unreachable" than the tiles Caravel shipped with.
const DEFAULT_TILE_CONFIG = {
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

// Zoom used when centring on the device's position. Closer than
// SINGLE_MARKER_ZOOM: you know roughly where you are, so the useful question
// is what is on the next street, not which region this is.
const HERE_ZOOM = 15;

// Every marker in this component is drawn as a CSS dot rather than an image.
// Leaflet's default marker is an <img> whose src is resolved relative to the
// *page* URL, which in an SPA means /trips/<id>/locations/marker-icon.png -
// answered with the app's HTML and rendered as a broken image. Sidestepping
// the icon assets entirely also keeps the vendored Leaflet copy image-free.
function markerIcon(L, category) {
  const color = CATEGORY_COLORS[category] || FALLBACK_MARKER_COLOR;
  return L.divIcon({
    className: "",
    html: `<span style="display:block;width:1rem;height:1rem;border-radius:50%;background:${color};border:2px solid white;box-shadow:0 0 2px rgba(0,0,0,.5)"></span>`,
    iconSize: [16, 16],
  });
}

function pickMarkerIcon(L) {
  return L.divIcon({
    className: "",
    html:
      `<span style="display:block;width:1.5rem;height:1.5rem;border-radius:50%;box-sizing:border-box;` +
      `border:4px solid ${PICK_MARKER_COLOR};background:rgba(255,255,255,.85);` +
      `box-shadow:0 0 3px rgba(0,0,0,.6)"></span>`,
    iconSize: [24, 24],
  });
}

function hereMarkerIcon(L) {
  return L.divIcon({
    className: "",
    html:
      `<span style="display:block;width:1rem;height:1rem;border-radius:50%;box-sizing:border-box;` +
      `background:${HERE_MARKER_COLOR};border:3px solid white;` +
      `box-shadow:0 0 4px rgba(0,0,0,.6)"></span>`,
    iconSize: [16, 16],
  });
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
    /* Leaflet parks internal helpers (.leaflet-proxy, the zoom-animation
       panes) at very large offsets - measured at right=1825757 on a settled
       page. .leaflet-container clips them once it is initialised, but during
       init and zoom animation they briefly contribute to layout, and the UI
       suite caught that as a page-level horizontal overflow of 1636px against
       a 1280px viewport. The component should not be able to widen the
       document whatever the library does mid-animation. */
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
  .empty {
    padding: 1rem;
    color: var(--color-text-muted, #666);
  }
  /* The popup's two destinations - this location in Caravel, and Google Maps.
     Blocks rather than inline, so each is its own row under the title and the
     tap-target height below has something to apply to. */
  .popup-link {
    display: block;
  }
  /* The locate control, overlaid on the map like a map control should be.
     Bottom left: Leaflet's zoom sits top left, the legend top right and the
     attribution bottom right, so this is the one free corner. */
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
    :host {
      /* max(...) rather than a separate min-height, so one expression is the
         whole answer and the overlay can share it. */
      --map-height: max(min(50vh, 20rem), 16rem);
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
       leaflet-* class, and Leaflet renders popup content inside
       .leaflet-popup-content - so these links are invisible to it. They are
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
    return ["trip-id", "lat", "lng", "marker-title", "marker-category", "pick", "locate"];
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

    // A render builds a new Leaflet map, so whatever the person did to the
    // previous one does not carry over.
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
    // Cleared up front and set again at the end: Leaflet is lazily imported
    // inside this method, so between "the route's fetches have settled" and
    // "the map has laid itself out" there is a window in which the component
    // is on the page but half-built. The UI sweeps used to measure that
    // window under load and report Leaflet's un-sized controls as content
    // overflowing .map-wrap. This is the component stating its own readiness
    // rather than the suite guessing - the small version of the "ready
    // signal" todo.md asks for app-wide.
    this.removeAttribute("data-ready");
    const single = this._singleMarker;
    // The legend filters trip-wide markers, of which pick mode has none - and
    // so does the "nothing has a location yet" line below.
    const chromeless = single || this._pick;
    this.shadowRoot.innerHTML = `
      <link rel="stylesheet" href="/js/vendor/leaflet/leaflet.css" />
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
        `<p class="empty" style="position:absolute;inset:0;margin:0;">${t("map.empty")}</p>`
      );
    }

    // Lazy-load Leaflet only when the map is actually shown, keeping it out
    // of the initial page weight for users who never open the Map tab. The
    // tile config is fetched alongside it rather than after: both are needed
    // before the first tile can be requested, so serialising them would cost
    // a round trip, and awaiting the config *after* L.map() would leave a
    // constructed, tile-less map behind whenever this render is superseded
    // mid-fetch.
    const [L, tiles] = await Promise.all([
      import("../vendor/leaflet/leaflet.esm.js"),
      loadTileConfig(),
    ]);
    if (generation !== this._generation) return;
    this._L = L;

    const mapEl = this.shadowRoot.getElementById("map");
    // dragging: false on touch is the other half of the "the map swallows the
    // page scroll" fix. With Leaflet's drag handler off, a one-finger drag is
    // not consumed by the map at all and scrolls the page; two fingers still
    // pan *and* zoom, because Leaflet's touchZoom handler (left enabled)
    // applies the pinch centre's delta even when the pinch scale is exactly 1
    // - see TouchZoom._onTouchMove in the vendored leaflet.esm.js. So this
    // needs no touchstart/touchend juggling of our own, which the Stage 13
    // plan had budgeted for: enabling dragging mid-touchstart would have been
    // too late for Leaflet's own listener to see that same gesture anyway.
    // scrollWheelZoom: false because the wheel is handled entirely in
    // bindGestureGate below -- see the reasoning there. Leaflet's own handler
    // being off is what makes the gesture deterministic: there is exactly one
    // piece of code deciding what a wheel does.
    const map = L.map(mapEl, {
      attributionControl: true,
      dragging: !isCoarsePointer(),
      scrollWheelZoom: false,
    });
    this._map = map;

    L.tileLayer(tiles.tile_url, {
      maxZoom: tiles.max_zoom,
      attribution: tiles.tile_attribution,
      // detectRetina does more than substitute {r}: it also halves tileSize
      // and bumps zoomOffset, so switching it on for a provider with no @2x
      // variant asks for four tiles where one would do. Gating on the
      // placeholder lets a provider that serves @2x opt in through its URL
      // alone, and leaves the default provider loading exactly what it did
      // before this was configurable.
      detectRetina: tiles.tile_url.includes("{r}"),
      // Covers both the 3-subdomain (a,b,c) and 4-subdomain conventions.
      // Leaflet ignores this entirely for a URL with no {s}.
      subdomains: "abcd",
    }).addTo(map);

    // Has the person moved this map themselves? syncPickMarker needs to know,
    // so that typing coordinates into the form can zoom to them without
    // yanking a map somebody has already positioned.
    //
    // Real input events rather than Leaflet's zoomend/dragend, and that is the
    // point: setView fires those too, so a flag fed by them would be set by
    // our *own* recentring - including the world view this very component
    // takes when it opens with no coordinates, which would disable the
    // feature outright. mousedown covers dragging, the zoom buttons and
    // double-click zoom; wheel covers wheel zoom; touchstart covers pinch;
    // keydown covers the arrow and +/- keys. None of them can be raised by
    // setView.
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

    // Delegated, because Leaflet builds and destroys popup DOM on demand -
    // there is nothing to bind to until a marker is clicked.
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
      // Click anywhere to place or move the point. Leaflet's own click event
      // rather than a DOM listener, because it hands over the latlng already
      // projected - and it does not fire on a marker drag, which has its own
      // handler in syncPickMarker.
      map.on("click", (e) => this.emitPick(e.latlng));
    }

    if (this.hasAttribute("locate")) this.bindLocate();

    this.setAttribute("data-ready", "");

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
    const L = this._L;
    this._markers.forEach((m) => m.remove());
    this._markers = [];

    if (this._pick) {
      this.syncPickMarker({ initial: true });
      return;
    }

    if (this._singleMarker) {
      const { lat, lng, title, category } = this._singleMarker;
      const mapsUrl = `https://www.google.com/maps/search/?api=1&query=${lat},${lng}`;
      const marker = L.marker([lat, lng], { icon: markerIcon(L, category) }).addTo(this._map);
      marker.bindPopup(
        `<strong>${escapeHtml(title)}</strong><br/><a href="${escapeAttr(mapsUrl)}" target="_blank" rel="noopener">${t("map.viewOnGoogleMaps")}</a>`
      );
      this._markers.push(marker);
      // animate: false - this is the first and only view this embed ever
      // takes, so there is nothing to animate *from*; the animation only
      // produced a transient wide layout (see .map-wrap's overflow note).
      this._map.setView([lat, lng], SINGLE_MARKER_ZOOM, { animate: false });
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
      const marker = L.marker([item.lat, item.lng], { icon: markerIcon(L, item.category) }).addTo(this._map);
      marker.bindPopup(
        `<strong>${escapeHtml(item.title)}</strong>` +
          `<a class="popup-link" data-item-id="${escapeAttr(item.id)}" href="${escapeAttr(`/trips/${tripId}/locations/${item.id}`)}">${t("map.openLocation")}</a>` +
          `<a class="popup-link" href="${escapeAttr(item.google_maps_url)}" target="_blank" rel="noopener">${t("map.viewOnGoogleMaps")}</a>`
      );
      this._markers.push(marker);
    }

    if (visible.length) {
      const bounds = L.latLngBounds(visible.map((i) => [i.lat, i.lng]));
      // maxZoom matters for a single marker (or several at the same spot):
      // the bounds are then zero-size and fitBounds would zoom all the way to
      // the tile layer's own maxZoom, past where most providers serve tiles
      // at all - a grey rectangle with one dot on it. 14 is the same zoom the
      // single-marker branch above picks deliberately.
      this._map.fitBounds(bounds, { padding: [32, 32], maxZoom: SINGLE_MARKER_ZOOM });
    } else {
      this._map.setView([20, 0], 2);
    }
  }

  // Pick mode's one marker. Deliberately not part of plotMarkers' other two
  // branches: it is the only marker that can be moved, and it has to survive
  // a lat/lng change instead of being torn down and rebuilt with the map.
  syncPickMarker({ initial = false } = {}) {
    const L = this._L;
    if (!L || !this._map) return;

    const lat = readCoordinate(this, "lat");
    const lng = readCoordinate(this, "lng");

    if (lat === null || lng === null) {
      // Nothing chosen yet, or the field was cleared. The world view is the
      // same "we don't know where you mean" view the empty trip map takes.
      this._pickMarker?.remove();
      this._pickMarker = null;
      if (initial) this._map.setView([20, 0], 2);
      return;
    }

    const creating = !this._pickMarker;
    if (this._pickMarker) {
      this._pickMarker.setLatLng([lat, lng]);
    } else {
      this._pickMarker = L.marker([lat, lng], {
        icon: pickMarkerIcon(L),
        draggable: true,
        // The marker is a control, so it has to be reachable and describable
        // without a mouse; Leaflet gives a draggable marker keyboard focus
        // but no name of its own.
        keyboard: true,
        title: t("map.pickMarkerLabel"),
        alt: t("map.pickMarkerLabel"),
      }).addTo(this._map);
      this._pickMarker.on("dragend", () => this.emitPick(this._pickMarker.getLatLng()));
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
      this._map.setView([lat, lng], SINGLE_MARKER_ZOOM, { animate: false });
    } else if (!this._map.getBounds().contains([lat, lng])) {
      this._map.setView([lat, lng], this._map.getZoom(), { animate: false });
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
    const L = this._L;
    if (!L || !this._map) return;

    this._hereMarker?.remove();
    this._hereCircle?.remove();

    this._hereMarker = L.marker([lat, lng], {
      icon: hereMarkerIcon(L),
      // Not draggable, unlike the pick marker: this is a reading, not a
      // choice, so dragging it would claim to move the device.
      interactive: false,
      keyboard: false,
    }).addTo(this._map);

    if (Number.isFinite(accuracy) && accuracy > 0) {
      this._hereCircle = L.circle([lat, lng], {
        radius: accuracy,
        color: HERE_MARKER_COLOR,
        weight: 1,
        fillColor: HERE_MARKER_COLOR,
        fillOpacity: 0.12,
        interactive: false,
      }).addTo(this._map);
    } else {
      this._hereCircle = null;
    }

    this._map.setView([lat, lng], HERE_ZOOM, { animate: false });
  }

  // A scroll that happens to pass under the cursor must not zoom the map.
  //
  // Leaflet's scrollWheelZoom handler zooms on *any* wheel event, so a page
  // scroll that crossed the map turned into a zoom - and on a map that is
  // most of the screen, that is most scrolls. The embedded-Google-Maps
  // convention is the fix: the wheel zooms only while Ctrl (or Meta, for the
  // Mac) is held, and a plain wheel says so and scrolls the page.
  //
  // Implemented by intercepting rather than by turning the handler off,
  // because enabling it mid-gesture is too late for the event that started
  // the gesture: Leaflet attaches its listener when the handler is enabled,
  // and the wheel event already in flight would never reach it. So the
  // handler stays on and a plain wheel is stopped before it arrives.
  //
  // The listener sits on .map-wrap, in the capture phase, and that placement
  // is load-bearing. Leaflet registers its own listener on the map container
  // itself, and on a shared target the capture/bubble distinction does not
  // decide the order - registration order does. Capturing on the *parent*
  // means this runs first whatever Leaflet did.
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
        // that the page scrolls the way it would anywhere else. Only Leaflet
        // is kept from seeing it.
        e.stopPropagation();
        this.showGestureHint(t("map.ctrlZoomHint"));
      },
      // passive: false explicitly. A wheel listener is one of the types
      // browsers may treat as passive by default, and a passive listener's
      // preventDefault is ignored - silently, apart from a console warning.
      { capture: true, passive: false }
    );

    // The touch half. Dragging is already disabled on a coarse pointer
    // (Stage 13), so a one-finger drag scrolls the page and the map ignores
    // it -- correct, and silent about why. This is the explanation, and it
    // fires on touchmove rather than touchstart so that a tap, which in pick
    // mode places a point, never triggers it.
    if (!isCoarsePointer()) return;
    mapEl.addEventListener(
      "touchmove",
      (e) => {
        if (e.touches.length !== 1) return; // two fingers already work
        this.showGestureHint(t("map.twoFingerHint"));
      },
      { passive: true }
    );
  }

  // Zoom the map for a Ctrl-held wheel.
  //
  // Leaflet's own scroll-wheel handler is switched off and this replaces it,
  // after two rounds of the gesture not working on the reporter's machine
  // while working everywhere it could be measured.
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

    // setZoomAround rather than setZoom: the point under the cursor stays
    // under the cursor, which is what makes wheel zoom feel like a map rather
    // than a slideshow.
    map.setZoomAround(map.mouseEventToContainerPoint(e), map.getZoom() + step);
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
  emitPick(latlng) {
    // Panning the world sideways gives longitudes past +/-180; wrap() folds
    // them back, so a click three worlds to the right still stores a
    // coordinate a database and a map tile server both accept.
    const { lat, lng } = this._map.wrapLatLng(latlng);
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

// Touch-first devices, where a one-finger drag has to belong to the page
// rather than to the map. Guarded because matchMedia is absent in some
// non-browser environments, and a missing match is the desktop answer.
function isCoarsePointer() {
  return window.matchMedia?.("(pointer: coarse)").matches ?? false;
}

function escapeHtml(s) {
  return String(s).replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[c]);
}

function escapeAttr(s) {
  return escapeHtml(s);
}

customElements.define("leaflet-map", LeafletMap);
