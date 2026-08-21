import { api } from "../api.js";
import { t } from "../i18n.js";

const CATEGORY_COLORS = {
  site: "#16a34a",
  stay: "#7c3aed",
  transport: "#2563eb",
};

// Zoom used when the view can't be derived from spread-out markers - a
// single marker, or a set of markers that all sit in the same place. Close
// enough to read street names, far enough that OSM definitely has tiles
// (the layer's maxZoom of 19 is well past what OSM renders in most places).
const SINGLE_MARKER_ZOOM = 14;

// Category colour for a marker, for items whose category is unknown or not
// one of the three the app defines (the single-marker mode gets it from an
// attribute, so it can legitimately be absent).
const FALLBACK_MARKER_COLOR = "#71717a";

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

const styles = `
  /* A column flex box, not a plain block: the map fills the height and the
     two-finger hint below it takes its own, so adding that line can't push
     the map past :host's height at any width. */
  :host {
    display: flex;
    flex-direction: column;
    height: 60vh;
    min-height: 24rem;
  }
  :host([lat]) {
    height: 16rem;
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
  #map {
    height: 100%;
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
  /* Rendered only on coarse pointers (see render()), where one-finger drag
     deliberately no longer pans the map. */
  .gesture-hint {
    margin: 0.5rem 0 0;
    font-size: 0.8rem;
    color: var(--color-text-muted, #666);
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
    #map {
      height: min(50vh, 20rem);
      min-height: 16rem;
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
    return ["trip-id", "lat", "lng", "marker-title", "marker-category"];
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

    // connectedCallback and attributeChangedCallback both fire for the
    // initial attributes, so two loads can race; only the most recent one
    // is allowed to touch the DOM once its awaits resolve.
    const generation = (this._generation = (this._generation || 0) + 1);

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
    const single = this._singleMarker;
    this.shadowRoot.innerHTML = `
      <link rel="stylesheet" href="/js/vendor/leaflet/leaflet.css" />
      <style>${styles}</style>
      <div class="map-wrap">
        <div id="map"></div>
        ${
          single
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
      </div>
      ${isCoarsePointer() ? `<p class="gesture-hint">${t("map.twoFingerHint")}</p>` : ""}
    `;

    if (!single && !this._items.length) {
      this.shadowRoot.querySelector(".map-wrap").insertAdjacentHTML(
        "beforeend",
        `<p class="empty" style="position:absolute;inset:0;margin:0;">${t("map.empty")}</p>`
      );
    }

    // Lazy-load Leaflet only when the map is actually shown, keeping it out
    // of the initial page weight for users who never open the Map tab.
    const L = await import("../vendor/leaflet/leaflet.esm.js");
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
    const map = L.map(mapEl, { attributionControl: true, dragging: !isCoarsePointer() });
    this._map = map;

    L.tileLayer("https://{s}.tile.openstreetmap.org/{z}/{x}/{y}.png", {
      maxZoom: 19,
      attribution: "&copy; <a href=\"https://www.openstreetmap.org/copyright\">OpenStreetMap</a> contributors",
    }).addTo(map);

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

    if (!single) {
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
      // the bounds are then zero-size and fitBounds would zoom all the way
      // to the tile layer's maxZoom of 19, where OSM serves no tiles at all
      // - a grey rectangle with one dot on it. 14 is the same zoom the
      // single-marker branch above picks deliberately.
      this._map.fitBounds(bounds, { padding: [32, 32], maxZoom: SINGLE_MARKER_ZOOM });
    } else {
      this._map.setView([20, 0], 2);
    }
  }
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
