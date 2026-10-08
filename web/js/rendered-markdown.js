import { api } from "./api.js";
import { t } from "./i18n.js";
import { icon } from "./icon.js";

// Making the links inside rendered note HTML behave like the rest of the app's
// links.
//
// The note HTML is the server's -- goldmark plus bluemonday, see
// internal/markdown -- so the anchors in it are whatever the author typed, and
// they arrive bare. The router only intercepts anchors carrying data-link
// (web/js/router.js), so a link to a place in this very trip did a full page
// load: the app booted again, refetched everything, and flashed white, where
// every other in-app link is instant.
//
// The marking cannot be done in the markdown, and not for want of trying:
// bluemonday strips a hand-written data-link attribute, which is correct of it.
// So it happens here, after insertion, keyed on the shape of the href.
//
// A module of its own rather than a copy at each of the three sinks, for the
// same reason url.js gives: the rule about which hrefs are ours is a security
// boundary of sorts -- get it wrong and an external URL is handed to the
// client-side router -- and a divergent copy of that is a hole, not a
// rendering bug.
//
// A link to a place that has since been deleted is not a link any more: the @
// picker writes a plain markdown link, nothing ties it to the place, and
// following it lands on a 404. Given the trip's live location ids, such a link
// is turned back into its text, muted and struck through, so the note still
// says what it said and no longer offers a dead end. Only links into the trip
// being shown are judged -- another trip's places are not ours to know about --
// and without the ids nothing is: a failed fetch must not declare every place
// in the note gone, and a link that 404s is the lesser mistake.

// The location page route, as app.js defines it. Matching it exactly, rather
// than accepting anything under /trips/, is what keeps the pin honest: it goes
// on links to a place and nothing else.
const LOCATION_PATH = /^\/trips\/([^/]+)\/locations\/([^/]+)$/;

// Ours means a root-relative path. Note what is excluded and why:
//
//   //evil.example       protocol-relative -- a different origin wearing a
//                        leading slash, which is exactly the shape this has to
//                        refuse.
//   /\evil.example       the same trick with a backslash, which several
//                        browsers normalise into the one above.
//   https://...          absolute, so not ours even when the host matches --
//                        an author who wrote the full URL asked for a real
//                        navigation.
//   #anchor, mailto:     not a path at all.
function isInternalPath(href) {
  return href.startsWith("/") && !href.startsWith("//") && !href.startsWith("/\\");
}

// loadTripLocationIds resolves to the set of the trip's live location ids, for
// markInternalLinks -- or to null when they cannot be had, which turns the
// dead-link check off rather than failing every link.
export function loadTripLocationIds(tripId) {
  return api
    .get(`/trips/${tripId}/locations`)
    .then((list) => (Array.isArray(list) ? new Set(list.map((location) => location.id)) : null))
    .catch(() => null);
}

// markInternalLinks marks the anchors in a freshly rendered note so the router
// picks them up, and puts a pin on the ones that point at a place. Call it
// straight after the innerHTML assignment, on the element that received it.
// With `tripId` and `locationIds` (from loadTripLocationIds), links to that
// trip's deleted places are unlinked instead -- see the top of this file.
export function markInternalLinks(root, { tripId, locationIds } = {}) {
  if (!root) return;
  for (const link of root.querySelectorAll("a[href]")) {
    const href = link.getAttribute("href");
    if (!isInternalPath(href)) continue;
    const place = LOCATION_PATH.exec(href);
    if (place && locationIds && place[1] === tripId && !locationIds.has(place[2])) {
      unlinkDeleted(link);
      continue;
    }
    // An author who asked for a new tab gets one; handing that to the router
    // would silently turn it into a same-tab navigation.
    if (link.target && link.target !== "_self") continue;
    link.setAttribute("data-link", "");
    if (!place) continue;
    // A place should read as a place. The icon markup is a constant, and goes
    // in as a node rather than by rebuilding the anchor's innerHTML -- the
    // text around it is somebody's note.
    link.classList.add("rendered-link--location");
    link.insertAdjacentHTML("afterbegin", icon("map-pin", { className: "rendered-link__pin" }));
  }
}

// The anchor's own children move across, so whatever the author put in the
// link text -- emphasis, code -- survives; the label is built with DOM calls
// for the same reason the pin is: the text around it is somebody's note.
function unlinkDeleted(link) {
  const label = t("tripNotes.deletedLocation");
  const span = document.createElement("span");
  span.className = "rendered-link--dead";
  span.title = label;
  span.append(...link.childNodes);
  // Still a place, so still the pin -- struck through with the text, by CSS.
  span.insertAdjacentHTML("afterbegin", icon("map-pin", { className: "rendered-link__pin" }));
  const sr = document.createElement("span");
  sr.className = "sr-only";
  sr.textContent = ` (${label})`;
  span.append(sr);
  link.replaceWith(span);
}
