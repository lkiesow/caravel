import { api } from "../api.js";
import { t, translatePage } from "../i18n.js";
import { navigate } from "../router.js";
import { icon } from "../icon.js";
import "../components/leaflet-map.js";
import { renderItemsTab } from "./locations-tab.js";
import { renderItineraryTab } from "./itinerary-tab.js";
import { renderDocumentList } from "../components/document-list.js";
import { renderChecklistList } from "../components/checklist-list.js";

const TABS = ["overview", "locations", "map", "itinerary", "documents", "checklists"];

export async function renderTripDetailPage(container, { tripId, tab }) {
  let trip;
  try {
    trip = await api.get(`/trips/${tripId}`);
  } catch {
    container.innerHTML = `<p>${t("common.notFound")}</p><a href="/trips" data-link>${t("common.back")}</a>`;
    return;
  }

  // Locations (not Overview) is what users actually want to land on - see
  // Stage 02 review. "/trips/:tripId" has no tab segment, so it
  // canonicalizes itself to the real tab URL rather than leaving a
  // tab-less URL in the address bar and history.
  if (!tab || !TABS.includes(tab)) {
    tab = "locations";
    window.history.replaceState({}, "", `/trips/${tripId}/locations`);
  }

  function render() {
    container.innerHTML = `
      <div class="page trip-detail">
        <a href="/trips" data-link class="back-link">${icon("arrow-left")} <span data-i18n="common.home"></span></a>
        <div class="page__header">
          <h1></h1>
          <button class="btn btn-secondary btn-collapse" data-action="edit-trip">${icon("pencil")} <span data-i18n="common.edit"></span></button>
        </div>
        <nav class="trip-tabs">
          ${TABS.map((tb) => `<button data-tab="${tb}" data-i18n="trip.tabs.${tb}" class="${tb === tab ? "active" : ""}"></button>`).join("")}
        </nav>
        <div class="trip-tab-content"></div>
      </div>
    `;
    translatePage(container);
    container.querySelector(".page__header h1").textContent = trip.title;

    container.querySelector('[data-action="edit-trip"]').addEventListener("click", () => {
      navigate(`/trips/${trip.id}/edit`);
    });

    container.querySelectorAll("[data-tab]").forEach((btn) => {
      btn.addEventListener("click", () => {
        tab = btn.getAttribute("data-tab");
        // Pushed directly (not via router.js's navigate) so switching tabs
        // stays a local re-render instead of re-fetching the trip through a
        // full route match - this only updates history/the URL bar. Back/
        // forward still works: browser back dispatches "popstate", which
        // the app-level router does listen for, and it re-renders this
        // page fresh from the URL at that point.
        window.history.pushState({}, "", `/trips/${trip.id}/${tab}`);
        render();
      });
    });

    const content = container.querySelector(".trip-tab-content");
    if (tab === "overview") {
      renderOverview(content);
    } else if (tab === "locations") {
      renderItemsTab(content, trip.id);
    } else if (tab === "map") {
      content.innerHTML = `<leaflet-map trip-id="${trip.id}"></leaflet-map>`;
    } else if (tab === "itinerary") {
      renderItineraryTab(content, trip);
    } else if (tab === "documents") {
      renderDocumentList(content, `/trips/${trip.id}/documents`);
    } else if (tab === "checklists") {
      renderChecklistList(content, trip.id);
    }
  }

  function renderOverview(content) {
    content.innerHTML = `
      ${trip.preview_image_url ? `<img class="trip-overview__image" src="${escapeAttr(trip.preview_image_url)}" alt="" />` : ""}
      <dl class="trip-overview">
        <dt data-i18n="trip.form.startDate"></dt>
        <dd>${trip.start_date ?? "—"}</dd>
        <dt data-i18n="trip.form.endDate"></dt>
        <dd>${trip.end_date ?? "—"}</dd>
        <dt data-i18n="trip.form.notes"></dt>
        <dd class="trip-overview__notes">${trip.notes ? trip.notes_html : "—"}</dd>
      </dl>
      <div class="trip-overview__actions">
        <button class="btn btn-secondary" data-action="edit">${icon("pencil")} <span data-i18n="common.edit"></span></button>
      </div>
    `;
    translatePage(content);

    content.querySelector('[data-action="edit"]').addEventListener("click", () => {
      navigate(`/trips/${trip.id}/edit`);
    });
  }

  render();
}

function escapeAttr(s) {
  return String(s).replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[c]);
}
