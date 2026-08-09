import { api } from "../api.js";
import { t, translatePage } from "../i18n.js";
import { navigate } from "../router.js";
import { icon } from "../icon.js";
import "../components/leaflet-map.js";
import { renderItemsTab } from "./locations-tab.js";
import { renderItineraryTab } from "./itinerary-tab.js";
import { renderDocumentList } from "../components/document-list.js";

const TABS = ["overview", "locations", "map", "itinerary", "documents"];

export async function renderTripDetailPage(container, { tripId }) {
  let trip;
  try {
    trip = await api.get(`/trips/${tripId}`);
  } catch {
    container.innerHTML = `<p>${t("trips.empty")}</p><a href="/trips" data-link>${t("common.back")}</a>`;
    return;
  }

  // Locations (not Overview) is what users actually want to land on - see
  // Stage 02 review.
  let activeTab = "locations";

  function render() {
    container.innerHTML = `
      <div class="page trip-detail">
        <a href="/trips" data-link class="back-link" data-i18n="common.back"></a>
        <div class="page__header">
          <h1></h1>
          <button data-action="edit-trip" data-i18n-aria-label="trip.editor.editTitle">${icon("pencil")}</button>
        </div>
        <nav class="trip-tabs">
          ${TABS.map((tab) => `<button data-tab="${tab}" data-i18n="trip.tabs.${tab}" class="${tab === activeTab ? "active" : ""}"></button>`).join("")}
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
        activeTab = btn.getAttribute("data-tab");
        render();
      });
    });

    const content = container.querySelector(".trip-tab-content");
    if (activeTab === "overview") {
      renderOverview(content);
    } else if (activeTab === "locations") {
      renderItemsTab(content, trip.id);
    } else if (activeTab === "map") {
      content.innerHTML = `<leaflet-map trip-id="${trip.id}"></leaflet-map>`;
    } else if (activeTab === "itinerary") {
      renderItineraryTab(content, trip);
    } else if (activeTab === "documents") {
      renderDocumentList(content, `/trips/${trip.id}/documents`);
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
        <dd class="trip-overview__notes">${trip.notes ?? "—"}</dd>
      </dl>
      <div class="trip-overview__actions">
        <button data-action="edit" data-i18n="common.edit"></button>
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
