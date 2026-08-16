import { api } from "../api.js";
import { t, translatePage } from "../i18n.js";
import { navigate } from "../router.js";
import { icon } from "../icon.js";
import "../components/leaflet-map.js";

const CATEGORY_COLORS = {
  site: "#16a34a",
  stay: "#7c3aed",
  transport: "#2563eb",
};

// Read-only detail view for an item: image, category badge, notes,
// location as address text plus an embedded single-marker map (leaflet-map
// in single-marker mode, driven by lat/lng attributes instead of a
// trip-id, since there's exactly one point to plot here - see
// leaflet-map.js), links, dates as ranges, and documents with download
// links. Nothing here is directly editable; one Edit button at the very
// bottom of the page leads to the edit route.
//
// Every section is conditional: a location with no coordinates, links,
// dates or documents used to render four cards whose entire content was
// "No location set." / "No links yet." / and so on, which is most of a
// screen of scaffolding saying nothing. Only what exists is shown, so a
// sparse location is a short page.
//
// The Edit button sits at the end rather than in .page__header for the
// same reason the trip header's did in Stage 05 (see stage-05.md Section
// 2): a header button next to a wrapping <h1> gets pushed onto its own
// row by a long title, and looks stranded there once it's icon-only on
// mobile. At the bottom it can be full-width and keep its label at every
// width - editing isn't frequent enough to need to be above the fold.
export async function renderLocationViewPage(container, { tripId, itemId }) {
  let item;
  try {
    item = await api.get(`/items/${itemId}`);
  } catch {
    container.innerHTML = `<p>${t("common.notFound")}</p><a href="/trips/${tripId}" data-link>${t("common.back")}</a>`;
    return;
  }

  const color = CATEGORY_COLORS[item.category] || "#71717a";
  const docs = await api.get(`/items/${itemId}/documents`);
  const hasCoords = item.location?.lat != null && item.location?.lng != null;
  const hasAddress = Boolean(item.location?.address);

  container.innerHTML = `
    <div class="page location-view">
      <a href="/trips/${tripId}" data-link class="back-link">${icon("arrow-left")} <span data-i18n="common.back"></span></a>
      <div class="page__header">
        <h1></h1>
      </div>
      <div class="location-view__meta">
        <span class="dot" style="background:${color}"></span>
        <span class="category-label"></span>
        ${item.type ? `<span class="type-label"></span>` : ""}
      </div>
      ${item.image_url ? `<img class="location-view__image" src="${escapeAttr(item.image_url)}" alt="" />` : ""}
      ${item.notes ? `<div class="location-view__notes"></div>` : ""}

      ${
        hasCoords || hasAddress
          ? `
        <div class="editor-card">
          <h4 data-i18n="item.detail.location"></h4>
          ${hasAddress ? `<p class="location-view__address"></p>` : ""}
          ${
            hasCoords
              ? `
            <leaflet-map lat="${item.location.lat}" lng="${item.location.lng}" marker-title="${escapeAttr(item.title)}" marker-category="${escapeAttr(item.category)}"></leaflet-map>
            <a class="location-view__maps-link" href="https://www.google.com/maps/search/?api=1&query=${item.location.lat},${item.location.lng}" target="_blank" rel="noopener" data-i18n="map.viewOnGoogleMaps"></a>
          `
              : ""
          }
        </div>
      `
          : ""
      }

      ${
        item.links.length
          ? `
        <div class="editor-card">
          <h4 data-i18n="item.detail.links"></h4>
          <ul class="link-list">
            ${item.links.map((l) => `<li><a href="${escapeAttr(l.url)}" target="_blank" rel="noopener">${escapeHtml(l.label || l.url)}</a></li>`).join("")}
          </ul>
        </div>
      `
          : ""
      }

      ${
        item.dates.length
          ? `
        <div class="editor-card">
          <h4 data-i18n="item.detail.dates"></h4>
          <ul class="date-list">
            ${item.dates
              .map((d) => {
                const range = d.end_date ? `${escapeHtml(d.start_date || "")} – ${escapeHtml(d.end_date)}` : escapeHtml(d.start_date || "");
                return `<li>${range}${d.label ? " — " + escapeHtml(d.label) : ""}</li>`;
              })
              .join("")}
          </ul>
        </div>
      `
          : ""
      }

      ${
        docs.length
          ? `
        <div class="editor-card">
          <h4 data-i18n="item.detail.documents"></h4>
          <ul class="documents">
            ${docs
              .map((d) => `<li><a href="${d.download_url}" target="_blank" rel="noopener">${escapeHtml(d.filename)}</a> <span class="document-size">${formatSize(d.size_bytes)}</span></li>`)
              .join("")}
          </ul>
        </div>
      `
          : ""
      }

      <button class="btn btn-secondary location-view__edit" data-action="edit">${icon("pencil")} <span data-i18n="location.view.edit"></span></button>
    </div>
  `;
  translatePage(container);

  container.querySelector("h1").textContent = item.title;
  container.querySelector(".category-label").textContent = t(`item.category.${item.category}`);
  if (item.type) container.querySelector(".type-label").textContent = item.type;
  if (item.notes) container.querySelector(".location-view__notes").innerHTML = item.notes_html;
  if (hasAddress) container.querySelector(".location-view__address").textContent = item.location.address;

  container.querySelector('[data-action="edit"]').addEventListener("click", () => {
    navigate(`/trips/${tripId}/locations/${itemId}/edit`);
  });
}

function formatSize(bytes) {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
}

function escapeHtml(s) {
  return String(s).replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[c]);
}

function escapeAttr(s) {
  return escapeHtml(s);
}
