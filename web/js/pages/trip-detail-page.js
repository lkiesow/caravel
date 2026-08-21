import { api } from "../api.js";
import { t, translatePage } from "../i18n.js";
import { icon } from "../icon.js";
import "../components/leaflet-map.js";
import { renderItemsTab } from "./locations-tab.js";
import { renderItineraryTab } from "./itinerary-tab.js";
import { renderFileList } from "../components/file-list.js";
import { renderChecklistList } from "../components/checklist-list.js";
import { renderSettingsTab } from "./settings-tab.js";
import { renderMembersTab } from "./members-tab.js";
import { TRIP_TABS, OVERFLOW_TRIP_TABS } from "../trip-tabs.js";
import { renderMenu } from "../components/menu.js";
import { navigate } from "../router.js";
import { formatDateRange } from "../format.js";
import { renderLoading } from "../components/loading.js";
import { renderNotFoundPage } from "./not-found-page.js";

const TABS = TRIP_TABS.map(({ key }) => key);

export async function renderTripDetailPage(container, { tripId, tab }) {
  renderLoading(container);

  let trip;
  try {
    trip = await api.get(`/trips/${tripId}`);
  } catch {
    renderNotFoundPage(container, { href: "/trips", labelKey: "common.home" });
    return;
  }

  // Locations (not Overview) is what users actually want to land on - see
  // Stage 02 review. "/trips/:tripId" has no tab segment, so it
  // canonicalizes itself to the real tab URL rather than leaving a
  // tab-less URL in the address bar and history.
  if (!tab || !TABS.includes(tab)) {
    tab = "locations";
    window.history.replaceState({}, "", `/trips/${tripId}/locations`);
  }

  function render() {
    // Subtitle and date range share one line, joined by a "·" that only
    // appears between two pieces that both actually exist. The range is
    // plain text rather than a labeled dt/dd pair - it reads fine as prose
    // but not as a table row.
    const dateRange = formatDateRange(trip.start_date, trip.end_date);
    const summaryParts = [];
    if (trip.subtitle) summaryParts.push(`<span class="trip-summary__subtitle"></span>`);
    if (dateRange) {
      if (summaryParts.length) summaryParts.push(`<span class="trip-summary__dot" aria-hidden="true">·</span>`);
      summaryParts.push(`<span class="trip-summary__dates">${dateRange}</span>`);
    }

    container.innerHTML = `
      <div class="page trip-detail">
        <a href="/trips" data-link class="back-link">${icon("arrow-left")} <span data-i18n="common.home"></span></a>
        ${trip.preview_image_url ? `<img class="trip-detail__cover" alt="" />` : ""}
        <div class="page__header">
          <h1></h1>
        </div>
        ${summaryParts.length ? `<div class="trip-summary">${summaryParts.join("")}</div>` : ""}
        <nav class="trip-tabs">
          ${TRIP_TABS.map(
            ({ key, icon: tabIcon, overflow }) =>
              `<button data-tab="${key}" class="${[key === tab ? "active" : "", overflow ? "trip-tabs__overflow-tab" : ""].filter(Boolean).join(" ")}">${icon(tabIcon)} <span data-i18n="trip.tabs.${key}"></span></button>`
          ).join("")}
          <div class="trip-tabs__more-slot"></div>
        </nav>
        <div class="trip-tab-content"></div>
      </div>
    `;
    translatePage(container);
    container.querySelector(".page__header h1").textContent = trip.title;
    const subtitleEl = container.querySelector(".trip-summary__subtitle");
    if (subtitleEl) subtitleEl.textContent = trip.subtitle;
    // src assigned as a property rather than interpolated into the template
    // above, the same way the title and subtitle are - no local escape helper
    // needed, and the URL can't break out of an attribute it never enters.
    // alt="" because it's decorative: the <h1> right below already names the
    // trip, so announcing the photo would just repeat it.
    const coverEl = container.querySelector(".trip-detail__cover");
    if (coverEl) coverEl.src = trip.preview_image_url;

    container.querySelectorAll("[data-tab]").forEach((btn) => {
      btn.addEventListener("click", () => {
        tab = btn.getAttribute("data-tab");
        // Pushed directly (not via router.js's navigate) so switching tabs
        // stays a local re-render instead of re-fetching the trip through a
        // full route match - this only updates history/the URL bar. Back/
        // forward still works: browser back dispatches "popstate", which
        // the app-level router does listen for, and it re-renders this
        // page fresh from the URL at that point.
        window.history.pushState({}, "", `/trips/${trip.id}/${tab}`);
        render();
      });
    });

    // "More" holds the tabs that don't fit a phone's row (see trip-tabs.js).
    // Both the row buttons and these menu items exist at every width; which
    // set is visible is a CSS decision, so there's no resize listener and no
    // re-render on rotation. The menu is the same component as the locations
    // filter rather than a third hand-rolled popup - it just wears a static
    // label and tab styling.
    renderMenu(container.querySelector(".trip-tabs__more-slot"), {
      iconName: "ellipsis",
      label: t("trip.tabs.more"),
      chevron: false,
      triggerClass: OVERFLOW_TRIP_TABS.some(({ key }) => key === tab) ? "active" : "",
      className: "menu--tabs",
      ariaLabel: "trip.tabs.more",
      // Same icon each section shows in the row, so a tab that moved into the
      // menu still looks like itself.
      items: OVERFLOW_TRIP_TABS.map(({ key, icon: tabIcon }) => ({ value: key, label: t(`trip.tabs.${key}`), iconName: tabIcon })),
      // No neutralValue: unlike a filter, one of these is always the current
      // section or none of them is, and the trigger's own `active` class
      // already carries that.
      activeValue: tab,
      onSelect: (key) => {
        tab = key;
        window.history.pushState({}, "", `/trips/${trip.id}/${tab}`);
        render();
      },
    });

    const content = container.querySelector(".trip-tab-content");
    if (tab === "locations") {
      renderItemsTab(content, trip.id);
    } else if (tab === "map") {
      content.innerHTML = `<leaflet-map trip-id="${trip.id}" locate></leaflet-map>`;
      // A marker popup's in-app link. leaflet-map.js can't let the router's
      // [data-link] interception handle it - that listener sits on document
      // and a click inside a shadow root retargets to the host - so it
      // dispatches the same "item-open" event location-card.js does, and the
      // page turns it into a navigation exactly as locations-tab.js does.
      content.addEventListener("item-open", (e) => {
        navigate(`/trips/${trip.id}/locations/${e.detail.itemId}`);
      });
    } else if (tab === "itinerary") {
      renderItineraryTab(content, trip);
    } else if (tab === "files") {
      renderFileList(content, `/trips/${trip.id}/files`);
    } else if (tab === "checklists") {
      renderChecklistList(content, trip.id);
    } else if (tab === "members") {
      renderMembersTab(content, trip);
    } else if (tab === "settings") {
      renderSettingsTab(content, trip, {
        onTripUpdated: (updated) => {
          Object.assign(trip, updated);
          render();
        },
      });
    }
  }

  render();
}
