// Single source of truth for the trip detail tab bar (Locations/Map/
// Itinerary/Documents/Checklists/Settings), shared between app.js (route
// table) and trip-detail-page.js (nav rendering) so the two lists can't
// drift. Every tab is a real route at every width; `overflow` only changes
// where its control lives in the bar.
//
// `overflow: true` moves a tab out of the row and into the "More" menu on
// narrow screens. Six labels do not fit a phone: at 324px each cell is 49px
// while "Documents" alone needs 60px, so labels overran their cells and
// collided. Documents and Settings are the two that go, being the least
// frequently used of the six - the four that stay are the ones you move
// between while actually planning.
//
// The split is deliberately for the whole `max-width: 640px` range rather
// than some narrower "small phone" cutoff, because the width six tabs need
// depends on the *language*: measured at the bar's own font size, six English
// labels need >=360px of label space and six German ones >=426px ("Einstellungen"
// alone is 71px against "Settings"' 42px). A media query can't know the locale,
// so a threshold tuned to English would break German on exactly the devices it
// was meant to help. Reusing the existing mobile breakpoint keeps one phone
// layout, correct in both locales, instead of a third variant to maintain.
export const TRIP_TABS = [
  { key: "locations", icon: "map-pin" },
  { key: "map", icon: "map" },
  { key: "itinerary", icon: "calendar" },
  { key: "documents", icon: "file-text", overflow: true },
  { key: "checklists", icon: "list-checks" },
  { key: "settings", icon: "settings", overflow: true },
];

// The tabs that stay in the row on narrow screens, in bar order.
export const PRIMARY_TRIP_TABS = TRIP_TABS.filter((t) => !t.overflow);

// The tabs that move into the "More" menu on narrow screens, in bar order.
export const OVERFLOW_TRIP_TABS = TRIP_TABS.filter((t) => t.overflow);
