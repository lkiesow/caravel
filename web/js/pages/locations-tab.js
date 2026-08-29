import { api } from "../api.js";
import { t, translatePage, getLocale } from "../i18n.js";
import { navigate } from "../router.js";
import { icon } from "../icon.js";
import { renderFilterMenu } from "../components/filter-menu.js";
import { renderMenu } from "../components/menu.js";
import "../components/location-card.js";
import { renderLoading } from "../components/loading.js";
import { canLocate, distanceKm, getCurrentPosition, locateErrorKey } from "../geolocation.js";
import { canEdit } from "../trip-role.js";
import { formatDateRange } from "../format.js";

const CATEGORIES = ["site", "stay", "transport"];

// Radii for the "near me" filter, in km. Coarse on purpose: the question is
// "walkable / a short drive / same region", not a precise number.
const DISTANCE_RADII_KM = [1, 2, 5, 10, 25];
const ANY_DISTANCE = "any";

// The orders the list can be read in. "added" rather than the trips list's
// "newest": locations have no reorder UI, so items.sort_order is the order they
// were created in, and calling that "newest" would be a lie about a list that
// reads oldest-first.
//
// A plain renderMenu, not a group in the filter menu beside it. Sorting is not
// filtering - it changes the order of the answer, not which questions the list
// is answering - and trips-page.js already established the separate sort
// trigger with this icon. Consistency between the app two list screens is
// worth more than one fewer control, and collapsing the filters into one
// trigger is exactly what made room for this one.
const SORTS = ["added", "title", "date"];
const DEFAULT_SORT = "added";

const ANY_TAG = "any";
const ANY_DATE = "any";
// "Not scheduled" is the reason the date filter is a small preset list and not
// only a range picker: while a trip is being planned, "what have I not placed
// yet" is the question asked most, and no range can express it.
const UNSCHEDULED = "unscheduled";
const SCHEDULED = "scheduled";

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
  let sort = DEFAULT_SORT;
  let activeTag = ANY_TAG;
  // The date filter has more states than a value: three presets plus a range,
  // so it carries a small object rather than a string. `mode` is what the
  // predicate switches on; from/to are only meaningful in "range".
  let dateFilter = { mode: ANY_DATE, from: "", to: "" };
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
        <div class="locations-sort-slot"></div>
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
    if (activeTag !== ANY_TAG && !(item.tags ?? []).includes(activeTag)) return false;
    if (!matchesDate(item)) return false;
    if (!query) return true;
    // Tags join the search as of Stage 26 Milestone 6: they are words somebody
    // chose for this location, so a search that ignored them would miss the
    // most deliberate labels on the page. `type` is still here and goes in
    // Milestone 7.
    return `${item.title} ${item.type ?? ""} ${(item.tags ?? []).join(" ")}`.toLowerCase().includes(query);
  }

  function matchesDate(item) {
    const dates = item.dates ?? [];
    switch (dateFilter.mode) {
      case UNSCHEDULED:
        return dates.length === 0;
      case SCHEDULED:
        return dates.length > 0;
      case "range": {
        const { from, to } = dateFilter;
        if (!from && !to) return true;
        // Overlap, not containment: a hotel booked the 5th to the 12th is
        // part of what happens on the 6th, so asking for the 6th has to find
        // it. An open end means unbounded in that direction, so a range with
        // only a start reads as "from here on".
        return dates.some((d) => (!to || d.start_date <= to) && (!from || d.end_date >= from));
      }
      default:
        return true;
    }
  }

  // Every tag in use on this trip, for the filter's options. Derived from the
  // list already in memory rather than from GET /trips/{id}/tags: the tab holds
  // every location, so asking the server for a projection of what it is already
  // looking at would be a second request for nothing. (The editor does call
  // that endpoint, because it holds one location and would otherwise fetch the
  // whole trip to learn three words.)
  function tripTags() {
    const seen = new Map();
    for (const item of allItems) {
      for (const tag of item.tags ?? []) {
        if (!seen.has(tag)) seen.set(tag, tag);
      }
    }
    return [...seen.values()].sort((a, b) => {
      const x = a.toLowerCase();
      const y = b.toLowerCase();
      if (x !== y) return x < y ? -1 : 1;
      return a < b ? -1 : a > b ? 1 : 0;
    });
  }

  // Sorts a copy rather than in place. allItems is in the order the API
  // returned, which is exactly what "as added" means, so it must never be
  // reordered.
  //
  // Being precise about what protects that, because it is easy to
  // misattribute: the caller already hands this a fresh array from .filter(),
  // so today the spread is belt and braces rather than the thing doing the
  // work -- removing it does not break anything, and no test catches it. It
  // stays so that sorted() is safe to call on any array, including allItems
  // itself if a later caller skips the filter. Same shape as trips-page.js.
  function sorted(items) {
    const out = [...items];
    if (sort === "title") {
      // Under the active locale, so German umlauts sort where a German reader
      // expects rather than after z. numeric so "Hut 2" precedes "Hut 10".
      const collator = new Intl.Collator(getLocale(), { sensitivity: "base", numeric: true });
      out.sort((a, b) => collator.compare(a.title, b.title));
    } else if (sort === "date") {
      // Earliest first, by the first range - the ranges arrive already sorted
      // from collapseDateRanges, so dates[0] is the earliest without sorting
      // them again. ISO dates compare as strings.
      //
      // A location with no dates goes last rather than first: it is
      // unscheduled, not imminent. That is the rule the trips list uses for a
      // trip with no start date, and it matters more here, since on a
      // half-planned trip most locations have no days yet and they would
      // otherwise bury the ones that do.
      const startOf = (item) => item.dates?.[0]?.start_date ?? null;
      out.sort((a, b) => {
        const x = startOf(a);
        const y = startOf(b);
        if (!x && !y) return 0;
        if (!x) return 1;
        if (!y) return -1;
        return x < y ? -1 : x > y ? 1 : 0;
      });
    }
    return out;
  }

  function applyFilters() {
    const visible = sorted(allItems.filter(matches));

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
  // Held by name so the tag group can be refilled once the locations arrive:
  // its options *are* the trip's tags, and the menu is built before the fetch.
  const tagGroup = {
    key: "tags",
    // Absent until the trip actually has tags. A filter offering only "Any
    // tag" is a row that can do nothing, and this is the one group whose
    // options can legitimately be empty.
    available: false,
    name: t("locations.filter.tags"),
    neutralLabel: t("locations.filter.anyTag"),
    neutralValue: ANY_TAG,
    activeValue: ANY_TAG,
    items: [],
    onSelect: (value) => {
      activeTag = value;
      applyFilters();
    },
  };

  const dateGroup = {
    key: "date",
    name: t("locations.filter.date"),
    neutralLabel: t("locations.filter.anyDate"),
    neutralValue: ANY_DATE,
    activeValue: ANY_DATE,
    isNeutral: () => dateFilter.mode === ANY_DATE,
    currentLabel: () => {
      if (dateFilter.mode === UNSCHEDULED) return t("locations.filter.unscheduled");
      if (dateFilter.mode === SCHEDULED) return t("locations.filter.scheduled");
      if (dateFilter.mode === "range") {
        return formatDateRange(dateFilter.from || null, dateFilter.to || null) ?? t("locations.filter.anyDate");
      }
      return t("locations.filter.anyDate");
    },
    onClear: () => {
      dateFilter = { mode: ANY_DATE, from: "", to: "" };
      applyFilters();
    },
    // Not a list of options: three presets and a range, which no set of
    // menuitemradio rows can express. See renderDatePanel.
    renderPanel: (body, { done }) => renderDatePanel(body, done),
  };

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
      tagGroup,
      dateGroup,
    ],
  });

  // The date panel: three presets, then a range. The presets are rendered as
  // menuitemradio rows so they read like every other option in this menu; the
  // range is a small form, because two dates and an Apply cannot be a row.
  function renderDatePanel(body, done) {
    const presets = [
      { value: ANY_DATE, label: t("locations.filter.anyDate") },
      { value: UNSCHEDULED, label: t("locations.filter.unscheduled") },
      { value: SCHEDULED, label: t("locations.filter.scheduled") },
    ];
    for (const preset of presets) {
      const btn = document.createElement("button");
      btn.type = "button";
      btn.setAttribute("role", "menuitemradio");
      btn.setAttribute("aria-checked", String(dateFilter.mode === preset.value));
      btn.dataset.value = preset.value;
      btn.insertAdjacentHTML("afterbegin", icon("check", { className: "menu__check" }));
      const label = document.createElement("span");
      label.textContent = preset.label;
      btn.append(label);
      btn.addEventListener("click", (e) => {
        e.stopPropagation();
        dateFilter = { mode: preset.value, from: "", to: "" };
        applyFilters();
        done();
      });
      body.appendChild(btn);
    }

    const form = document.createElement("form");
    form.className = "date-filter";
    // Stacked and labelled rather than two boxes with a dash between them. Two
    // native date inputs side by side are wider than this panel can be at
    // 324px - the panel is anchored to the trigger's right edge, so it had
    // nowhere to grow but off the left of the screen - and once they are
    // stacked, "which one is the start" needs saying out loud anyway.
    form.innerHTML = `
      <label class="date-filter__field">
        <span data-i18n="locations.filter.from"></span>
        <input type="date" name="from" />
      </label>
      <label class="date-filter__field">
        <span data-i18n="locations.filter.to"></span>
        <input type="date" name="to" />
      </label>
      <button type="submit" class="btn btn-secondary" data-i18n="locations.filter.apply"></button>
    `;
    translatePage(form);
    if (dateFilter.mode === "range") {
      form.from.value = dateFilter.from;
      form.to.value = dateFilter.to;
    }
    // The same guard the editor's date card uses: an end before its start is
    // refused by the browser rather than by a message of ours.
    form.from.addEventListener("change", () => {
      form.to.min = form.from.value;
    });
    form.to.min = form.from.value;

    form.addEventListener("submit", (e) => {
      e.preventDefault();
      e.stopPropagation();
      const from = form.from.value;
      const to = form.to.value;
      // Both ends empty is not a range, it is the neutral state - so Apply on
      // an empty form clears the filter rather than pretending to set one.
      dateFilter = from || to ? { mode: "range", from, to } : { mode: ANY_DATE, from: "", to: "" };
      applyFilters();
      done();
    });
    // Enter in either date field submits the form, not the page.
    form.addEventListener("keydown", (e) => e.stopPropagation());
    body.appendChild(form);
  }

  renderMenu(container.querySelector(".locations-sort-slot"), {
    iconName: "arrow-down-up",
    ariaLabel: "locations.sort.label",
    activeValue: DEFAULT_SORT,
    // Sorting by anything other than the default tints the trigger, so a
    // collapsed icon-only button on a phone still says the order is not the
    // one the list normally has - the same cue the filter funnel carries.
    neutralValue: DEFAULT_SORT,
    items: SORTS.map((value) => ({ value, label: t(`locations.sort.${value}`) })),
    onSelect: (value) => {
      sort = value;
      applyFilters();
    },
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

  // The tag options only exist once the locations do.
  const tags = tripTags();
  tagGroup.available = tags.length > 0;
  tagGroup.items = [
    { value: ANY_TAG, label: t("locations.filter.anyTag") },
    ...tags.map((tag) => ({ value: tag, label: tag })),
  ];
  filterMenu.refresh();

  applyFilters();
}
