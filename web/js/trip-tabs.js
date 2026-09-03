// Single source of truth for the trip detail tab bar (Locations/Map/
// Itinerary/Notes/Checklists/Files/Expenses/Members/Settings), shared between
// app.js (route table) and trip-detail-page.js (nav rendering) so the two
// lists can't drift. Every tab is a real route at every width; `overflow` only
// changes where its control lives in the bar.
//
// `overflow: true` moves a tab out of the row and into the "More" menu on
// narrow screens. Six labels do not fit a phone: at 324px each cell is 49px
// while the longest label overruns it (measured at 60px back when the Files
// tab still read "Documents"; "Einstellungen" is worse), so labels overran
// their cells and collided. The four that stay are the ones you move between
// while actually planning; everything else goes to "More". (Members joined the
// overflow group in Stage 14 Milestone 3: seven tabs never fit a phone row, so
// a new tab has no realistic choice but `overflow`. This sentence said "the two
// that go" until Milestone 9, because the Milestone 3 edit that should have
// updated it was a scripted replace with no assertion and silently matched
// nothing.)
//
// The split is deliberately for the whole `max-width: 640px` range rather
// than some narrower "small phone" cutoff, because the width six tabs need
// depends on the *language*: measured at the bar's own font size, six English
// labels need >=360px of label space and six German ones >=426px ("Einstellungen"
// alone is 71px against "Settings"' 42px). A media query can't know the locale,
// so a threshold tuned to English would break German on exactly the devices it
// was meant to help. Reusing the existing mobile breakpoint keeps one phone
// layout, correct in both locales, instead of a third variant to maintain.
//
// Order matters twice over, and getting it wrong is visible: this array is the
// order the desktop bar shows *and* the order the phone splits into a row of
// four plus a More menu. Files used to sit between itinerary and checklists
// while being an overflow tab, so desktop read "... itinerary, files,
// checklists ..." while the phone row read "... itinerary, checklists" with
// files hidden in More — the same two tabs in opposite relative order depending
// on the width. Keeping every overflow tab after every primary one makes the
// two agree by construction, so a future tab should be inserted with that in
// mind rather than wherever it reads best in this list.
//
// That invariant is what decided Stage 31, and the reasoning is worth keeping
// because the question will come back with the next tab. Notes belongs beside
// Itinerary — the two are read together while planning — but the phone row was
// already full at four. Marking Notes `overflow` and leaving it in place would
// have put an overflow tab ahead of a primary one, which is precisely the bug
// above. Widening the row to five primaries plus More means six cells, which
// the measurements above say does not fit. So a primary tab had to leave, and
// Checklists was the one: of the four, it is the tab you visit to tick
// something off rather than the one you pass through while planning, and it is
// also the longest German label in the row ("Checklisten"). Demoting it keeps
// the row at four, keeps the invariant, and puts Notes exactly where it reads.
export const TRIP_TABS = [
  { key: "locations", icon: "map-pin" },
  { key: "map", icon: "map" },
  { key: "itinerary", icon: "calendar" },
  { key: "notes", icon: "notebook-pen" },
  { key: "checklists", icon: "list-checks", overflow: true },
  { key: "files", icon: "file-text", overflow: true },
  // Expenses is an overflow tab because eight tabs cannot share a phone row,
  // and it is placed here rather than next to the planning tabs to keep the
  // invariant above: every overflow tab after every primary one.
  { key: "expenses", icon: "wallet", overflow: true },
  { key: "members", icon: "users", overflow: true },
  { key: "settings", icon: "settings", overflow: true },
];

// The tabs that stay in the row on narrow screens, in bar order.
export const PRIMARY_TRIP_TABS = TRIP_TABS.filter((t) => !t.overflow);

// The tabs that move into the "More" menu on narrow screens, in bar order.
export const OVERFLOW_TRIP_TABS = TRIP_TABS.filter((t) => t.overflow);
