import { api } from "../api.js";
import { translatePage, t } from "../i18n.js";
import "../components/trip-card.js";
import { navigate } from "../router.js";
import { icon } from "../icon.js";
import { renderMenu } from "../components/menu.js";
import { renderLoading } from "../components/loading.js";
import { byTitle } from "../sort.js";
import { todayISO } from "../format.js";

// The trips list, with a search box and a sort menu (Stage 15 Milestone 2).
// Before that it was a bare loop over GET /trips in the server's fixed
// created_at DESC, which was fine for the two trips a new instance has and
// nothing else - and since Stage 14 other people's trips arrive in the same
// list, so there is more in it than you put there. That fetch order is still
// what the server sends; as of Stage 35 it is one of three orders the page can
// impose rather than the one it defaults to.
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
//
// The default order is "upcoming" rather than the server's created_at DESC
// (Stage 35). On a trip planner the interesting trip is the next one, and
// neither of the orders this list had put it first: created_at DESC is
// whichever trip was typed in most recently, and the old ascending "By start
// date" led with the oldest holiday on record. Plain *descending* start date
// is no better - it hands the top of the list to a vague idea pencilled in for
// 2029, over next week's flight.
//
// The choice is remembered per browser (Stage 35 Milestone 2), the way
// theme.js remembers light/dark: `sort` used to be a closure variable that the
// router reset on every render, so opening a trip and pressing Back put the
// list back in an order you had just changed it out of. Per *browser* rather
// than per history entry, which is the other half of this and belongs with the
// locations tab's version of the same problem - see the todo.md entry on view
// state and the URL.
//
// Name and added can be reversed as of Stage 43, by tapping the order again
// while it is the selection (menu.js's `reversible`). Upcoming cannot: it is
// three blocks with their own directions, and backwards it means nothing.
const SORTS = ["upcoming", "title", "added"];
const REVERSIBLE = ["title", "added"];
const DEFAULT_SORT = "upcoming";
const STORAGE_KEY = "caravel.trips.sort";

// localStorage throws outright in a few real configurations (private windows
// with storage blocked, embedded webviews), and an order is not worth failing
// to render a page over, so both accessors swallow. An unknown stored value
// falls back too, which is what happens to a browser that still holds the
// "newest" or "start" this stage removed.
//
// The value is the order, with ":reversed" appended when it runs backwards
// (Stage 43). A value stored before then has no suffix and so loads in its
// natural direction, which is the direction it was stored in.
function storedSort() {
  const fallback = { sort: DEFAULT_SORT, reversed: false };
  try {
    const stored = localStorage.getItem(STORAGE_KEY) ?? "";
    const [sort, suffix] = stored.split(":");
    if (!SORTS.includes(sort)) return fallback;
    if (suffix === undefined) return { sort, reversed: false };
    if (suffix === "reversed" && REVERSIBLE.includes(sort)) return { sort, reversed: true };
    return fallback;
  } catch {
    return fallback;
  }
}

function storeSort(value, reversed) {
  try {
    // The default is stored as the absence of a key, so "never chose" and
    // "chose the default" are one state - and a future change of default is
    // not silently stuck behind a value somebody selected once. Same reasoning
    // as theme.js and "auto".
    if (value === DEFAULT_SORT && !reversed) localStorage.removeItem(STORAGE_KEY);
    else localStorage.setItem(STORAGE_KEY, reversed ? `${value}:reversed` : value);
  } catch {
    // Unpersisted, but applied for this page: a control that visibly does
    // nothing is worse than one that forgets.
  }
}

export async function renderTripsPage(container) {
  let query = "";
  let { sort, reversed } = storedSort();
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
    // The remembered order, not the default one: the trigger has to say which
    // order the list is actually in.
    activeValue: sort,
    activeReversed: reversed,
    // Sorting by anything other than the default tints the trigger, so a
    // collapsed icon-only button on a phone still says the order is not the
    // one the list normally has. Still DEFAULT_SORT rather than `sort` - the
    // tint means "not the normal order", which stays true of a remembered
    // choice, and pinning it to the remembered value would make it mean
    // nothing at all.
    neutralValue: DEFAULT_SORT,
    // `.asc` is always oldest or A first, so for added - which starts newest
    // first - the natural label is the `.desc` one.
    items: [
      { value: "upcoming", label: t("trips.sort.upcoming") },
      { value: "title", reversible: true, label: t("trips.sort.title.asc"), reversedLabel: t("trips.sort.title.desc") },
      { value: "added", reversible: true, label: t("trips.sort.added.desc"), reversedLabel: t("trips.sort.added.asc") },
    ],
    onSelect: (value, { reversed: r }) => {
      sort = value;
      reversed = r;
      storeSort(value, r);
      apply();
    },
  });

  function matches(trip) {
    if (!query) return true;
    return `${trip.title} ${trip.subtitle ?? ""}`.toLowerCase().includes(query);
  }

  // Which of the three blocks a trip belongs to under the "upcoming" order.
  // A trip that has started but not ended is *current*, so it ranks with the
  // future ones - and since that block runs earliest-first, being already
  // under way puts it at the very top, which is where a trip you are on
  // belongs. ISO dates compare as strings.
  function upcomingBucket(trip, today) {
    if (!trip.start_date) return 2;
    return (trip.end_date ?? trip.start_date) >= today ? 0 : 1;
  }

  // Sorting a copy, never allTrips: sorting in place would fold one order into
  // the next and leave the result depending on what was picked before.
  function sorted(trips) {
    const out = [...trips];
    // Ties break by title, so the order is a property of the trips rather than
    // of the order the server happened to send them in.
    const tie = byTitle();
    // -1 while the reader has flipped the order. Only the primary key flips;
    // ties still break A-Z.
    const dir = reversed ? -1 : 1;
    if (sort === "title") {
      out.sort((a, b) => dir * tie(a, b));
    } else if (sort === "added") {
      // Newest first. The fetch arrives in exactly this order
      // (ORDER BY t.created_at DESC), but saying so in code costs nothing and
      // survives the query changing under us.
      out.sort((a, b) => dir * cmp(b.created_at, a.created_at) || tie(a, b));
    } else {
      const today = todayISO();
      out.sort((a, b) => {
        const bucket = upcomingBucket(a, today);
        const diff = bucket - upcomingBucket(b, today);
        if (diff !== 0) return diff;
        // Future: soonest first. Past: most recent first, so the trip you just
        // got back from leads the trips you will not be taking again. Undated
        // trips have no date to order by at all, so they fall back to when
        // they were added - and they sit last throughout, because unscheduled
        // is not the same as imminent.
        if (bucket === 0) return cmp(a.start_date, b.start_date) || tie(a, b);
        if (bucket === 1) return cmp(b.start_date, a.start_date) || tie(a, b);
        return cmp(b.created_at, a.created_at) || tie(a, b);
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

// Ordinary ascending compare for two ISO strings, returning 0 for equal so it
// chains into a tiebreak with `||`.
function cmp(a, b) {
  return a < b ? -1 : a > b ? 1 : 0;
}
