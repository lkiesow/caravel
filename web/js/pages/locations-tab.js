import { api } from "../api.js";
import { t, translatePage } from "../i18n.js";
import { navigate } from "../router.js";
import { icon } from "../icon.js";
import { renderMenu } from "../components/menu.js";
import "../components/location-card.js";
import { renderLoading } from "../components/loading.js";

const CATEGORIES = ["site", "stay", "transport"];

// The toolbar is one non-wrapping row - search (flexible) + filter menu +
// "New location" - which is what makes it fit 324px, where the previous
// row of four category pills did not (they only scrolled; see stage-04.md
// Section 5 and stage-06.md Section 1).
//
// Consequence of putting a search box next to the filter: filtering moved
// from the server to the client. The trip's locations are fetched once and
// both the category filter and the search query are applied in memory, so
// typing gives instant feedback and switching filters no longer refetches.
// The backend's ?category= filter still exists, just unused here. This is
// fine at realistic per-trip location counts; a `q` predicate + pagination
// for very large trips is a todo.md item.
export async function renderItemsTab(container, tripId) {
  let activeFilter = "all";
  let query = "";
  let allItems = [];

  container.innerHTML = `
    <div class="items-tab">
      <div class="locations-toolbar">
        <div class="locations-search">
          ${icon("search", { className: "locations-search__icon" })}
          <input type="search" name="q" autocomplete="off" data-i18n-placeholder="locations.searchPlaceholder" data-i18n-aria-label="locations.searchPlaceholder" />
        </div>
        <div class="locations-filter-slot"></div>
        <button class="btn btn-primary btn-collapse" data-action="new-item">${icon("plus")} <span data-i18n="locations.new"></span></button>
      </div>
      <p class="items-empty" data-i18n="locations.empty" hidden></p>
      <p class="items-empty items-empty--no-matches" data-i18n="locations.noMatches" hidden></p>
      <div class="item-list"></div>
    </div>
  `;
  translatePage(container);

  const list = container.querySelector(".item-list");
  const emptyState = container.querySelector(".items-empty:not(.items-empty--no-matches)");
  const noMatchesState = container.querySelector(".items-empty--no-matches");

  renderMenu(container.querySelector(".locations-filter-slot"), {
    iconName: "funnel",
    ariaLabel: "locations.filter.label",
    activeValue: "all",
    neutralValue: "all",
    items: [{ value: "all", label: t("locations.filter.all") }, ...CATEGORIES.map((c) => ({ value: c, label: t(`item.category.${c}`) }))],
    onSelect: (value) => {
      activeFilter = value;
      applyFilters();
    },
  });

  function matches(item) {
    if (activeFilter !== "all" && item.category !== activeFilter) return false;
    if (!query) return true;
    return `${item.title} ${item.type ?? ""}`.toLowerCase().includes(query);
  }

  function applyFilters() {
    const visible = allItems.filter(matches);

    // Two distinct empty states: an untouched trip with no locations at
    // all reads differently from a search that matched none of them.
    emptyState.hidden = allItems.length > 0;
    noMatchesState.hidden = allItems.length === 0 || visible.length > 0;

    list.innerHTML = "";
    for (const item of visible) {
      const card = document.createElement("item-card");
      card.setAttribute("item-id", item.id);
      card.setAttribute("title", item.title);
      card.setAttribute("category", item.category);
      if (item.type) card.setAttribute("type", item.type);
      if (item.image_url) card.setAttribute("image-url", item.image_url);
      list.appendChild(card);
    }
  }

  container.querySelector('input[name="q"]').addEventListener("input", (e) => {
    query = e.target.value.trim().toLowerCase();
    applyFilters();
  });

  container.querySelector('[data-action="new-item"]').addEventListener("click", () => {
    navigate(`/trips/${tripId}/locations/new`);
  });

  list.addEventListener("item-open", (e) => {
    navigate(`/trips/${tripId}/locations/${e.detail.itemId}`);
  });

  renderLoading(list);
  allItems = await api.get(`/trips/${tripId}/items`);
  applyFilters();
}
