import { api } from "../api.js";
import { t, translatePage } from "../i18n.js";
import { navigate } from "../router.js";
import { icon } from "../icon.js";
import { renderFilterMenu } from "../components/filter-menu.js";
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
// every filter is applied in memory, so typing gives instant feedback and
// switching filters no longer refetches. The backend's ?category= filter still
// exists, just unused here. This is fine at realistic per-trip location counts.
// (This comment used to say a `q` predicate and pagination were a todo.md item.
// They are not: the 2026-08-29 backlog review dropped that entry deliberately,
// and todo.md forbids reconstructing one without asking.)
//
// As of Stage 26 Milestone 4 the filters live behind one trigger rather than
// one button each - see components/filter-menu.js for why that shape.
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
      if (item.dates?.length) card.setAttribute("dates", JSON.stringify(item.dates));
      if (item.image_url) card.setAttribute("image-url", item.image_url);
      list.appendChild(card);
    }
  }

  // The distance filter needs somewhere to say what went wrong, since it is the
  // one filter that can fail: it needs the device's position, and asking for
  // that can be refused or simply never answered.
  const distanceStatus = container.querySelector(".locations-distance-status");
  const distanceNote = container.querySelector(".locations-distance-note");

  function setStatus(key) {
    distanceStatus.textContent = key ? t(key) : "";
    distanceStatus.hidden = !key;
  }

  // One trigger, two groups. Distance is omitted entirely - rather than
  // rendered and disabled - where the device's position cannot be had at all:
  // no geolocation API, or a page served over plain HTTP, where the browser
  // silently never answers (see geolocation.js). A filter that can only ever
  // fail is worse than no filter. That used to be "render the whole menu or
  // not"; now it is one absent row.
  const filterMenu = renderFilterMenu(container.querySelector(".locations-filter-slot"), {
    ariaLabel: "locations.filter.label",
    title: "locations.filter.title",
    groups: [
      {
        key: "category",
        name: t("locations.filter.category"),
        // "All categories", not the bare "All" the option inside the panel
        // uses. On its own button the funnel icon and the aria-label said what
        // was being filtered; as one row among several it has to say so itself.
        neutralLabel: t("locations.filter.allCategories"),
        neutralValue: "all",
        activeValue: "all",
        items: [
          { value: "all", label: t("locations.filter.all") },
          ...CATEGORIES.map((c) => ({ value: c, label: t(`item.category.${c}`) })),
        ],
        onSelect: (value) => {
          activeFilter = value;
          applyFilters();
        },
      },
      {
        key: "distance",
        available: canLocate(),
        name: t("locations.filter.distance"),
        neutralLabel: t("locations.distance.any"),
        neutralValue: ANY_DISTANCE,
        activeValue: ANY_DISTANCE,
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
              // row goes back to "any distance" rather than showing a radius
              // that is not being applied.
              radiusKm = null;
              filterMenu.setActive("distance", ANY_DISTANCE);
              setStatus(locateErrorKey(err.reason || "unavailable"));
              applyFilters();
              return;
            }
          }
          setStatus(null);
          applyFilters();
        },
      },
    ],
  });

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
