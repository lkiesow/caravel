// Single source of truth for the trip detail tab bar (Locations/Map/
// Itinerary/Documents/Checklists/Settings), shared between app.js (route
// table) and trip-detail-page.js (nav rendering) so the two lists can't
// drift.
export const TRIP_TABS = [
  { key: "locations", icon: "map-pin" },
  { key: "map", icon: "map" },
  { key: "itinerary", icon: "calendar" },
  { key: "documents", icon: "file-text" },
  { key: "checklists", icon: "list-checks" },
  { key: "settings", icon: "settings" },
];
