// Shared display formatting. Anything here is presentation-only: it takes
// API values (ISO dates, byte counts, ...) and produces strings for the UI,
// never the other way round.

// Compact human-readable date range, e.g. "Aug 18 – Aug 21, 2026" (year
// shown once when both dates fall in it) or "Aug 18, 2026 – Jan 2, 2027"
// across a year boundary. A single bound renders on its own, so a trip with
// only a start date doesn't read as an open-ended "2026-08-18 – ". Returns
// null when neither date is set, so callers can omit the line entirely
// rather than showing bare punctuation.
//
// Locale comes from the browser (Intl's undefined locale), matching how the
// itinerary formats its day headings.
export function formatDateRange(start, end) {
  if (!start && !end) return null;

  const parse = (d) => new Date(`${d}T00:00:00`);
  const short = (d) => new Intl.DateTimeFormat(undefined, { month: "short", day: "numeric" }).format(parse(d));
  const full = (d) => new Intl.DateTimeFormat(undefined, { month: "short", day: "numeric", year: "numeric" }).format(parse(d));

  // One day, named once. Both bounds are set and equal on a single-day range,
  // which is what a location that is on exactly one itinerary day produces
  // (Stage 25); "Sep 5 – Sep 5, 2026" would be a range with nothing in it.
  if (start && end && start === end) return full(start);

  if (start && end) {
    const sameYear = parse(start).getFullYear() === parse(end).getFullYear();
    return sameYear ? `${short(start)} – ${full(end)}` : `${full(start)} – ${full(end)}`;
  }
  return full(start || end);
}

// Byte count as a short human string: "28 B", "55.2 KB", "1.4 MB". One decimal
// above a kilobyte, none below - "1024.0 B" would be both wrong-looking and
// longer than the number it replaces.
//
// Lived in two copies (the file list and the location view page) until Stage 11,
// which is how they were free to disagree. Anything showing a size uses this.
export function formatBytes(bytes) {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
}

// Money.
//
// An amount is an integer in its currency's *minor unit* everywhere except the
// moment it is shown or typed — cents for EUR, whole yen for JPY, which has no
// minor unit at all. That is why neither of the two functions below takes a
// float: converting through one is how a ledger ends up a cent out.
//
// The exponent is asked of Intl rather than assumed to be 2. Hardcoding it
// would render ¥1200 as ¥12.00, and there is no list to maintain if the
// platform already knows.
//
// Locale comes from the browser (Intl's undefined locale), matching
// formatDateRange above and the itinerary's day headings.

// CURRENCIES mirrors db.Currencies in internal/db/domain.go, which is the
// authority: the server refuses anything not on its own list, so drift here
// costs a 400 with a clear message rather than a silently stored bad code. The
// same deliberate duplication as trip-role.js's RANK.
export const CURRENCIES = ["EUR", "USD", "GBP", "CHF", "SEK", "NOK", "DKK", "PLN", "CZK", "ISK", "JPY", "CAD", "AUD"];

// currencyExponent is how many decimal places a currency has: 2 for EUR, 0 for
// JPY. Unknown codes fall back to 2, which is what the server's allowlist makes
// unreachable — it is here so a hand-edited database cannot break rendering.
export function currencyExponent(currency) {
  try {
    return new Intl.NumberFormat(undefined, { style: "currency", currency }).resolvedOptions().maximumFractionDigits;
  } catch {
    return 2;
  }
}

// formatMoney renders minor units as a currency string: (1250, "EUR") is
// "€12.50", (1200, "JPY") is "¥1,200".
//
// The division is the one place a float appears, and it is safe where an
// exact-integer alternative would not be simpler: a double holds 15+ digits
// exactly, so any amount short of ten trillion cents divides and rounds back to
// the same string. Intl rounds to the currency's own precision regardless.
export function formatMoney(amountMinor, currency) {
  const exponent = currencyExponent(currency);
  const major = amountMinor / 10 ** exponent;
  try {
    return new Intl.NumberFormat(undefined, { style: "currency", currency }).format(major);
  } catch {
    // An unusable currency code must not take the row down with it: show the
    // number and the code rather than nothing.
    return `${major.toFixed(exponent)} ${currency}`;
  }
}

// parseMoney turns typed text into minor units, or null if it is not a valid
// amount. ("12.50", "EUR") is 1250; ("12,50", "EUR") is also 1250, because a
// German-speaking user types the separator their keyboard and their language
// give them and being strict about it would be a bug, not a validation.
//
// Done on the string rather than through parseFloat: parseFloat("12.55") * 100
// is 1254.9999999999998, and while Math.round covers that particular case, the
// class of bug it belongs to has no business anywhere near money. Padding the
// fraction and concatenating is exact by construction.
export function parseMoney(text, currency) {
  const exponent = currencyExponent(currency);
  const trimmed = String(text).trim().replace(",", ".");
  if (trimmed === "") return null;
  // No sign accepted: the server refuses amounts of zero or less, so a leading
  // minus is rejected here rather than travelling to be refused there.
  const match = /^(\d+)(?:\.(\d+))?$/.exec(trimmed);
  if (!match) return null;
  const [, whole, fraction = ""] = match;
  // More decimals than the currency has is a mistake worth reporting rather
  // than silently rounding: "12.567" EUR is not 12.57 with any confidence.
  if (fraction.length > exponent) return null;
  const minor = Number(whole + fraction.padEnd(exponent, "0"));
  if (!Number.isSafeInteger(minor) || minor <= 0) return null;
  return minor;
}

// moneyPlaceholder is the example shown in an empty amount field: "0.00" where
// the currency has cents, "0" where it does not.
export function moneyPlaceholder(currency) {
  const exponent = currencyExponent(currency);
  return exponent > 0 ? `0.${"0".repeat(exponent)}` : "0";
}

// moneyExample is a plausible amount to name in an error message. Derived from
// the currency rather than written into the copy: a JPY trip told to "enter an
// amount, for example 12.50" is being shown the exact thing it just refused.
export function moneyExample(currency) {
  return currencyExponent(currency) > 0 ? "12.50" : "1200";
}
