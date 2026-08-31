// Deciding whether a stored URL may become an href.
//
// The server refuses anything but http and https on the way in (see
// validateLinkURL in internal/httpapi/items.go), so on a database written
// entirely by the current code this module never rejects anything. It exists
// for the database that is not: link rows written before that check existed
// are still there, and "javascript:alert(1)" in an href is a working link,
// not a broken one.
//
// A module of its own rather than a helper copied into the two pages that
// render links. Every other escaping helper in web/js is duplicated per file
// -- escapeHtml and escapeAttr appear in seven -- and that is tolerable for
// entity escaping, where a divergent copy is a rendering bug. A divergent
// copy of this is a hole.
//
// Note what escapeAttr does *not* do, which is what made this necessary: it is
// an alias of escapeHtml, so it escapes &<>"' and says nothing whatever about
// schemes. Quoting a javascript: URL into an attribute produces a perfectly
// well-formed dangerous link.

const SAFE_SCHEMES = ["http:", "https:"];

// safeHref returns the URL when it is safe to put in an href, and null when it
// is not. Callers render a rejected URL as text rather than dropping it: the
// person looking at it should be able to see what is stored, and the whole
// point is that it must not be clickable.
export function safeHref(raw) {
  const trimmed = String(raw ?? "").trim();
  if (trimmed === "") return null;
  let parsed;
  try {
    parsed = new URL(trimmed);
  } catch {
    // Relative URLs land here, which is the answer we want: these are external
    // links and one that is not absolute is not one of ours to open.
    return null;
  }
  // URL lowercases the protocol itself, so "JavaScript:" is already handled.
  if (!SAFE_SCHEMES.includes(parsed.protocol)) return null;
  if (parsed.host === "") return null;
  return trimmed;
}

// googleMapsUrl builds the outbound "View on Google Maps" link for a place.
//
// One helper rather than a string in each caller. It was written out three
// times before Stage 29 Milestone 1 -- twice here in web/js and once in
// internal/httpapi/map.go -- and the three had drifted apart: the Go copy
// formatted coordinates with %f (six decimals, trailing zeros and all) while
// both JS copies interpolated the raw number. Worse, the two popups in
// leaflet-map.js disagreed about where the URL even comes from, the trip-wide
// one reading a server-provided item.google_maps_url while the single-marker
// one built its own. A change made in two of the three places did not look
// wrong.
//
// The single-marker embed has no server payload to read -- it is driven
// entirely by its own attributes -- so this function is the single path for
// both JS callers, and its Go twin (googleMapsURL in internal/httpapi/map.go)
// is kept byte-for-byte identical rather than being the one source. The Go
// side formats with strconv.FormatFloat(v, 'f', -1, 64), which is the same
// shortest-round-trip form JS gives a number in a template literal, so the
// same place produces the same URL wherever the link is rendered. A UI spec
// asserts that.
export function googleMapsUrl(lat, lng) {
  return `https://www.google.com/maps/search/?api=1&query=${lat},${lng}`;
}
