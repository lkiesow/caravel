import { api } from "../api.js";
import { t, translatePage } from "../i18n.js";
import { navigate } from "../router.js";
import { icon } from "../icon.js";
import { renderMenu } from "../components/menu.js";
import "../components/location-card.js";
import { renderLoading } from "../components/loading.js";
import { canLocate, distanceKm, getCurrentPosition, locateErrorKey } from "../geolocation.js";
import { canEdit } from "../trip-role.js";

const CATEGORIES = ["site", "stay", "transport"];

// Radii for the "near me" filter, in km. Coarse on purpose: the question is
// "walkable / a short drive / same region", not a precise number.
const DISTANCE_RADII_KM = [1, 2, 5, 10, 25];
const ANY_DISTANCE = "any";

// The toolbar is one non-wrapping row - search (flexible) + filter menu +
// "New location" - which is what makes it fit 324px, where the previous
// row of four category pills did not (they only scrolled; see stage-04.md
// Section 5 and stage-06.md Section 1). Its .list-toolbar/.list-search rules
// are shared with the trips list as of Stage 15 Milestone 2.
//
// Consequence of putting a search box next to the filter: filtering moved
// from the server to the client. The trip's locations are fetched once and
// both the category filter and the search query are applied in memory, so
// typing gives instant feedback and switching filters no longer refetches.
// The backend's ?category= filter still exists, just unused here. This is
// fine at realistic per-trip location counts; a `q` predicate + pagination
// for very large trips is a todo.md item.
// Takes the whole trip rather than just its id since Stage 14 Milestone 4: the
// toolbar has to know whether the reader may add a location, and `trip.role` is
// where that lives.
export async function renderItemsTab(container, trip) {
  const tripId = trip.id;
  const editable = canEdit(trip);
  let activeFilter = "all";
  let query = "";
  let allItems = [];
  let radiusKm = null;
  // Kept between selections so switching 5km -> 10km does not ask the device
  // again. Not fetched on load: asking for someone's position before they
  // have expressed any interest in it is rude, and the permission prompt
  // would arrive unexplained.
  let devicePosition = null;

  container.innerHTML = `
    <div class="items-tab">
      <div class="list-toolbar">
        <div class="list-search">
          ${icon("search", { className: "list-search__icon" })}
          <input type="search" name="q" autocomplete="off" data-i18n-placeholder="locations.searchPlaceholder" data-i18n-aria-label="locations.searchPlaceholder" />
        </div>
        <div class="locations-filter-slot"></div>
        <div class="locations-distance-slot"></div>
        ${editable ? `<button class="btn btn-primary btn-collapse" data-action="new-item">${icon("plus")} <span data-i18n="locations.new"></span></button>` : ""}
      </div>
      <p class="locations-distance-status" role="status" hidden></p>
      <p class="locations-distance-note" hidden></p>
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

  function hasCoordinates(item) {
    return typeof item.lat === "number" && typeof item.lng === "number";
  }

  function matches(item) {
    if (activeFilter !== "all" && item.category !== activeFilter) return false;
    // A location with no coordinates is not far away, it is unmeasurable -
    // so a radius never hides one. Hiding them would make a gap in the data
    // look like a distance result, and the note below says they are there.
    if (radiusKm && devicePosition && hasCoordinates(item)) {
      if (distanceKm(devicePosition, { lat: item.lat, lng: item.lng }) > radiusKm) return false;
    }
    if (!query) return true;
    return `${item.title} ${item.type ?? ""}`.toLowerCase().includes(query);
  }

  function applyFilters() {
    const visible = allItems.filter(matches);

    // Only while a radius is active, and only if there is actually something
    // it could not measure - otherwise it is a warning about nothing.
    const unplaced = radiusKm && devicePosition ? visible.filter((item) => !hasCoordinates(item)).length : 0;
    distanceNote.textContent = unplaced ? t("locations.distance.unplaced", { count: unplaced }, unplaced) : "";
    distanceNote.hidden = !unplaced;

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
      if (item.tags?.length) card.setAttribute("tags", JSON.stringify(item.tags));
      if (item.image_url) card.setAttribute("image-url", item.image_url);
      list.appendChild(card);
    }
  }

  // The distance filter. Icon-only like the category funnel beside it: the
  // toolbar is one deliberately non-wrapping row that has to fit 324px (see
  // the note at the top of this file), and a third labelled button does not.
  //
  // Hidden entirely where the device's position cannot be had at all - no
  // geolocation API, or a page served over plain HTTP, where the browser
  // silently never answers (see geolocation.js). A filter that can only ever
  // fail is worse than no filter.
  const distanceStatus = container.querySelector(".locations-distance-status");
  const distanceNote = container.querySelector(".locations-distance-note");

  function setStatus(key) {
    distanceStatus.textContent = key ? t(key) : "";
    distanceStatus.hidden = !key;
  }

  if (canLocate()) {
    const distanceMenu = renderMenu(container.querySelector(".locations-distance-slot"), {
      iconName: "locate-fixed",
      ariaLabel: "locations.distance.label",
      activeValue: ANY_DISTANCE,
      neutralValue: ANY_DISTANCE,
      items: [
        { value: ANY_DISTANCE, label: t("locations.distance.any") },
        ...DISTANCE_RADII_KM.map((km) => ({ value: String(km), label: t("locations.distance.within", { km }) })),
      ],
      onSelect: async (value) => {
        if (value === ANY_DISTANCE) {
          radiusKm = null;
          setStatus(null);
          applyFilters();
          return;
        }
        radiusKm = Number(value);
        if (!devicePosition) {
          setStatus("map.locate.searching");
          try {
            devicePosition = await getCurrentPosition();
          } catch (err) {
            // The filter cannot be honored, so it must not look active: the
            // menu goes back to "any distance" rather than showing a radius
            // that is not being applied.
            radiusKm = null;
            distanceMenu.setActive(ANY_DISTANCE);
            setStatus(locateErrorKey(err.reason || "unavailable"));
            applyFilters();
            return;
          }
        }
        setStatus(null);
        applyFilters();
      },
    });
  }

  container.querySelector('input[name="q"]').addEventListener("input", (e) => {
    query = e.target.value.trim().toLowerCase();
    applyFilters();
  });

  container.querySelector('[data-action="new-item"]')?.addEventListener("click", () => {
    navigate(`/trips/${tripId}/locations/new`);
  });

  list.addEventListener("item-open", (e) => {
    navigate(`/trips/${tripId}/locations/${e.detail.itemId}`);
  });

  renderLoading(list);
  allItems = await api.get(`/trips/${tripId}/items`);
  applyFilters();
}
