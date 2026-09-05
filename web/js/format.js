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

// Exchange rates. Stage 32.
//
// A trip stores one rate per additional currency, as an integer in parts per
// billion converting the *minor unit* of that currency into the *minor unit*
// of the trip's main currency. Nobody types that number, and nobody should
// have to read it: the pair below is what stands between it and the field.
//
// The server deliberately has no idea how many decimal places a currency has
// -- that knowledge is here, from Intl, and is the reason the stored rate is
// minor-to-minor at all. So folding the two exponents in is this file's job.
// One yen, which has no minor unit, is 0.58 cents: type "0.0058" for JPY on a
// EUR trip and 580000000 is stored.
//
// RATE_SCALE is the "per billion" part, as a power of ten rather than a
// literal, because every calculation below is done on exponents.
const RATE_SCALE = 9;

// RATE_ONE is the rate of a currency against itself: the main currency's
// implied rate, and the identity convertMinor preserves exactly. Mirrors
// db.RateOne in internal/db/domain.go, the same deliberate duplication as
// CURRENCIES above.
export const RATE_ONE = 10 ** RATE_SCALE;

// rateShift is how many powers of ten separate the typed rate from the stored
// one: the billion, plus the difference between the two currencies' exponents.
// JPY (0) into EUR (2) is 9 + 2 - 0 = 11, so 0.0058 becomes 58 * 10^7.
function rateShift(foreign, main) {
  return RATE_SCALE + currencyExponent(main) - currencyExponent(foreign);
}

// parseRate turns a typed rate into the integer to store, or null if it is not
// a usable one. ("0.0058", "JPY", "EUR") is 580000000.
//
// Done on the string rather than through parseFloat, exactly as parseMoney is
// and for exactly the same reason: 0.0058 * 1e11 is not 580000000 in binary
// floating point, and a rate is the multiplier under every amount on the trip.
// Splitting the digits from the decimal point and shifting by a power of ten is
// exact by construction.
export function parseRate(text, foreign, main) {
  const trimmed = String(text).trim().replace(",", ".");
  if (trimmed === "") return null;
  // No sign, and no exponent notation: the server refuses a rate of zero or
  // less, and "1e-3" in a currency field is far more likely a typo than intent.
  const match = /^(\d*)(?:\.(\d+))?$/.exec(trimmed);
  if (!match) return null;
  const [, whole = "", fraction = ""] = match;
  if (whole === "" && fraction === "") return null;

  const shift = rateShift(foreign, main) - fraction.length;
  // More decimals than the shift can absorb would need a fraction of a part
  // per billion. Reported rather than silently rounded, the same call
  // parseMoney makes about an over-precise amount.
  if (shift < 0) return null;

  const digits = (whole + fraction).replace(/^0+/, "");
  if (digits === "") return null;
  const ppb = Number(digits + "0".repeat(shift));
  if (!Number.isSafeInteger(ppb) || ppb <= 0) return null;
  return ppb;
}

// formatRate is parseRate inverted: the string to put back in the field for a
// stored rate. (580000000, "JPY", "EUR") is "0.0058".
//
// Also integer-only. The digits are padded and a decimal point is inserted
// rather than dividing, so what comes back is exactly what was typed -- a
// round trip through the form must not drift the rate it is displaying.
export function formatRate(ratePPB, foreign, main) {
  const shift = rateShift(foreign, main);
  const digits = String(ratePPB).padStart(shift + 1, "0");
  const whole = digits.slice(0, digits.length - shift);
  // Trailing zeros are noise in a rate: 0.00580000 says nothing 0.0058 does
  // not, and the field is easier to correct without them.
  const fraction = digits.slice(digits.length - shift).replace(/0+$/, "");
  return fraction === "" ? whole : `${whole}.${fraction}`;
}

// convertMinor mirrors the server's own conversion, for the live preview under
// the amount field. The server remains the authority -- every stored total and
// balance comes back converted from it -- and this exists only so the number
// the user most wants confirmed does not need a round trip.
//
// Number is exact to 2^53, and an amount times a rate stays far inside that for
// any real expense; Math.round then matches the server's half-away-from-zero
// for the positive amounts the column allows.
export function convertMinor(amountMinor, ratePPB) {
  return Math.round((amountMinor * ratePPB) / 10 ** RATE_SCALE);
}
