import { api } from "../api.js";
import { t, translatePage } from "../i18n.js";
import { navigate } from "../router.js";
import { icon } from "../icon.js";
import "../components/leaflet-map.js";
import { renderLoading } from "../components/loading.js";
import { canEdit, isShared } from "../trip-role.js";
import { renderFileList } from "../components/file-list.js";
import { formatDateRange } from "../format.js";
import { googleMapsUrl, openStreetMapUrl, safeHref } from "../url.js";

// A URL reduced to its host, for a credit that has a source page but no named
// author.
function hostOf(raw) {
  try {
    return new URL(raw).host;
  } catch {
    return raw;
  }
}
import { renderNotFoundPage } from "./not-found-page.js";

const CATEGORY_COLORS = {
  site: "#16a34a",
  stay: "#7c3aed",
  transport: "#2563eb",
};

// Read-only detail view for an item: image, category badge, notes,
// location as address text plus an embedded single-marker map (leaflet-map
// in single-marker mode, driven by lat/lng attributes instead of a
// trip-id, since there's exactly one point to plot here - see
// leaflet-map.js), links, the days it is on in the itinerary as ranges
// (Stage 25 - these are derived from the itinerary, not stored on the
// location), and files with download
// links. Nothing here is directly editable; one Edit button at the very
// bottom of the page leads to the edit route.
//
// Every section is conditional: a location with no coordinates, links,
// dates or files used to render four cards whose entire content was
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
  renderLoading(container);

  // The trip comes along for its `role` — this page has no other use for it,
  // but the Edit button has to know whether editing is possible, and the role
  // lives on the trip rather than on the item. In parallel with the item so it
  // costs latency rather than a second round trip.
  let item, trip;
  try {
    [item, trip] = await Promise.all([api.get(`/items/${itemId}`), api.get(`/trips/${tripId}`)]);
  } catch {
    renderNotFoundPage(container, { href: `/trips/${tripId}`, labelKey: "common.back" });
    return;
  }

  const color = CATEGORY_COLORS[item.category] || "#71717a";
  const files = await api.get(`/items/${itemId}/files`);
  const editable = canEdit(trip);
  const hasCoords = item.location?.lat != null && item.location?.lng != null;
  const hasAddress = Boolean(item.location?.address);
  // null unless this place was saved through the address search, which is the
  // only route that knows an OSM element. Absent, not broken, for a dropped pin.
  const osmUrl = openStreetMapUrl(item.location?.osm_type, item.location?.osm_id);

  container.innerHTML = `
    <div class="page location-view">
      <a href="/trips/${tripId}" data-link class="back-link">${icon("arrow-left")} <span data-i18n="common.back"></span></a>
      <div class="page__header">
        <h1></h1>
      </div>
      <div class="location-view__meta">
        <span class="dot" style="background:${color}"></span>
        <span class="category-label"></span>
        ${item.tags?.length ? `<ul class="tag-list location-view__tags"></ul>` : ""}
      </div>
      ${
        item.image_url
          ? `
        <figure class="location-view__cover">
          <img class="location-view__image" src="${escapeAttr(item.image_url)}" alt="" />
          ${item.image_credit ? `<figcaption class="image-credit"></figcaption>` : ""}
        </figure>
      `
          : ""
      }
      ${item.notes ? `<div class="location-view__notes"></div>` : ""}

      ${
        hasCoords || hasAddress
          ? `
        <div class="editor-card">
          <h2 data-i18n="item.detail.location"></h2>
          ${hasAddress ? `<p class="location-view__address"></p>` : ""}
          ${
            hasCoords
              ? `
            <leaflet-map lat="${item.location.lat}" lng="${item.location.lng}" marker-title="${escapeAttr(item.title)}" marker-address="${escapeAttr(item.location.address ?? "")}" marker-category="${escapeAttr(item.category)}"></leaflet-map>
            <a class="location-view__maps-link" href="${escapeAttr(googleMapsUrl(item.location.lat, item.location.lng, item.title, item.location.address))}" target="_blank" rel="noopener" data-i18n="map.viewOnGoogleMaps"></a>
            ${
              osmUrl
                ? `<a class="location-view__maps-link" href="${escapeAttr(osmUrl)}" target="_blank" rel="noopener" data-i18n="map.viewOnOpenStreetMap"></a>`
                : ""
            }
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
          <h2 data-i18n="item.detail.links"></h2>
          <ul class="link-list">
            ${item.links.map(renderLink).join("")}
          </ul>
        </div>
      `
          : ""
      }

      ${
        item.dates.length
          ? `
        <div class="editor-card">
          <h2 data-i18n="item.detail.dates"></h2>
          <ul class="date-list">
            ${item.dates
              .map((d) => `<li>${escapeHtml(formatDateRange(d.start_date, d.end_date) ?? "")}</li>`)
              .join("")}
          </ul>
        </div>
      `
          : ""
      }

      ${
        files.length
          ? `
        <div class="editor-card">
          <h2 data-i18n="item.detail.files"></h2>
          <div class="file-list-slot"></div>
        </div>
      `
          : ""
      }

      ${editable ? `<button class="btn btn-secondary location-view__edit" data-action="edit">${icon("pencil")} <span data-i18n="location.view.edit"></span></button>` : ""}
    </div>
  `;
  translatePage(container);

  // The same card the editor and the trip tab render, in read-only mode: no
  // add row and no per-row menu, since everything editable on this page lives
  // behind the Edit button at the bottom. The rows are handed over rather than
  // refetched - they were already needed above to decide whether this card
  // exists at all.
  if (files.length) {
    // readOnly, so no controls either way — but `shared` is still passed so a
    // personal file is *marked* as one here too. Seeing a lock on a file you
    // uploaded is the confirmation that it is not on show to the trip.
    renderFileList(container.querySelector(".file-list-slot"), `/items/${itemId}/files`, {
      rows: files,
      readOnly: true,
      shared: isShared(trip),
    });
  }

  container.querySelector("h1").textContent = item.title;
  container.querySelector(".category-label").textContent = t(`item.category.${item.category}`);
  // textContent per chip rather than interpolation: a tag is whatever somebody
  // typed, the same reason the title above is set this way.
  if (item.tags?.length) {
    container.querySelector(".location-view__tags").replaceChildren(
      ...item.tags.map((tag) => {
        const li = document.createElement("li");
        li.className = "tag-chip";
        li.textContent = tag;
        return li;
      })
    );
  }

  // The credit, where the cover is actually shown at size.
  //
  // Built with DOM calls: every value here came off a third party's page, and
  // storing it was the whole point of migration 0002 -- a freely licensed
  // photograph is not an unencumbered one, and Caravel is already multi-user
  // with public share links on the backlog.
  //
  // Deliberately only here, and not on the thumbnails in the locations list or
  // the itinerary: a credit line under a 60px thumbnail is unreadable, and
  // those thumbnails link to this page, which carries it.
  const creditEl = container.querySelector(".image-credit");
  if (creditEl && item.image_credit) {
    const { text, license, source_url: sourceURL } = item.image_credit;
    const link = document.createElement("a");
    link.href = sourceURL;
    link.target = "_blank";
    link.rel = "noopener noreferrer";
    // The author when it is known, the source otherwise -- "from this page" is
    // a real attribution, and an image with a source and no named author is a
    // normal thing for Wikimedia to return.
    link.textContent = text || hostOf(sourceURL);
    creditEl.append(document.createTextNode(t("image.creditPrefix") + " "), link);
    if (license) {
      const lic = document.createElement("span");
      lic.className = "image-credit__license";
      lic.textContent = license;
      creditEl.append(document.createTextNode(" · "), lic);
    }
  }
  if (item.notes) container.querySelector(".location-view__notes").innerHTML = item.notes_html;
  if (hasAddress) container.querySelector(".location-view__address").textContent = item.location.address;

  container.querySelector('[data-action="edit"]')?.addEventListener("click", () => {
    navigate(`/trips/${tripId}/locations/${itemId}/edit`);
  });
}

// One stored link. Rendered as an anchor only when its URL is one a browser
// may safely follow -- see web/js/url.js. Anything else is shown as text, so
// a link written before the server refused non-http schemes is still visible
// and no longer clickable.
function renderLink(l) {
  const href = safeHref(l.url);
  const text = escapeHtml(l.label || l.url);
  if (!href) return `<li><span class="link-list__unsafe">${text}</span></li>`;
  return `<li><a href="${escapeAttr(href)}" target="_blank" rel="noopener">${text}</a></li>`;
}

function escapeHtml(s) {
  return String(s).replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[c]);
}

function escapeAttr(s) {
  return escapeHtml(s);
}
