// Entity escaping for HTML built as template strings.
//
// One copy rather than one per file: there used to be twelve, two of which had
// drifted (they skipped the String() coercion and threw on a number), and eight
// files also carried an escapeAttr that was this same function under a name
// promising more than it did.
//
// escapeHtml is correct for text content and for *quoted* attribute values,
// and that is all. It says nothing about what a value means in the attribute
// it lands in: a javascript: URL escapes to a perfectly well-formed dangerous
// href. A stored URL headed for an href goes through safeHref in url.js first.

export function escapeHtml(s) {
  return String(s).replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[c]);
}
