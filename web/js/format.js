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

  if (start && end) {
    const sameYear = parse(start).getFullYear() === parse(end).getFullYear();
    return sameYear ? `${short(start)} – ${full(end)}` : `${full(start)} – ${full(end)}`;
  }
  return full(start || end);
}
