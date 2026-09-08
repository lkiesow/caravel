// Locale-aware comparators for lists the user reads by name.
import { getLocale } from "./i18n.js";

// Compare two objects by their `title`, under the active locale: German
// umlauts sort where a German reader expects rather than after z, and numeric
// so "Hut 2" precedes "Hut 10". Same settings the locations tab and the trips
// list sort with, so a title-sorted list reads the same wherever it appears.
//
// Built per call rather than once at import time because the locale can change
// while the app is open.
export function byTitle() {
  const collator = new Intl.Collator(getLocale(), { sensitivity: "base", numeric: true });
  return (a, b) => collator.compare(a.title, b.title);
}
