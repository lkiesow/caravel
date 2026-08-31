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

// The zoom the coordinate bias is applied at. Street level: close enough that
// Google resolves the query against the right block, far enough that a
// slightly-off coordinate still contains the place.
const MAPS_BIAS_ZOOM = 17;

// googleMapsUrl builds the outbound "View on Google Maps" link for a place.
//
// One helper rather than a string in each caller. It was written out three
// times before Stage 29 Milestone 1 -- twice here in web/js and once in
// internal/httpapi/map.go -- and the three had drifted apart: the Go copy
// formatted coordinates with %f while both JS copies interpolated the raw
// number. Worse, the two popups in leaflet-map.js disagreed about where the URL
// even comes from, the trip-wide one reading a server-provided
// item.google_maps_url while the single-marker one built its own. A change made
// in two of the three places did not look wrong.
//
// The single-marker embed has no server payload to read -- it is driven
// entirely by its own attributes -- so this function is the single path for
// both JS callers, and its Go twin (googleMapsURL in internal/httpapi/map.go)
// is kept identical rather than being the one source. A UI spec asserts that
// the two agree.
//
// With a title, the link is a text search biased by the coordinates, which
// lands on the place's own Google entry rather than on a dropped pin. Without
// one it falls back to the documented coordinate form. The Go twin carries the
// full reasoning, including the three things measured in a browser that explain
// why the bias must be a /@lat,lng,z path segment and not a query parameter --
// read that comment before changing this one.
export function googleMapsUrl(lat, lng, title = "", address = "") {
  const coords = `${lat},${lng}`;

  let query = String(title ?? "").trim();
  if (query === "" || looksLikeCoordinate(query)) {
    return `https://www.google.com/maps/search/?api=1&query=${coords}`;
  }
  const addr = String(address ?? "").trim();
  if (addr !== "") query += `, ${addr}`;

  return `https://www.google.com/maps/search/${escapeMapsQuery(query)}/@${coords},${MAPS_BIAS_ZOOM}z`;
}

// Encodes a search phrase for the path segment it sits in.
//
// Not encodeURIComponent: the form verified in a browser during Stage 29
// planning is the one Google itself emits, spaces as "+" and commas left alone,
// and encodeURIComponent writes those as %20 and %2C. It also disagrees with
// Go's url.PathEscape about several ordinary characters -- an apostrophe is
// untouched here and %27 there -- so "Bob's Cafe" would have produced two
// different URLs from the two twins. This escapes exactly what would otherwise
// break the URL, in the same order as escapeMapsQuery in
// internal/httpapi/map.go. Keep the two in step.
function escapeMapsQuery(s) {
  return s
    .replaceAll("%", "%25") // first, so nothing below is escaped twice
    .replaceAll("#", "%23")
    .replaceAll("?", "%3F")
    .replaceAll("&", "%26")
    .replaceAll("+", "%2B")
    .replaceAll("/", "%2F")
    .replaceAll("\\", "%5C")
    .replaceAll(" ", "+");
}

// A title that is really just a coordinate pair makes a text search a
// tautology, so those take the coordinate form instead. Deliberately loose,
// and identical to looksLikeCoordinate in internal/httpapi/map.go.
function looksLikeCoordinate(s) {
  return s.includes(",") && /^[0-9+\-., ]+$/.test(s);
}

// openStreetMapUrl returns the feature page for an OpenStreetMap element, or
// null when the location has no OSM identity -- which is most of them: only a
// place saved through the address search has one, and a dropped pin is not an
// OSM feature.
//
// The link this project should arguably have had before the Google one. It is
// the OSM equivalent of a place card, carrying the opening hours, phone,
// website and full tag set somebody already mapped, and it costs no key, no
// third party and no request leaving the instance.
//
// The type and id are validated server-side on the way in (validate on
// itemLocationRequest in internal/httpapi/items.go, and geocode.toResult before
// that), and re-checked here rather than trusted. This is the same reasoning as
// safeHref above: the check that matters is the one at the render site, because
// a database written before the check existed is still a database this code has
// to render.
export function openStreetMapUrl(type, id) {
  if (!OSM_ELEMENT_TYPES.includes(type)) return null;
  if (!/^[0-9]+$/.test(String(id ?? ""))) return null;
  return `https://www.openstreetmap.org/${type}/${id}`;
}

const OSM_ELEMENT_TYPES = ["node", "way", "relation"];
