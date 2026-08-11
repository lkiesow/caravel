import { api } from "../api.js";
import { t, translatePage } from "../i18n.js";
import { navigate } from "../router.js";
import { icon } from "../icon.js";
import "../components/location-card.js";

const CATEGORIES = ["site", "stay", "transport"];

export async function renderItemsTab(container, tripId) {
  let activeFilter = "all";

  container.innerHTML = `
    <div class="items-tab">
      <div class="items-tab__header">
        <div class="items-filter">
          <button data-filter="all" class="active" data-i18n="locations.filter.all"></button>
          ${CATEGORIES.map((c) => `<button data-filter="${c}">${t(`item.category.${c}`)}</button>`).join("")}
        </div>
        <button class="btn btn-primary btn-collapse" data-action="new-item">${icon("plus")} <span data-i18n="locations.new"></span></button>
      </div>
      <p class="items-empty" data-i18n="locations.empty" hidden></p>
      <div class="item-list"></div>
    </div>
  `;
  translatePage(container);

  const list = container.querySelector(".item-list");
  const emptyState = container.querySelector(".items-empty");

  async function load() {
    const query = activeFilter === "all" ? "" : `?category=${activeFilter}`;
    const items = await api.get(`/trips/${tripId}/items${query}`);
    list.innerHTML = "";
    emptyState.hidden = items.length > 0;
    for (const item of items) {
      const card = document.createElement("item-card");
      card.setAttribute("item-id", item.id);
      card.setAttribute("title", item.title);
      card.setAttribute("category", item.category);
      if (item.type) card.setAttribute("type", item.type);
      if (item.image_url) card.setAttribute("image-url", item.image_url);
      list.appendChild(card);
    }
  }

  container.querySelectorAll("[data-filter]").forEach((btn) => {
    btn.addEventListener("click", () => {
      activeFilter = btn.getAttribute("data-filter");
      container.querySelectorAll("[data-filter]").forEach((b) => b.classList.toggle("active", b === btn));
      load();
    });
  });

  container.querySelector('[data-action="new-item"]').addEventListener("click", () => {
    navigate(`/trips/${tripId}/locations/new`);
  });

  list.addEventListener("item-open", (e) => {
    navigate(`/trips/${tripId}/locations/${e.detail.itemId}`);
  });

  await load();
}
