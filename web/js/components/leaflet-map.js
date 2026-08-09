import { api } from "../api.js";
import { t } from "../i18n.js";

const CATEGORY_COLORS = {
  site: "#16a34a",
  stay: "#7c3aed",
  transport: "#2563eb",
};

const styles = `
  :host {
    display: block;
    height: 60vh;
    min-height: 24rem;
  }
  .map-wrap {
    position: relative;
    height: 100%;
  }
  #map {
    height: 100%;
    border-radius: 0.5rem;
  }
  .legend {
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
`;

class LeafletMap extends HTMLElement {
  static get observedAttributes() {
    return ["trip-id"];
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
    const tripId = this.getAttribute("trip-id");
    if (!tripId) return;

    // connectedCallback and attributeChangedCallback both fire for the
    // initial trip-id attribute, so two loads can race; only the most
    // recent one is allowed to touch the DOM once its awaits resolve.
    const generation = (this._generation = (this._generation || 0) + 1);

    const items = await api.get(`/trips/${tripId}/map`);
    if (generation !== this._generation) return;
    this._items = items;
    await this.render(generation);
  }

  async render(generation) {
    this.shadowRoot.innerHTML = `
      <link rel="stylesheet" href="/js/vendor/leaflet/leaflet.css" />
      <style>${styles}</style>
      <div class="map-wrap">
        <div id="map"></div>
        <div class="legend">
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
        </div>
      </div>
    `;

    if (!this._items.length) {
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
    const map = L.map(mapEl, { attributionControl: true });
    this._map = map;

    L.tileLayer("https://{s}.tile.openstreetmap.org/{z}/{x}/{y}.png", {
      maxZoom: 19,
      attribution: "&copy; <a href=\"https://www.openstreetmap.org/copyright\">OpenStreetMap</a> contributors",
    }).addTo(map);

    this.plotMarkers();

    this.shadowRoot.querySelectorAll("[data-category]").forEach((cb) => {
      cb.addEventListener("change", () => {
        const cat = cb.getAttribute("data-category");
        if (cb.checked) this._activeCategories.add(cat);
        else this._activeCategories.delete(cat);
        this.plotMarkers();
      });
    });
  }

  plotMarkers() {
    const L = this._L;
    this._markers.forEach((m) => m.remove());
    this._markers = [];

    const visible = this._items.filter((item) => this._activeCategories.has(item.category));

    for (const item of visible) {
      const icon = L.divIcon({
        className: "",
        html: `<span style="display:block;width:1rem;height:1rem;border-radius:50%;background:${CATEGORY_COLORS[item.category]};border:2px solid white;box-shadow:0 0 2px rgba(0,0,0,.5)"></span>`,
        iconSize: [16, 16],
      });
      const marker = L.marker([item.lat, item.lng], { icon }).addTo(this._map);
      marker.bindPopup(
        `<strong>${escapeHtml(item.title)}</strong><br/><a href="${escapeAttr(item.google_maps_url)}" target="_blank" rel="noopener">${t("map.viewOnGoogleMaps")}</a>`
      );
      this._markers.push(marker);
    }

    if (visible.length) {
      const bounds = L.latLngBounds(visible.map((i) => [i.lat, i.lng]));
      this._map.fitBounds(bounds, { padding: [32, 32] });
    } else {
      this._map.setView([20, 0], 2);
    }
  }
}

function escapeHtml(s) {
  return String(s).replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[c]);
}

function escapeAttr(s) {
  return escapeHtml(s);
}

customElements.define("leaflet-map", LeafletMap);
