// The location categories and their colours, in one place.
//
// The server enforces the same set -- the CHECK constraint on items.category,
// validCategories in internal/httpapi/locations.go and in internal/assist/agent.go
// -- and TestCategoriesModuleMatchesTheServer pins this list to it, so adding a
// category means a migration, those two Go lists, this file, and an
// item.category.* key in every locale.
//
// The colours are the light-map marker palette. Why each one, and the darker
// twins the map switches to on a dark style, are in map-view.js next to the
// --marker-* custom properties: that is the only place the palette changes with
// a theme. Everywhere else a category dot is a fixed swatch.

// In legend, select and filter order.
export const CATEGORIES = ["site", "stay", "transport", "area", "food", "event", "shop"];

export const CATEGORY_COLORS = Object.freeze({
  site: "#16a34a",
  stay: "#7c3aed",
  transport: "#2563eb",
  area: "#e11d48",
  food: "#a16207",
  event: "#a21caf",
  shop: "#3f6212",
});

// For a category that is missing or not one of the above.
export const FALLBACK_CATEGORY_COLOR = "#71717a";

// An own-property lookup, so that "constructor" or "toString" coming back from
// somewhere is a grey dot and not a function interpolated into a style.
export function categoryColor(category) {
  return Object.hasOwn(CATEGORY_COLORS, category)
    ? CATEGORY_COLORS[category]
    : FALLBACK_CATEGORY_COLOR;
}
