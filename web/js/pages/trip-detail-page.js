import { api } from "../api.js";
import { t, translatePage } from "../i18n.js";
import { icon } from "../icon.js";
import "../components/leaflet-map.js";
import { renderItemsTab } from "./locations-tab.js";
import { renderItineraryTab } from "./itinerary-tab.js";
import { renderDocumentList } from "../components/document-list.js";
import { renderChecklistList } from "../components/checklist-list.js";
import { renderSettingsTab } from "./settings-tab.js";
import { TRIP_TABS } from "../trip-tabs.js";

const TABS = TRIP_TABS.map(({ key }) => key);

export async function renderTripDetailPage(container, { tripId, tab }) {
  let trip;
  try {
    trip = await api.get(`/trips/${tripId}`);
  } catch {
    container.innerHTML = `<p>${t("common.notFound")}</p><a href="/trips" data-link>${t("common.back")}</a>`;
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
    // appears between two pieces that both actually exist - see
    // formatDateRange below for why the range itself is plain text, not a
    // labeled dt/dd pair (that reads fine as prose but not as a table row).
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
        <div class="page__header">
          <h1></h1>
        </div>
        ${summaryParts.length ? `<div class="trip-summary">${summaryParts.join("")}</div>` : ""}
        <nav class="trip-tabs">
          ${TRIP_TABS.map(
            ({ key, icon: tabIcon }) =>
              `<button data-tab="${key}" class="${key === tab ? "active" : ""}">${icon(tabIcon)} <span data-i18n="trip.tabs.${key}"></span></button>`
          ).join("")}
        </nav>
        <div class="trip-tab-content"></div>
      </div>
    `;
    translatePage(container);
    container.querySelector(".page__header h1").textContent = trip.title;
    const subtitleEl = container.querySelector(".trip-summary__subtitle");
    if (subtitleEl) subtitleEl.textContent = trip.subtitle;

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

    const content = container.querySelector(".trip-tab-content");
    if (tab === "locations") {
      renderItemsTab(content, trip.id);
    } else if (tab === "map") {
      content.innerHTML = `<leaflet-map trip-id="${trip.id}"></leaflet-map>`;
    } else if (tab === "itinerary") {
      renderItineraryTab(content, trip);
    } else if (tab === "documents") {
      renderDocumentList(content, `/trips/${trip.id}/documents`);
    } else if (tab === "checklists") {
      renderChecklistList(content, trip.id);
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

// Compact human-readable date range for the under-title summary line, e.g.
// "Aug 18 – Aug 21, 2026" (year shown once when both dates fall in it) or
// "Aug 18, 2026 – Jan 2, 2027" across a year boundary. Returns null when
// neither date is set, so the caller can omit the summary line entirely
// rather than showing bare punctuation.
function formatDateRange(start, end) {
  if (!start && !end) return null;

  const parse = (d) => new Date(`${d}T00:00:00`);
  const short = (d) => new Intl.DateTimeFormat(undefined, { month: "short", day: "numeric" }).format(parse(d));
  const full = (d) => new Intl.DateTimeFormat(undefined, { month: "short", day: "numeric", year: "numeric" }).format(parse(d));

  if (start && end) {
    const sameYear = parse(start).getFullYear() === parse(end).getFullYear();
    return sameYear ? `${short(start)} – ${full(end)}` : `${full(start)} – ${full(end)}`;
  }
  return full(start || end);
}
