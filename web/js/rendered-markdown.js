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

// The location page route, as app.js defines it. Matching it exactly, rather
// than accepting anything under /trips/, is what keeps the pin honest: it goes
// on links to a place and nothing else.
const LOCATION_PATH = /^\/trips\/[^/]+\/locations\/[^/]+$/;

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

// markInternalLinks marks the anchors in a freshly rendered note so the router
// picks them up, and puts a pin on the ones that point at a place. Call it
// straight after the innerHTML assignment, on the element that received it.
export function markInternalLinks(root) {
  if (!root) return;
  for (const link of root.querySelectorAll("a[href]")) {
    const href = link.getAttribute("href");
    if (!isInternalPath(href)) continue;
    // An author who asked for a new tab gets one; handing that to the router
    // would silently turn it into a same-tab navigation.
    if (link.target && link.target !== "_self") continue;
    link.setAttribute("data-link", "");
    if (!LOCATION_PATH.test(href)) continue;
    // A place should read as a place. The icon markup is a constant, and goes
    // in as a node rather than by rebuilding the anchor's innerHTML -- the
    // text around it is somebody's note.
    link.classList.add("rendered-link--location");
    link.insertAdjacentHTML("afterbegin", icon("map-pin", { className: "rendered-link__pin" }));
  }
}
