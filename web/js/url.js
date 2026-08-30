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
