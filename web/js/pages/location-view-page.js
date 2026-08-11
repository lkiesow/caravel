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
// links. One Edit button -> the edit route; nothing here is directly
// editable.
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

  container.innerHTML = `
    <div class="page location-view">
      <a href="/trips/${tripId}" data-link class="back-link">${icon("arrow-left")} <span data-i18n="common.back"></span></a>
      <div class="page__header">
        <h1></h1>
        <button class="btn btn-secondary btn-collapse" data-action="edit">${icon("pencil")} <span data-i18n="common.edit"></span></button>
      </div>
      <div class="location-view__meta">
        <span class="dot" style="background:${color}"></span>
        <span class="category-label"></span>
        ${item.type ? `<span class="type-label"></span>` : ""}
      </div>
      ${item.image_url ? `<img class="location-view__image" src="${escapeAttr(item.image_url)}" alt="" />` : ""}
      ${item.notes ? `<div class="location-view__notes"></div>` : ""}

      <div class="editor-card">
        <h4 data-i18n="item.detail.location"></h4>
        ${
          item.location && item.location.lat != null && item.location.lng != null
            ? `
          <p class="location-view__address"></p>
          <leaflet-map lat="${item.location.lat}" lng="${item.location.lng}" marker-title="${escapeAttr(item.title)}"></leaflet-map>
          <a class="location-view__maps-link" href="https://www.google.com/maps/search/?api=1&query=${item.location.lat},${item.location.lng}" target="_blank" rel="noopener" data-i18n="map.viewOnGoogleMaps"></a>
        `
            : `<p data-i18n="item.detail.locationEmpty"></p>`
        }
      </div>

      <div class="editor-card">
        <h4 data-i18n="item.detail.links"></h4>
        <ul class="link-list">
          ${
            item.links.length
              ? item.links.map((l) => `<li><a href="${escapeAttr(l.url)}" target="_blank" rel="noopener">${escapeHtml(l.label || l.url)}</a></li>`).join("")
              : `<li class="empty">${t("item.detail.linksEmpty")}</li>`
          }
        </ul>
      </div>

      <div class="editor-card">
        <h4 data-i18n="item.detail.dates"></h4>
        <ul class="date-list">
          ${
            item.dates.length
              ? item.dates
                  .map((d) => {
                    const range = d.end_date ? `${escapeHtml(d.start_date || "")} – ${escapeHtml(d.end_date)}` : escapeHtml(d.start_date || "");
                    return `<li>${range}${d.label ? " — " + escapeHtml(d.label) : ""}</li>`;
                  })
                  .join("")
              : `<li class="empty">${t("item.detail.datesEmpty")}</li>`
          }
        </ul>
      </div>

      <div class="editor-card">
        <h4 data-i18n="item.detail.documents"></h4>
        <ul class="documents">
          ${
            docs.length
              ? docs
                  .map((d) => `<li><a href="${d.download_url}" target="_blank" rel="noopener">${escapeHtml(d.filename)}</a> <span class="document-size">${formatSize(d.size_bytes)}</span></li>`)
                  .join("")
              : `<li class="empty">${t("documents.empty")}</li>`
          }
        </ul>
      </div>
    </div>
  `;
  translatePage(container);

  container.querySelector("h1").textContent = item.title;
  container.querySelector(".category-label").textContent = t(`item.category.${item.category}`);
  if (item.type) container.querySelector(".type-label").textContent = item.type;
  if (item.notes) container.querySelector(".location-view__notes").innerHTML = item.notes_html;
  if (item.location?.address) container.querySelector(".location-view__address").textContent = item.location.address;

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
