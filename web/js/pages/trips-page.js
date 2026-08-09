import { api } from "../api.js";
import { t, translatePage } from "../i18n.js";
import "../components/trip-card.js";
import { renderTripForm } from "../components/trip-form.js";

export async function renderTripsPage(container) {
  container.innerHTML = `
    <div class="page trips-page">
      <div class="page__header">
        <h1 data-i18n="trips.title"></h1>
        <button data-action="new-trip" data-i18n="trips.new"></button>
      </div>
      <div class="trip-form-slot"></div>
      <p class="trips-empty" data-i18n="trips.empty" hidden></p>
      <div class="trip-grid"></div>
    </div>
  `;
  translatePage(container);

  const grid = container.querySelector(".trip-grid");
  const emptyState = container.querySelector(".trips-empty");
  const formSlot = container.querySelector(".trip-form-slot");

  async function load() {
    const trips = await api.get("/trips");
    grid.innerHTML = "";
    emptyState.hidden = trips.length > 0;
    for (const trip of trips) {
      const card = document.createElement("trip-card");
      card.setAttribute("trip-id", trip.id);
      card.setAttribute("title", trip.title);
      if (trip.start_date) card.setAttribute("start-date", trip.start_date);
      if (trip.end_date) card.setAttribute("end-date", trip.end_date);
      if (trip.preview_image_url) card.setAttribute("image-url", trip.preview_image_url);
      grid.appendChild(card);
    }
  }

  grid.addEventListener("trip-open", (e) => {
    window.history.pushState({}, "", `/trips/${e.detail.tripId}`);
    window.dispatchEvent(new PopStateEvent("popstate"));
  });

  container.querySelector('[data-action="new-trip"]').addEventListener("click", () => {
    renderTripForm(formSlot, null, {
      onSaved: async () => {
        formSlot.innerHTML = "";
        await load();
      },
      onCancel: () => {
        formSlot.innerHTML = "";
      },
    });
  });

  await load();
}
