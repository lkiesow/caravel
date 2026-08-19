import { api } from "../api.js";
import { translatePage } from "../i18n.js";
import "../components/trip-card.js";
import { navigate } from "../router.js";
import { icon } from "../icon.js";
import { renderLoading } from "../components/loading.js";

export async function renderTripsPage(container) {
  container.innerHTML = `
    <div class="page trips-page">
      <div class="page__header">
        <h1 data-i18n="trips.title"></h1>
        <button class="btn btn-primary btn-collapse" data-action="new-trip">${icon("plus")} <span data-i18n="trips.new"></span></button>
      </div>
      <p class="trips-empty" data-i18n="trips.empty" hidden></p>
      <div class="trip-grid"></div>
    </div>
  `;
  translatePage(container);

  const grid = container.querySelector(".trip-grid");
  const emptyState = container.querySelector(".trips-empty");

  async function load() {
    renderLoading(grid);
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
    navigate(`/trips/${e.detail.tripId}`);
  });

  container.querySelector('[data-action="new-trip"]').addEventListener("click", () => {
    navigate("/trips/new");
  });

  await load();
}
