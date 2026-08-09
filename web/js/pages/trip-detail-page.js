import { api } from "../api.js";
import { t, translatePage } from "../i18n.js";
import { renderTripForm } from "../components/trip-form.js";
import { renderImageField } from "../components/image-field.js";
import "../components/leaflet-map.js";
import { renderItemsTab } from "./items-tab.js";
import { renderItineraryTab } from "./itinerary-tab.js";
import { renderDocumentList } from "../components/document-list.js";

const TABS = ["overview", "items", "map", "itinerary", "documents"];

export async function renderTripDetailPage(container, { tripId }) {
  let trip;
  try {
    trip = await api.get(`/trips/${tripId}`);
  } catch {
    container.innerHTML = `<p>${t("trips.empty")}</p><a href="/trips" data-link>${t("common.back")}</a>`;
    return;
  }

  let activeTab = "overview";

  function render() {
    container.innerHTML = `
      <div class="page trip-detail">
        <a href="/trips" data-link class="back-link" data-i18n="common.back"></a>
        <div class="page__header">
          <h1></h1>
        </div>
        <nav class="trip-tabs">
          ${TABS.map((tab) => `<button data-tab="${tab}" data-i18n="trip.tabs.${tab}" class="${tab === activeTab ? "active" : ""}"></button>`).join("")}
        </nav>
        <div class="trip-tab-content"></div>
      </div>
    `;
    translatePage(container);
    container.querySelector(".page__header h1").textContent = trip.title;

    container.querySelectorAll("[data-tab]").forEach((btn) => {
      btn.addEventListener("click", () => {
        activeTab = btn.getAttribute("data-tab");
        render();
      });
    });

    const content = container.querySelector(".trip-tab-content");
    if (activeTab === "overview") {
      renderOverview(content);
    } else if (activeTab === "items") {
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
      <h4 data-i18n="trip.overview.image"></h4>
      <div class="image-field-slot"></div>
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
        <button data-action="delete" data-i18n="common.delete"></button>
      </div>
      <div class="trip-form-slot"></div>
    `;
    translatePage(content);

    renderImageField(content.querySelector(".image-field-slot"), {
      tripId: trip.id,
      imageUrl: trip.preview_image_url,
      attachPath: `/trips/${trip.id}/preview-image`,
      onChanged: (updated) => {
        trip = updated;
      },
    });

    const formSlot = content.querySelector(".trip-form-slot");
    content.querySelector('[data-action="edit"]').addEventListener("click", () => {
      renderTripForm(formSlot, trip, {
        onSaved: (updated) => {
          trip = updated;
          render();
        },
        onCancel: () => {
          formSlot.innerHTML = "";
        },
      });
    });

    content.querySelector('[data-action="delete"]').addEventListener("click", async () => {
      if (!window.confirm(t("trip.deleteConfirm"))) return;
      await api.delete(`/trips/${trip.id}`);
      window.history.pushState({}, "", "/trips");
      window.dispatchEvent(new PopStateEvent("popstate"));
    });
  }

  render();
}
