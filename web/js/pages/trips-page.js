import { api } from "../api.js";
import { translatePage, t, getLocale } from "../i18n.js";
import "../components/trip-card.js";
import { navigate } from "../router.js";
import { icon } from "../icon.js";
import { renderMenu } from "../components/menu.js";
import { renderLoading } from "../components/loading.js";

// The trips list, with a search box and a sort menu (Stage 15 Milestone 2).
// Before that it was a bare loop over GET /trips in the server's fixed
// created_at DESC, which was fine for the two trips a new instance has and
// nothing else - and since Stage 14 other people's trips arrive in the same
// list, so there is more in it than you put there.
//
// Filtering and sorting happen **in memory**, over a list fetched once. Same
// decision as locations-tab.js, for the same reason: the API returns every trip
// unconditionally, so typing gives instant feedback and changing the sort costs
// no round trip. A `q`/`sort` pair on ListTripsForUser is the version that
// matters once somebody has hundreds of trips, and is a todo.md entry rather
// than something to build blind.
//
// The toolbar is the same one-non-wrapping-row shape the locations tab uses, on
// the shared .list-toolbar/.list-search rules - which is why "New trip" moved
// out of the page header and into the row.
const SORTS = ["newest", "title", "start"];
const DEFAULT_SORT = "newest";

export async function renderTripsPage(container) {
  let query = "";
  let sort = DEFAULT_SORT;
  let allTrips = [];

  container.innerHTML = `
    <div class="page trips-page">
      <div class="page__header">
        <h1 data-i18n="trips.title"></h1>
      </div>
      <div class="list-toolbar">
        <div class="list-search">
          ${icon("search", { className: "list-search__icon" })}
          <input type="search" name="q" autocomplete="off" data-i18n-placeholder="trips.searchPlaceholder" data-i18n-aria-label="trips.searchPlaceholder" />
        </div>
        <div class="trips-sort-slot"></div>
        <button class="btn btn-primary btn-collapse" data-action="new-trip">${icon("plus")} <span data-i18n="trips.new"></span></button>
      </div>
      <p class="trips-empty" data-i18n="trips.empty" hidden></p>
      <p class="trips-empty trips-empty--no-matches" data-i18n="trips.noMatches" hidden></p>
      <div class="trip-grid"></div>
    </div>
  `;
  translatePage(container);

  const grid = container.querySelector(".trip-grid");
  const emptyState = container.querySelector(".trips-empty:not(.trips-empty--no-matches)");
  const noMatchesState = container.querySelector(".trips-empty--no-matches");

  renderMenu(container.querySelector(".trips-sort-slot"), {
    iconName: "arrow-down-up",
    ariaLabel: "trips.sort.label",
    activeValue: DEFAULT_SORT,
    // Sorting by anything other than the default tints the trigger, so a
    // collapsed icon-only button on a phone still says the order is not the
    // one the list normally has.
    neutralValue: DEFAULT_SORT,
    items: SORTS.map((value) => ({ value, label: t(`trips.sort.${value}`) })),
    onSelect: (value) => {
      sort = value;
      apply();
    },
  });

  function matches(trip) {
    if (!query) return true;
    return `${trip.title} ${trip.subtitle ?? ""}`.toLowerCase().includes(query);
  }

  // Sorting a copy, never allTrips: the fetched order *is* the "newest" answer,
  // and sorting in place would destroy it the first time another order was
  // picked.
  function sorted(trips) {
    const out = [...trips];
    if (sort === "title") {
      // Under the active locale, so German umlauts sort where a German reader
      // expects rather than after z.
      const collator = new Intl.Collator(getLocale(), { sensitivity: "base", numeric: true });
      out.sort((a, b) => collator.compare(a.title, b.title));
    } else if (sort === "start") {
      // Earliest first, and a trip with no start date goes last rather than
      // first: it is unscheduled, not imminent. ISO dates compare as strings.
      out.sort((a, b) => {
        if (!a.start_date && !b.start_date) return 0;
        if (!a.start_date) return 1;
        if (!b.start_date) return -1;
        return a.start_date < b.start_date ? -1 : a.start_date > b.start_date ? 1 : 0;
      });
    }
    return out;
  }

  function apply() {
    const visible = sorted(allTrips.filter(matches));

    // Two empty states, as the locations tab has: an account with no trips at
    // all reads differently from a search that matched none of them.
    emptyState.hidden = allTrips.length > 0;
    noMatchesState.hidden = allTrips.length === 0 || visible.length > 0;

    grid.innerHTML = "";
    for (const trip of visible) {
      const card = document.createElement("trip-card");
      card.setAttribute("trip-id", trip.id);
      card.setAttribute("title", trip.title);
      if (trip.start_date) card.setAttribute("start-date", trip.start_date);
      if (trip.end_date) card.setAttribute("end-date", trip.end_date);
      if (trip.preview_image_url) card.setAttribute("image-url", trip.preview_image_url);
      // trip.owner is present only for trips the user doesn't own, so its
      // presence is the test — no need to compare roles here.
      if (trip.owner) {
        card.setAttribute("shared-label", t("trips.sharedBy", { name: trip.owner.display_name }));
      }
      grid.appendChild(card);
    }
  }

  container.querySelector('input[name="q"]').addEventListener("input", (e) => {
    query = e.target.value.trim().toLowerCase();
    apply();
  });

  grid.addEventListener("trip-open", (e) => {
    navigate(`/trips/${e.detail.tripId}`);
  });

  container.querySelector('[data-action="new-trip"]').addEventListener("click", () => {
    navigate("/trips/new");
  });

  renderLoading(grid);
  allTrips = await api.get("/trips");
  apply();
}
