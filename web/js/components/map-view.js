import { api } from "../api.js";
import { t, getLocale } from "../i18n.js";
import { formatDistance } from "../format.js";
import { icon } from "../icon.js";
import {
  LOCATE_CANCELLED,
  locateErrorKey,
  locateUnavailableReason,
  watchPosition,
} from "../geolocation.js";
import { compassAllowed, requestCompassPermission, watchCompass } from "../heading.js";
import { googleMapsUrl } from "../url.js";
import { eventBus } from "../eventbus.js";
import { resolveMapTheme } from "../map-theme.js";

// The map, as the instance has it configured. Defaults duplicated from
// internal/httpapi/map.go on purpose: they are what the map falls back to when
// the request fails, and a Map tab of nothing is a worse answer to "the config
// endpoint is briefly unreachable" than the map Caravel shipped with.
//
// Two styles, because light and dark are a per-browser preference
// (map-theme.js) and the instance has to be able to answer both. The server
// resolves them -- including repeating the light one when an operator named no
// dark counterpart -- so there is no naming convention guessed at here.
const DEFAULT_MAP_CONFIG = {
  style_url: "/js/vendor/map-styles/liberty.json",
  dark_style_url: "/js/vendor/map-styles/dark.json",
};

// Memoised at module scope, not per instance: three routes mount a map and a
// trip page can swap between them, but the answer is instance-wide and cannot
// change without a server restart. One request per page load, not per map.
let mapConfigPromise = null;

function loadMapConfig() {
  if (!mapConfigPromise) {
    mapConfigPromise = api.get("/map/config").catch(() => DEFAULT_MAP_CONFIG);
  }
  return mapConfigPromise;
}

// MapLibre draws from a *style*, not from a tile layer, so there is no longer
// a one-line "here is the tile URL" call: the server names two style
// documents and this fetches whichever the current scheme calls for.
//
// Raster tiles are gone entirely as of Stage 30 Milestone 6, along with
// CARAVEL_TILE_URL and its two companions. That setting existed only because
// pre-rendered labels cannot follow a reader's language, and vector labels do
// -- so keeping a second rendering path would have meant two sets of
// documentation, two code paths, and a per-reader light/dark preference that
// silently did nothing wherever an operator had pinned a provider.
//
// Memoised per URL for the same reason loadMapConfig is: three mounts, two
// styles, and re-parsing 25KB of JSON per map is waste.
const styleCache = new Map();

async function buildStyle(config, scheme) {
  const url = (scheme === "dark" ? config.dark_style_url : config.style_url) || config.style_url;
  if (!styleCache.has(url)) {
    styleCache.set(
      url,
      fetch(url).then((r) => {
        if (!r.ok) throw new Error(`style ${r.status}`);
        return r.json();
      })
    );
  }
  try {
    // Structured-cloned rather than handed out: MapLibre mutates the style
    // object it is given, so two maps sharing one parsed document would
    // corrupt each other -- and the trip page mounts a second map without
    // tearing the first one down.
    return localiseLabels(structuredClone(await styleCache.get(url)), getLocale());
  } catch {
    styleCache.delete(url);
    // A dark style that will not load falls back to the light one rather than
    // to nothing. The server already repeats the light URL when an operator
    // named no dark counterpart, so reaching here means the document itself is
    // broken - and one working cartography beats an empty rectangle.
    if (url !== config.style_url) return buildStyle(config, "light");
    return null;
  }
}

// Labels in the reader's own language, which is the thing raster tiles could
// never do: their labels are pixels baked in before anyone asked, so the whole
// instance had to share one language. A vector style draws them in the browser,
// so this rewrites the expression that chooses the text.
//
// The stock OpenFreeMap styles ship
//   ["case", ["has","name:nonlatin"],
//            ["concat", ["get","name:latin"], "\n", ["get","name:nonlatin"]],
//            ["coalesce", ["get","name_en"], ["get","name"]]]
// which is "English for everyone, and both scripts stacked where the local one
// is non-Latin". Replaced with a plain preference chain: the reader's locale,
// then whatever Latin-script name the data has, then the local name. The last
// two are what keeps a place labelled at all when it has no translation.
//
// OpenMapTiles carries both spellings for the two locales this app supports --
// `name:de` and `name_de` -- and which one a given feature has varies, so both
// are asked for before falling back.
//
// NOT every text-field layer, which is the trap here: three layers in each
// style are road shields, whose text-field is ["to-string", ["get","ref"]].
// A motorway shield reads "A1", which is not a name and has no translation, so
// rewriting those would blank every shield on the map. Only expressions that
// actually ask for a name are touched.
function localiseLabels(style, locale) {
  for (const layer of style.layers || []) {
    const field = layer.layout?.["text-field"];
    if (!field || !readsAName(field)) continue;
    layer.layout["text-field"] = [
      "coalesce",
      ["get", `name:${locale}`],
      ["get", `name_${locale}`],
      ["get", "name:latin"],
      ["get", "name"],
    ];
  }
  return style;
}

// Does this expression read any of the name fields? Walked rather than
// string-matched so that a nested expression cannot hide a ["get", "name..."]
// from it, and so "ref" can never look like a name.
function readsAName(expr) {
  if (!Array.isArray(expr)) return false;
  if (expr[0] === "get" && typeof expr[1] === "string" && /^name([:_]|$)/.test(expr[1])) {
    return true;
  }
  return expr.some((part) => readsAName(part));
}

const CATEGORY_COLORS = {
  site: "#16a34a",
  stay: "#7c3aed",
  transport: "#2563eb",
  area: "#e11d48",
  food: "#a16207",
  event: "#a21caf",
  shop: "#3f6212",
};

// The categories, in legend order. Derived from the palette above so that the
// legend, the filter set and the marker colours cannot disagree about what the
// categories are.
const CATEGORIES = Object.keys(CATEGORY_COLORS);

// Zoom used when the view can't be derived from spread-out markers - a
// single marker, or a set of markers that all sit in the same place. Close
// enough to read street names, and shallow enough that any tile provider has
// something to serve - the layer's own maxZoom is configurable and sits well
// past what most providers actually render.
const SINGLE_MARKER_ZOOM = 14;

// How long the gesture hint stays up. Long enough to read six words, short
// enough that it is gone before it becomes the thing you are looking at.
const GESTURE_HINT_MS = 1500;

// Wheel pixels per zoom level, and the ceiling on what a single flick may
// bank. 60 is Leaflet's own wheelPxPerZoomLevel, so one mouse notch (which
// Firefox reports as 3 lines, i.e. 60px) is exactly one level.
const WHEEL_PX_PER_ZOOM = 60;
const WHEEL_ACCUM_CAP = 240;

// Category colour for a marker, for items whose category is unknown or not
// one of the seven the app defines (the single-marker mode gets it from an
// attribute, so it can legitimately be absent).
const FALLBACK_MARKER_COLOR = "#71717a";

// The marker being *placed* in pick mode. Amber deliberately: none of the
// category colours above, and not --color-accent either, which is the
// same #2563eb transport already uses. It is also drawn as a ring with a
// centre dot rather than as a plain disc, so it reads as "the point you are
// setting" rather than as one more category.
const PICK_MARKER_COLOR = "#ea580c";

// Coordinates are emitted at 6 decimals (~11cm). Without this a map click
// writes a 17-digit float straight into a number input.
const PICK_PRECISION = 1e6;

// "You are here". Not a category colour and not the pick amber either - this
// marker means something different from both, and the accuracy ring is the
// same hue at low opacity so the two read as one thing.
const HERE_MARKER_COLOR = "#0891b2";

// The GeoJSON source and layer prefix for the accuracy ring.
const ACCURACY_SOURCE = "here-accuracy";

// The *closest* zoom used when centring on the device's position. Closer than
// SINGLE_MARKER_ZOOM: you know roughly where you are, so the useful question
// is what is on the next street, not which region this is.
//
// A ceiling since Stage 36 rather than the answer. It used to be applied
// unconditionally, which is how a coast fix came to look like a confident dot:
// at zoom 15 a pixel is about 2.8m at this latitude, so the 3km accuracy ring
// that was supposed to give the reading away had a radius of roughly 1070px
// and sat entirely outside the viewport. The ring was drawn correctly and
// could not be seen. zoomForAccuracy() is what fixes that.
const HERE_ZOOM = 15;

// Breathing room around the accuracy ring when the camera fits it, so the ring
// reads as a ring rather than as the edge of the viewport.
const ACCURACY_FIT_PADDING = 32;

// When the marker turns into an arrow along the direction of travel (Stage
// 42). Two thresholds, not one: a walker's speed hovers around any single
// value, and the marker would flicker between dot and arrow with every fix.
//
// 1.0 m/s is a slow walk. The first version used 1.5 -- a *brisk* walk, on
// the theory that the heading is noise below it -- and on a real walk that
// put the switch right on top of ordinary walking pace (1.2-1.4 m/s), so the
// marker flickered exactly where it was meant to be steady.
const MOVING_ON_MPS = 1.0;
const MOVING_OFF_MPS = 0.5;

// How long the arrow survives without a fix that supports it -- one too slow,
// one with no heading, or no fix at all. Platforms drop the heading from the
// odd fix mid-walk, and GNSS speed dips for a second at a time; reverting to
// the dot on any single one of those was the other half of the flicker seen
// on a real walk. Long enough to ride out a few bad fixes at the ~1Hz
// tracking pace, short enough that stopping at a crossing shows as stopping.
//
// This is also the staleness rule: an arrow is a claim about *now*, so when
// fixes stop arriving altogether -- the signal went, the watch is paused -- it
// goes back to the dot after the same time rather than freezing at the last
// angle.
const COURSE_HOLD_MS = 5000;
const COURSE_STALE_CHECK_MS = 1000;

// How wide the compass cone is, as degrees either side of the direction
// (Milestone 3). Where the platform reports how far off the compass may be --
// only iOS does -- that is the width, clamped: below 15 a cone reads as a
// laser pointer, which no phone compass deserves, and past 45 it is a
// semicircle that says nothing. Elsewhere a fixed 30, since there is nothing
// to go on.
const CONE_SPREAD_DEFAULT = 30;
const CONE_SPREAD_MIN = 15;
const CONE_SPREAD_MAX = 45;

// Past this, a fix is worth apologising for in words. Chosen as the point
// where a position stops being useful for "am I at the right building" -- and
// a tower-trilaterated fix, the one that puts you ashore, is far beyond it.
const COARSE_ACCURACY_M = 200;

// Every marker in this component is drawn as a CSS dot rather than an image,
// and under MapLibre that is simply what a marker *is*: `new Marker({element})`
// takes a DOM node and positions it. The library's own default marker is an
// inline SVG, so there are no icon assets to vendor either way -- which is one
// of the small things that got easier in the swap. Leaflet's default marker
// was an <img> whose src resolved against the *page* URL, meaning
// /trips/<id>/locations/marker-icon.png in an SPA, answered with the app's own
// HTML and rendered as a broken image; sidestepping that is no longer a
// consideration.
//
// MapLibre adds `maplibregl-marker` and an anchor class to whatever element it
// is handed, and writes `transform` on it to place it, leaving everything else
// alone -- so the inline styles below survive and the tests can still select a
// marker by class.
function markerElement(category) {
  // A custom property rather than the hex, so that changing the map's
  // light/dark scheme restyles every marker already on the map without
  // rebuilding any of them. Markers survive setStyle deliberately (they are
  // DOM), so recreating them to recolour them would undo that.
  const color = markerColorVar(category);
  return styled(
    `display:block;width:1rem;height:1rem;border-radius:50%;background:${color};` +
      `border:2px solid var(--marker-ring);box-shadow:0 0 2px rgba(0,0,0,.5)`
  );
}

export function markerColorVar(category) {
  return Object.prototype.hasOwnProperty.call(CATEGORY_COLORS, category)
    ? `var(--marker-${category})`
    : "var(--marker-fallback)";
}

function pickMarkerElement() {
  return styled(
    `display:block;width:1.5rem;height:1.5rem;border-radius:50%;box-sizing:border-box;` +
      `border:4px solid var(--marker-pick);background:var(--marker-pick-fill);` +
      `box-shadow:0 0 3px rgba(0,0,0,.6)`
  );
}

// "You are here" has two shapes since Stage 42, so unlike the other markers
// it is styled by class (see .here in the stylesheet): which one shows is a
// selector on data-moving, and inline styles cannot express a selector. The
// outer element is the one MapLibre positions and writes `transform` on, so
// the arrow's own rotation lives on the inner SVG where it cannot collide
// with that.
//
// The arrow is a navigation chevron pointing north at rest; --course turns it.
// The cone behind both is the compass (Milestone 2), turned by --facing.
function hereMarkerElement() {
  const el = document.createElement("span");
  el.className = "here";
  el.innerHTML =
    `<span class="here__cone" aria-hidden="true"></span>` +
    `<span class="here__dot"></span>` +
    `<svg class="here__arrow" viewBox="0 0 24 24" aria-hidden="true" focusable="false">` +
    `<path d="M12 2.5 19.5 20.5 12 16.5 4.5 20.5Z"/></svg>`;
  return el;
}

function styled(css) {
  const el = document.createElement("span");
  el.style.cssText = css;
  return el;
}

// The accuracy ring, as a polygon.
//
// MapLibre has no metre-radius circle: a `circle` layer's radius is in screen
// pixels, so it would be a fixed dot that means a different distance at every
// zoom -- the opposite of what this ring is for. Approximating the circle in
// degrees instead keeps it correct on the ground at any zoom, with no
// expression arithmetic to re-derive when the position changes.
//
// 111320 is metres per degree of latitude; longitude is that scaled by
// cos(latitude), which is what keeps the ring circular rather than an ellipse
// away from the equator.
const RING_VERTICES = 64;

function accuracyRing(lat, lng, radiusMetres) {
  const dLat = radiusMetres / 111320;
  const dLng = radiusMetres / (111320 * Math.cos((lat * Math.PI) / 180));
  const ring = [];
  for (let i = 0; i <= RING_VERTICES; i++) {
    const theta = (i / RING_VERTICES) * 2 * Math.PI;
    ring.push([lng + dLng * Math.sin(theta), lat + dLat * Math.cos(theta)]);
  }
  return { type: "Feature", geometry: { type: "Polygon", coordinates: [ring] }, properties: {} };
}

const styles = `
  /* The marker palette, as custom properties so that a light/dark change
     restyles every marker and legend dot in place.

     Two sets rather than one, because a colour chosen to read on near-white
     paper does not read on near-black. Measured against each cartography's own
     background (liberty rgb(248,244,240), dark rgb(12,12,12)): the light
     values sit between 3.0:1 and 5.2:1 there, but "stay" and "transport" fall
     to 3.4:1 and 3.8:1 on the unmodified dark map - above the 3:1 floor for a
     graphical object, and visibly dimmer than their neighbours. The dark set
     is the same hues two steps lighter, which puts all six between 7:1 and
     11:1.

     Re-checked against liberty when it replaced positron as the light style.
     Its land is busier -- green parks, blue water -- so the tightest pairing
     is now a green "site" pin over a park (2.6:1) or a blue "transport" pin
     over water (2.8:1). Both stay legible because of the ring below rather
     than the fill: perceptually they are 58 and 55 dE apart, which is not a
     near miss. The ring is what does the work, and always was.

     "area", added later, was picked to the same rules: rose #e11d48 is 4.4:1 on
     liberty paper, and its dark twin #fb7185 is 7.3:1 on the dark map, where
     the light value would have been 4.1:1. Rose rather than another orange
     because #ea580c is already the pick marker, and rather than a teal
     because #0891b2 is already "you are here".

     "food", "event" and "shop" came next, and by then the easy hues were gone,
     so they were chosen by search rather than by eye: every Tailwind-ish
     candidate scored on contrast against both papers AND on CIELab distance to
     every other marker colour, the pick amber, the here cyan and the grey
     fallback. The winners are yellow #a16207, fuchsia #a21caf and a dark lime
     #3f6212, all between 4.5:1 and 6.5:1 on liberty -- better than the green
     "site" has ever had -- with dark twins #facc15, #d946ef and #a3e635.

     Two notes on that set. The first pass used the obvious amber #ca8a04 for
     food; it measured 2.7:1 on paper, below the 3:1 floor, which is exactly
     the kind of thing eyeballing a swatch does not catch. And the dark fuchsia
     is #d946ef rather than the lighter #e879f9: the lighter one is 7.9:1
     against 5.7:1, but it lands 27 dE from the violet "stay", and two pins
     that are the same colour are worse than one pin that is slightly dimmer.
     With #d946ef nothing new is closer than 39 dE to anything else, and the
     tightest pair in the dark set is still the original stay/transport at 31.

     Seven categories is about what this encoding holds. An eighth would have
     to be a different pin shape or an icon, not another hue.

     The legend dots use these too. They sit on app chrome rather than on the
     map, so they could have followed the app's theme instead - but a legend
     whose dot is a different colour from the marker it names is worse than a
     legend dot with less contrast behind it. */
  :host {
    --marker-site: ${CATEGORY_COLORS.site};
    --marker-stay: ${CATEGORY_COLORS.stay};
    --marker-transport: ${CATEGORY_COLORS.transport};
    --marker-area: ${CATEGORY_COLORS.area};
    --marker-food: ${CATEGORY_COLORS.food};
    --marker-event: ${CATEGORY_COLORS.event};
    --marker-shop: ${CATEGORY_COLORS.shop};
    --marker-fallback: ${FALLBACK_MARKER_COLOR};
    --marker-pick: ${PICK_MARKER_COLOR};
    --marker-pick-fill: rgba(255, 255, 255, 0.85);
    --marker-here: ${HERE_MARKER_COLOR};
    --marker-ring: #fff;
    /* The popup is dressed by the *map's* scheme, not the app's - same
       reasoning as the marker ring above it. It is a box sitting on the
       cartography, and MapLibre's own default (white paper, no colour set on
       the text) turned into an unreadable pane on a dark map: the title
       inherited the app's light --color-text through the shadow boundary and
       vanished, and so did the close button. */
    --popup-surface: #fff;
    --popup-text: #18181b;
    --popup-link: #1d4ed8;
    --popup-hover: rgba(0, 0, 0, 0.05);
  }
  :host([data-scheme="dark"]) {
    --marker-site: #22c55e;
    --marker-stay: #a78bfa;
    --marker-transport: #60a5fa;
    --marker-area: #fb7185;
    --marker-food: #facc15;
    --marker-event: #d946ef;
    --marker-shop: #a3e635;
    --marker-fallback: #a1a1aa;
    --marker-pick: #fb923c;
    --marker-pick-fill: rgba(24, 24, 27, 0.85);
    --marker-here: #22d3ee;
    /* The white ring that separates a marker from the map underneath has to
       become a dark one, or every marker wears a bright halo on a dark map. */
    --marker-ring: #18181b;
    /* Near-black paper, the app's dark surface one step darker so the popup
       reads as sitting *on* the map rather than being a piece of app chrome
       that landed there. */
    --popup-surface: #18181b;
    --popup-text: #fafafa;
    --popup-link: #60a5fa;
    --popup-hover: rgba(255, 255, 255, 0.08);
  }

  /* A column flex box, not a plain block: the map takes --map-height and
     everything stacked under it -- the two-finger hint's reserved line, the
     locate status, the credit, the legend -- takes its own, so adding one of
     those can never push the map past its height.

     The height lives on .map-wrap rather than on :host, and that is the whole
     layout model (Stage 38). The other way round -- :host fixed, .map-wrap
     flex: 1 -- means anything added below the map is subtracted *from* the
     map, which is how the legend ended up as an overlay at wide widths in the
     first place: it was the only way to add it without shrinking the thing it
     describes. With the height on the wrapper the component simply grows by
     whatever is stacked under the map, and the map is the size it says it is
     at every width. */
  :host {
    display: flex;
    flex-direction: column;
    height: auto;
    --map-height: 60vh;
    --map-min-height: 24rem;
  }
  :host([lat]) {
    --map-height: 16rem;
    --map-min-height: 0;
  }
  /* After :host([lat]) on purpose - equal specificity, so source order wins.
     A picker inside an editor card is the same size whether or not it has
     coordinates yet; without this it would be 16rem once a point exists and
     60vh before that, which is a form card that jumps on first click. */
  :host([pick]) {
    --map-height: 20rem;
    --map-min-height: 0;
  }
  /* .map-wrap *is* the map: the map element fills it, and the only other
     things inside it are laid over the cartography (the gesture hint, the
     locate button, the "nothing has a location yet" line). That is what lets
     the overlay below use a plain inset: 0.

     --map-min-height is a second property rather than a literal because the
     floor and the height have to move together: 24rem under a 20rem picker
     would silently inflate it to 24rem, which is the trap the old
     min-height: 0 overrides on :host existed to avoid. */
  /* Everything that goes fullscreen with the map: the map itself, the locate
     status and the credit - i.e. everything but the legend (Stage 44). The
     status has to come along because a locate error would otherwise happen
     off screen, and the credit because the tile licences ask for it to be
     visible wherever the map is. The legend stays behind on purpose: the
     filters are set before entering, and the fullscreen view is the map.

     A column flex box like :host, so outside fullscreen the wrapper changes
     nothing about how the three stack. */
  .map-stage {
    display: flex;
    flex-direction: column;
  }
  /* Two ways in, one look. :fullscreen is the Fullscreen API (the UA already
     makes the element fixed and viewport-sized, with !important); the
     attribute is the in-page fallback for browsers without element
     fullscreen - iPhone Safari - which has to do the positioning itself.
     z-index only matters for that one: the top layer is above everything
     anyway. It does not need to beat the tab panel's z-index: 0 cap
     (base.css), because nothing outside the panel is positioned above it.

     The padding keeps the status and credit off the screen edge, and the
     map's corner radius goes because a rounded rectangle filling the screen
     reads as a card that did not quite fit. */
  .map-stage:fullscreen,
  :host([data-fullscreen]) .map-stage {
    position: fixed;
    inset: 0;
    z-index: 2000;
    box-sizing: border-box;
    padding: 0 0 0.5rem;
    background: var(--color-bg, #fff);
  }
  .map-stage:fullscreen .map-wrap,
  :host([data-fullscreen]) .map-wrap {
    flex: 1;
    height: auto;
    min-height: 0;
  }
  .map-stage:fullscreen #map,
  :host([data-fullscreen]) #map {
    border-radius: 0;
  }
  .map-stage:fullscreen .locate-status,
  .map-stage:fullscreen .attribution,
  :host([data-fullscreen]) .locate-status,
  :host([data-fullscreen]) .attribution {
    /* The credit's max-width: 100% would otherwise be 100% plus this. */
    box-sizing: border-box;
    padding: 0 0.75rem;
  }
  .map-wrap {
    position: relative;
    height: var(--map-height);
    min-height: var(--map-min-height);
    flex: none;
    /* Kept from the Leaflet era, for a weaker reason than it had then.
       Leaflet parked internal helpers at very large offsets (measured at
       right=1825757) and briefly widened the document by 1636px mid-animation,
       which the UI suite caught as a page-level overflow. MapLibre draws into
       one canvas and does no such thing. This stays because the component
       still should not be able to widen the document whatever the library does
       -- the popup and the attribution control both live in here and are sized
       by their own content. */
    overflow: hidden;
  }
  #map {
    height: 100%;
    border-radius: 0.5rem;
  }
  /* The category filter, under the credit at the very bottom of the component
     (Stage 38). It used to sit over the map's top-right corner at wide widths
     and in the flow *above* the map on a phone, where at seven categories it
     stood 158px tall and left 42px of an 85vh map above the fold. It is a
     control people open rarely, and it was outranking the thing the page
     exists to show.

     A fieldset because it is a named group of checkboxes rather than a
     caption: over the map its meaning was positional, and down here it needs
     to say what it is. The UA border and padding go; the card border below is
     ours. */
  .legend {
    /* base.css's global box-sizing reset doesn't pierce this shadow root, and
       the border and padding here are added to a 100%-wide box - left at the
       browser default (content-box) they would push past the container's
       right edge. */
    box-sizing: border-box;
    margin: 0.5rem 0 0;
    background: var(--color-surface, #fff);
    border: 1px solid var(--color-border, #ccc);
    border-radius: 0.375rem;
    padding: 0.5rem 0.75rem;
    font-size: 0.8rem;
    display: flex;
    flex-direction: row;
    flex-wrap: wrap;
    gap: 0.25rem 0.75rem;
  }
  .legend > legend {
    /* A fieldset's legend is taken out of the flex flow by the UA, so it
       cannot be a flex item here; float: left with a full-width clear is the
       arrangement that puts it on its own line above the wrapped checkboxes
       in every engine. padding: 0 overrides the UA's inline padding, which
       would otherwise indent it past the checkboxes below. */
    float: left;
    width: 100%;
    padding: 0;
    margin-bottom: 0.25rem;
    font-weight: 600;
    color: var(--color-text-muted, #666);
  }
  .legend label {
    display: flex;
    align-items: center;
    gap: 0.4rem;
    cursor: pointer;
  }
  .dot {
    width: 0.6rem;
    height: 0.6rem;
    border-radius: 50%;
    display: inline-block;
  }
  /* The "nothing has a location yet" message, laid over the map.
     
     pointer-events: none is the whole point of this rule existing rather than
     the positioning living in an inline style, as it did from Caravel v1 until
     Stage 23: absolutely positioned across the entire wrapper and hit-testable,
     it swallowed every mouse event the map should have had, so a trip with no
     locations had a map that could not be dragged, clicked or interacted with
     at all. Nothing pointed at the message as the cause -- it reads as a label,
     not as a sheet of glass over the map. */
  .empty {
    position: absolute;
    inset: 0;
    margin: 0;
    padding: 1rem;
    color: var(--color-text-muted, #666);
    pointer-events: none;
  }
  /* The popup's two destinations - this location in Caravel, and Google Maps.
     Blocks rather than inline, so each is its own row under the title and the
     tap-target height below has something to apply to. */
  .popup-link {
    display: block;
  }
  /* The location's photo, when it has one (Stage 34). object-fit crops it to
     the box the same way .location-view__image does in base.css, so a portrait
     upload is cropped rather than allowed to set the popup's height.
     The ratio is 21/9 rather than that rule's 16/9 on purpose: a popup is a
     hover-sized card, not a page banner, and the letterbox keeps the picture
     from pushing the two links below the fold of a short map. At the 180px
     content width below that is a 180x77 strip.
     180px flat rather than width: 100%, which is not the same thing here: a
     popup is shrink-to-fit, so a percentage width follows the longest line of
     text and gave a picture that changed size with the length of the place's
     name. A fixed width makes it the widest thing in the box instead, so every
     photo is identical - and with popup()'s 200px maxWidth (10px of vendor
     padding a side) it is exactly the content width, not an inset picture with
     a gutter beside full-width links. max-width keeps it honest if that
     padding ever changes. Full-bleeding it past the padding would put the
     close button on top of the picture. */
  .popup-image {
    display: block;
    width: 180px;
    max-width: 100%;
    aspect-ratio: 21 / 9;
    object-fit: cover;
    border-radius: 0.25rem;
    margin: 0.375rem 0 0.25rem;
  }
  /* The popup itself, repainted from MapLibre's stylesheet in the map's own
     scheme (see --popup-* above). Everything here overrides a vendor rule from
     /js/vendor/maplibre/maplibre-gl.css, which the shadow root links *before*
     this block - so equal specificity is enough and is what these selectors
     deliberately match, tip included: each anchor variant paints its own
     border side, and missing one leaves a white arrow under a dark box.

     Colours are literals rather than --color-* tokens on purpose: the map's
     scheme is resolved from day/night at the map's centre and is independent
     of the app theme, so a popup borrowing app tokens would be light chrome on
     a dark map exactly as often as it was right. */
  .maplibregl-popup-content {
    background: var(--popup-surface);
    color: var(--popup-text);
    box-shadow: 0 1px 3px rgba(0, 0, 0, 0.3);
  }
  .maplibregl-popup-content a,
  .maplibregl-popup-content a:visited {
    color: var(--popup-link);
  }
  .maplibregl-popup-close-button {
    color: var(--popup-text);
  }
  .maplibregl-popup-close-button:hover {
    background-color: var(--popup-hover);
  }
  .maplibregl-popup-anchor-top .maplibregl-popup-tip,
  .maplibregl-popup-anchor-top-left .maplibregl-popup-tip,
  .maplibregl-popup-anchor-top-right .maplibregl-popup-tip {
    border-bottom-color: var(--popup-surface);
  }
  .maplibregl-popup-anchor-bottom .maplibregl-popup-tip,
  .maplibregl-popup-anchor-bottom-left .maplibregl-popup-tip,
  .maplibregl-popup-anchor-bottom-right .maplibregl-popup-tip {
    border-top-color: var(--popup-surface);
  }
  .maplibregl-popup-anchor-left .maplibregl-popup-tip {
    border-right-color: var(--popup-surface);
  }
  .maplibregl-popup-anchor-right .maplibregl-popup-tip {
    border-left-color: var(--popup-surface);
  }
  /* The locate control, overlaid on the map like a map control should be.
     Bottom left. There is no zoom control and the legend and the credit both
     live under the map now, so the only other corner in use is the fullscreen
     toggle's, top right. */
  .locate {
    position: absolute;
    bottom: 0.5rem;
    left: 0.5rem;
    z-index: 1000;
    display: flex;
    align-items: center;
    justify-content: center;
    gap: 0.4rem;
    /* A map control is a tap target like any other, at every width - not just
       under 640px. Nothing here is ever reachable with a mouse only. */
    min-width: var(--tap-min, 2.75rem);
    min-height: var(--tap-min, 2.75rem);
    padding: 0 0.6rem;
    border: 1px solid var(--color-border, #ccc);
    border-radius: 0.375rem;
    background: var(--color-surface, #fff);
    color: var(--color-text, #111);
    font: inherit;
    font-size: 0.8rem;
    cursor: pointer;
  }
  .locate[disabled] {
    cursor: not-allowed;
    opacity: 0.6;
  }
  /* Tracking. The same accent the filter menus use for "this control is
     currently doing something", and it tints the icon too because the sprite
     strokes with currentColor. This is the only indicator that the position is
     live, since the visible label cannot change without resizing the control
     (see setTracking). */
  .locate[data-tracking] {
    color: var(--color-accent, #2563eb);
    border-color: var(--color-accent, #2563eb);
  }
  /* The fullscreen toggle (Stage 44), top right and only on the trip map.
     The locate button's chrome and tap size, icon-only: the two words of
     "Show map fullscreen" would not fit beside the legendless corner at
     324px, and a maximise/minimise glyph is the convention every video player
     and map has taught. The name is on aria-label and title. */
  .fullscreen {
    position: absolute;
    top: 0.5rem;
    right: 0.5rem;
    z-index: 1000;
    display: flex;
    align-items: center;
    justify-content: center;
    min-width: var(--tap-min, 2.75rem);
    min-height: var(--tap-min, 2.75rem);
    padding: 0;
    border: 1px solid var(--color-border, #ccc);
    border-radius: 0.375rem;
    background: var(--color-surface, #fff);
    color: var(--color-text, #111);
    cursor: pointer;
  }
  .locate .icon,
  .fullscreen .icon {
    width: 1.1rem;
    height: 1.1rem;
    /* The sprite's symbols are strokes with no fill, so they are invisible
       without this - base.css says the same for .icon outside the shadow. */
    fill: none;
    stroke: currentColor;
    stroke-width: 2;
    stroke-linecap: round;
    stroke-linejoin: round;
  }
  /* "You are here" (hereMarkerElement). The box is the arrow's size so the
     anchor -- its centre -- is the position whichever shape is showing; the
     dot sits in the middle of it at the size it has always had. */
  .here {
    display: block;
    width: 1.5rem;
    height: 1.5rem;
    pointer-events: none;
  }
  .here__dot {
    position: absolute;
    inset: 0.25rem;
    border-radius: 50%;
    box-sizing: border-box;
    background: var(--marker-here);
    border: 3px solid var(--marker-ring);
    box-shadow: 0 0 4px rgba(0, 0, 0, 0.6);
  }
  .here__arrow {
    position: absolute;
    inset: 0;
    width: 100%;
    height: 100%;
    display: none;
    fill: var(--marker-here);
    stroke: var(--marker-ring);
    stroke-width: 2.5;
    stroke-linejoin: round;
    filter: drop-shadow(0 0 2px rgba(0, 0, 0, 0.6));
    /* No transition: easing from 350deg to 10deg would sweep the long way
       round, and the arrow is redrawn every few seconds anyway. */
    transform: rotate(calc(var(--course, 0) * 1deg));
  }
  /* The compass cone: which way the phone faces. A radial fade, cut to a
     wedge by a conic mask, so its width is one custom property -- --spread,
     degrees either side, which showFacing sets on the marker from the
     reported accuracy -- rather than a path to rebuild.
     Centred on the marker and drawn first, so the dot or arrow sits on top. */
  .here__cone {
    position: absolute;
    left: 50%;
    top: 50%;
    width: 7.5rem;
    height: 7.5rem;
    display: none;
    border-radius: 50%;
    background: radial-gradient(
      closest-side,
      color-mix(in srgb, var(--marker-here) 55%, transparent),
      transparent
    );
    mask-image: conic-gradient(
      from calc(var(--spread, 30) * -1deg),
      #000 calc(var(--spread, 30) * 2deg),
      transparent 0
    );
    transform: translate(-50%, -50%) rotate(calc(var(--facing, 0) * 1deg));
  }
  .here[data-facing] .here__cone {
    display: block;
  }
  .here[data-moving] .here__dot {
    display: none;
  }
  .here[data-moving] .here__arrow {
    display: block;
  }
  .locate-status {
    margin: 0.5rem 0 0;
    font-size: 0.8rem;
    color: var(--color-text-muted, #666);
  }
  .locate-status[hidden] {
    display: none;
  }

  /* The map credit, mounted under the map rather than over it (Stage 34).
     MapLibre's own control is instantiated by hand in initMap and its
     element appended here, so everything below is restyling a dependency's
     markup that has left the context its CSS was written for.

     max-width is the load-bearing one. Inside .map-wrap the credit was
     clipped by overflow: hidden when it outgrew the map; out here there is
     nothing to clip it, so without this it could widen the document -- which
     routes.spec.js sweeps for. */
  .attribution {
    max-width: 100%;
    margin: 0.5rem 0 0;
    /* .maplibregl-map sets font: 12px/20px Helvetica for everything inside
       the map container, and that inheritance is gone out here. Without an
       explicit size the credit would come out at the app's body size. */
    font-size: 0.7rem;
    line-height: 1.4;
    color: var(--color-text-muted, #666);
  }
  /* No background: the vendored hsla(0,0%,100%,.5) plate exists to keep the
     credit readable over cartography, and there is no cartography under it
     any more. Padding and the corner-position margins go for the same
     reason -- this is a line of text in the page's flow now. */
  .attribution .maplibregl-ctrl.maplibregl-ctrl-attrib {
    background: none;
    padding: 0;
    margin: 0;
    float: none;
  }
  /* The vendor gives these rgba(0, 0, 0, .75) and no underline, which is
     invisible on a dark page. Muted rather than accent-coloured, because a
     credit should read as a footnote and nearly every word in it is a link;
     underlined, because they still have to look like links. */
  .attribution .maplibregl-ctrl-attrib a,
  .attribution .maplibregl-ctrl-attrib a:visited {
    color: inherit;
    text-decoration: underline;
  }
  .attribution .maplibregl-ctrl-attrib a:hover {
    color: var(--color-text, #111);
  }
  /* Between the render and the map's construction, and permanently when the
     map could not be built at all, there is no control in here -- an empty
     box should not reserve a line of margin under the map. */
  .attribution:empty {
    display: none;
  }

  /* Rendered only on coarse pointers (see render()), where one-finger drag
     deliberately no longer pans the map. */
  /* The gesture hint, shown *when the gesture happens* rather than standing
     permanently under the map (Stage 23 Milestone 6). Two things it must never
     do: cover the map for longer than it takes to read, and eat the gesture it
     is describing - hence pointer-events: none, so a second wheel or a
     two-finger pan goes straight through it to the map underneath.

     role="status" and aria-live="polite" rather than an alert: it is an
     explanation of what just did not happen, and interrupting a screen reader
     mid-sentence for it would be worse than useless. */
  .gesture-hint {
    position: absolute;
    /* A plain inset: 0 since Stage 38. It used to be pinned to the bottom and
       given --map-height explicitly, because the legend sat inside this
       wrapper on a phone and an overlay across the whole wrapper would have
       dimmed it too; the legend is out of here now, so the wrapper and the map
       are the same box at every width. */
    inset: 0;
    /* base.css's global border-box reset does not pierce this shadow root -
       the same trap the legend rule records. Without this the 1rem padding is
       added to the height and the overlay stands 32px taller than the map. */
    box-sizing: border-box;
    z-index: 500;
    margin: 0;
    display: flex;
    align-items: center;
    justify-content: center;
    padding: 1rem;
    text-align: center;
    font-size: 0.9rem;
    font-weight: 500;
    color: #fff;
    background: rgba(0, 0, 0, 0.55);
    /* The overlay is not a control, so it does not take focus and does not
       need a focus ring; it is announced instead. */
    pointer-events: none;
    opacity: 1;
    transition: opacity 150ms ease-out;
  }
  /* MapLibre's own cooperative-gesture overlay, suppressed in favour of
     .gesture-hint above.
     Not a styling preference. Its screen is aria-hidden="true" with no
     role="status", so it announces nothing; it picks its wording from the user
     agent (Windows vs Mac help text) rather than from which modifier was
     actually pressed; and it runs on its own ~2s opacity transition rather
     than GESTURE_HINT_MS. The hint above is the app's accessibility contract
     and is what both gesture specs assert on, so having two overlays saying
     the same thing differently would be strictly worse than one. The handler
     itself stays on -- it is what makes a one-finger drag scroll the page. */
  .maplibregl-cooperative-gesture-screen {
    display: none;
  }
  .gesture-hint[hidden] {
    /* [hidden] alone would be display:none, which cannot transition. Keeping
       it in the layout and fading it out is what makes the appearance and the
       disappearance both readable. */
    display: flex;
    opacity: 0;
  }
  @media (prefers-reduced-motion: reduce) {
    .gesture-hint {
      transition: none;
    }
  }

  /* What is left of the mobile block, and it is worth saying what is *not*
     here any more (Stage 38). It used to restate the whole layout: height:
     auto on :host and all three mounts, a second flex column on .map-wrap,
     and the legend pulled out of the overlay into the flow with order: -1.
     All of that was undoing the desktop model, where :host carried the height
     and .map-wrap took the leftover. With the height on .map-wrap at every
     width and the legend out of the wrapper entirely, the only thing that
     genuinely differs on a phone is how tall the trip map is.

     The trip map is not capped, and that is deliberate. The cap carried a
     warning from Stage 13 that it was the fix for "the map swallows the page
     scroll", but that reasoning predates the coarse-pointer drag fix landing
     in the same milestone: a one-finger drag over the map is never consumed
     at any height (Stage 30 replaced the mechanism with MapLibre's
     cooperativeGestures, which keeps that property).

     85vh rather than 100vh: enough that the map is the screen, with a strip
     of page left at the bottom so it is visible that there is more below, and
     so the scroll position after a page load does not look like a full-bleed
     map with no context. That strip is where the credit and the legend now
     live. The other two mounts sit inside a page of other content and keep
     their own heights at every width. */
  @media (max-width: 640px) {
    :host {
      --map-height: 85vh;
      --map-min-height: 0;
    }
    /* The legend's category toggles are tap targets like any other, and at
       20px they were among the smallest in the app (Stage 09 Milestone 6).
       The label carries the height rather than the checkbox inside it, for
       the reason base.css's mobile block gives: a native checkbox blown up
       to 44px looks wrong, and clicking the label toggles it anyway.
       --tap-min is inherited through the shadow boundary (custom properties
       do pierce it, unlike the box-sizing reset noted above), with a literal
       fallback in case this component is ever used without base.css. */
    .legend label {
      min-height: var(--tap-min, 2.75rem);
    }
    /* Same reasoning, and it needs stating here rather than being caught by
       the sweep: routes.spec.js's tap-target check skips anything under a
       maplibregl-* class, and popup content is rendered inside
       .maplibregl-popup-content - so these links are invisible to it. They are
       asserted directly in map.spec.js instead. */
    .popup-link {
      min-height: var(--tap-min, 2.75rem);
      display: flex;
      align-items: center;
    }
  }
`;

class MapView extends HTMLElement {
  static get observedAttributes() {
    return ["trip-id", "lat", "lng", "marker-title", "marker-address", "marker-category", "pick", "locate"];
  }

  connectedCallback() {
    if (!this.shadowRoot) this.attachShadow({ mode: "open" });
    this._activeCategories = new Set(CATEGORIES);
    this._markers = [];
    this.load();
  }

  attributeChangedCallback() {
    if (this.isConnected) this.load();
  }

  // Leaflet needed no teardown worth the name. MapLibre holds a WebGL context
  // and a worker, and a browser will start killing the oldest contexts once
  // enough are live - so a map that is navigated away from has to say so.
  disconnectedCallback() {
    this._generation = (this._generation || 0) + 1;
    // A position watch outlives the element unless it is told not to, and a
    // GNSS watch running for a map nobody is looking at is a battery leak with
    // no upside. Cancelling settles the pending promise too, so the click
    // handler that is awaiting it can run its finally rather than hanging on
    // to this element forever.
    this._locateWatch?.cancel();
    this._locateWatch = null;
    this.stopCompass();
    // The intent has to go with it, or a visibilitychange after this element
    // is gone would start a watch for a map that no longer exists.
    this._trackingIntent = false;
    if (this._onVisibilityChange) {
      document.removeEventListener("visibilitychange", this._onVisibilityChange);
      this._onVisibilityChange = null;
    }
    this.destroyMap();
  }

  // Swap the cartography under a live map.
  //
  // setStyle rather than a rebuild, and that is the point of the whole
  // arrangement: it keeps the camera, keeps the markers (they are DOM), and
  // costs no refetch of the trip's locations. What it does *not* keep is
  // sources and layers, which is why applyOverlays exists and is bound to
  // style.load.
  async restyle() {
    const map = this._map;
    if (!map || !this._mapConfig) return;
    // Day/night is about where the *reader* is, not where the map is looking,
    // so this asks nothing of the viewport. map-theme.js owns the timer that
    // wakes everybody up at dusk.
    const next = resolveMapTheme();
    if (next === this._scheme) return;
    this._scheme = next;
    // Set before the style is fetched, so the markers recolour immediately
    // rather than a network round trip later.
    this.dataset.scheme = next;
    const generation = this._generation;
    const style = await buildStyle(this._mapConfig, next);
    if (generation !== this._generation || this._map !== map) return;
    // A newer restyle overtook this one while the style was fetching -- two
    // preference changes in quick succession, which day/night plus a theme
    // switch produces. Applying now would race it and could leave the map in a
    // cartography its own data-scheme denies.
    if (this._scheme !== next) return;
    // A style that will not load leaves the current one alone. Better a map in
    // the wrong scheme than no map at all, and the reader can flip back.
    if (!style) return;
    // The old style's sources and layers go with it, and the new one cannot
    // take any until it has parsed. The style.load above turns this back on
    // and rebuilds the overlays from component state.
    this._styleReady = false;
    map.setStyle(style, { diff: false });
  }

  // The map credit, mounted under the map instead of floating in its
  // bottom-right corner. On a 324px phone the corner version was clipped: the
  // library parks it in an absolutely positioned, shrink-to-fit box and floats
  // it right, nothing constrains its left edge, and .map-wrap cut off whatever
  // ran past the map. Under the map it is a line of text that wraps.
  //
  // Still MapLibre's own control, not a string of ours. onAdd returns a
  // detached element and only wires map events - the one place it reads the
  // map's geometry is the compact-mode check, and compact: false takes the
  // branch that does not care - so it works perfectly well outside the map
  // container, and it keeps the property that matters: the credit names the
  // sources that actually loaded, and re-reads them on styledata. That is why
  // Stage 30 Milestone 6 could delete CARAVEL_TILE_ATTRIBUTION, and writing
  // our own static credit here would hand the trap straight back.
  mountAttribution(maplibre, map) {
    const box = this.shadowRoot.querySelector(".attribution");
    if (!box) return;
    // compact: false keeps the credit visible rather than behind a toggle
    // under 640px, which is what every provider's terms actually ask for.
    //
    // No customAttribution: a style carries its own credit, inline on its
    // sources or in the TileJSON they point at, and MapLibre renders it.
    const ctrl = new maplibre.AttributionControl({ compact: false });
    box.appendChild(ctrl.onAdd(map));
    this._attribution = ctrl;
  }

  destroyMap() {
    this.teardownFullscreen();
    // Explicitly, and before the map goes: a control added by hand is not in
    // the map's own control list, so map.remove() does not tear it down and
    // its five map listeners would outlive the map they point at.
    if (this._attribution) {
      this._attribution.onRemove();
      this._attribution = null;
    }
    if (this._onMapThemeChanged) {
      eventBus.removeEventListener("map-theme-changed", this._onMapThemeChanged);
      this._onMapThemeChanged = null;
    }
    // Before the marker goes, so the arrow's staleness poll goes with it.
    this.clearCourse();
    this._map?.remove();
    this._map = null;
    this._markers = [];
    this._pickMarker = null;
    this._hereMarker = null;
  }

  // Shown instead of a map when a context cannot be had at all. Reuses the
  // .empty overlay the trip map already has for "nothing has a location yet":
  // both are the same situation from the reader's side - a rectangle where a
  // map should be, with a sentence saying why.
  showMapUnavailable(err) {
    console.warn("map unavailable", err);
    const wrap = this.shadowRoot.querySelector(".map-wrap");
    if (!wrap) return;
    wrap.querySelector(".empty")?.remove();
    // Nothing to make bigger, and it was never bound.
    wrap.querySelector('[data-action="fullscreen"]')?.remove();
    const p = document.createElement("p");
    p.className = "empty";
    p.textContent = t("map.unavailable");
    wrap.appendChild(p);
  }

  async load() {
    // For a parser-inserted element with preset attributes, attributeChangedCallback
    // can fire before connectedCallback attaches the shadow root (the node is
    // already isConnected, but the reaction that calls attachShadow hasn't run
    // yet) - bail out here and let connectedCallback's own load() call (which
    // runs after the shadow root exists) handle it instead.
    if (!this.shadowRoot) return;

    // Pick mode is settled before anything else, because it also reads
    // lat/lng - but as a starting point that is meant to be moved, rather
    // than as the fixed embed the single-marker branch below renders. It
    // never fetches anything.
    this._pick = this.hasAttribute("pick");
    if (this._pick) {
      // Once the map exists, a lat/lng change must only move the marker.
      // The editor rewrites those attributes as its coordinate fields are
      // typed in, and the inherited behaviour - a full load() and a fresh
      // innerHTML - would tear the map down and refetch tiles per keystroke.
      if (this._map) {
        this.syncPickMarker();
        return;
      }
      this._singleMarker = null;
      this._items = [];
      await this.render((this._generation = (this._generation || 0) + 1));
      return;
    }

    // connectedCallback and attributeChangedCallback both fire for the
    // initial attributes, so two loads can race; only the most recent one
    // is allowed to touch the DOM once its awaits resolve.
    const generation = (this._generation = (this._generation || 0) + 1);

    // A render builds a new map, so whatever the person did to the previous
    // one does not carry over.
    this._userMovedMap = false;
    // Separate from _userMovedMap, which is set once per render and never
    // reset. Following needs the narrower question -- has the reader taken the
    // camera *since the last press of the locate button* -- because a press is
    // exactly the gesture that hands it back.
    this._followCamera = false;

    const lat = this.getAttribute("lat");
    const lng = this.getAttribute("lng");
    if (lat != null && lng != null) {
      // Single-marker mode: an item's own location page embeds one point,
      // driven directly by attributes - no trip-wide fetch, no legend.
      this._singleMarker = {
        lat: Number(lat),
        lng: Number(lng),
        title: this.getAttribute("marker-title") || "",
        // Only for the outbound Google Maps link, which names the place rather
        // than dropping a pin on a coordinate. Nothing is drawn from it.
        address: this.getAttribute("marker-address") || "",
        category: this.getAttribute("marker-category") || "",
      };
      this._items = [];
      await this.render(generation);
      return;
    }
    this._singleMarker = null;

    const tripId = this.getAttribute("trip-id");
    if (!tripId) return;

    const items = await api.get(`/trips/${tripId}/map`);
    if (generation !== this._generation) return;
    this._items = items;
    await this.render(generation);
  }

  async render(generation) {
    // Before the innerHTML below detaches its container, not after: a map torn
    // down with its container already gone still releases the GL context, but
    // only by the library's good manners. Doing it in the right order means
    // not depending on them.
    this.destroyMap();

    // Cleared up front and set again on the map's own `load`: the library is
    // lazily imported inside this method, so between "the route's fetches have
    // settled" and "the map has laid itself out" there is a window in which
    // the component is on the page but half-built. The UI sweeps used to
    // measure that window under load and report un-sized map controls as
    // content overflowing .map-wrap. This is the component stating its own
    // readiness rather than the suite guessing - the small version of the
    // "ready signal" todo.md asks for app-wide.
    this.removeAttribute("data-ready");
    const single = this._singleMarker;
    // The legend filters trip-wide markers, of which pick mode has none - and
    // so does the "nothing has a location yet" line below.
    const chromeless = single || this._pick;
    this.shadowRoot.innerHTML = `
      <link rel="stylesheet" href="/js/vendor/maplibre/maplibre-gl.css" />
      <style>${styles}</style>
      <div class="map-stage">
        <div class="map-wrap">
          <div id="map"></div>
          <p class="gesture-hint" role="status" aria-live="polite" hidden></p>
          ${
            this.hasAttribute("locate")
              ? `<button type="button" class="locate" data-action="locate">${icon("locate-fixed")}<span>${t("map.locate.label")}</span></button>`
              : ""
          }
          ${
            this.hasAttribute("fullscreen-toggle")
              ? `<button type="button" class="fullscreen" data-action="fullscreen" aria-label="${t("map.fullscreen.enter")}" title="${t("map.fullscreen.enter")}">${icon("maximize")}</button>`
              : ""
          }
        </div>
        ${this.hasAttribute("locate") ? `<p class="locate-status" role="status" hidden></p>` : ""}
        <div class="attribution"></div>
      </div>
      ${
        chromeless
          ? ""
          : `<fieldset class="legend">
        <legend>${t("map.legend.label")}</legend>
        ${CATEGORIES
          .map(
            (cat) => `
            <label>
              <input type="checkbox" data-category="${cat}" checked />
              <span class="dot" style="background:${markerColorVar(cat)}"></span>
              ${t(`item.category.${cat}`)}
            </label>`
          )
          .join("")}
      </fieldset>`
      }
    `;

    if (!chromeless && !this._items.length) {
      this.shadowRoot.querySelector(".map-wrap").insertAdjacentHTML(
        "beforeend",
        `<p class="empty">${t("map.empty")}</p>`
      );
    }

    // Lazy-load MapLibre only when the map is actually shown, keeping it out
    // of the initial page weight for users who never open the Map tab. That
    // mattered with Leaflet and matters more now: the vendored library is
    // ~1.1MB across three modules against Leaflet's 440KB.
    //
    // The tile config is fetched alongside it rather than after: both are
    // needed before the first tile can be requested, so serialising them would
    // cost a round trip, and awaiting the config *after* constructing the map
    // would leave a constructed, tile-less map behind whenever this render is
    // superseded mid-fetch.
    const [maplibre, mapConfig] = await Promise.all([
      import("../vendor/maplibre/maplibre-gl.mjs"),
      loadMapConfig(),
    ]);
    if (generation !== this._generation) return;
    this._maplibre = maplibre;

    this._mapConfig = mapConfig;
    // Resolved before the map exists - the mode needs nothing from it - so the
    // first paint is already in the right scheme rather than flipping a moment
    // later.
    this._scheme = resolveMapTheme();
    this.dataset.scheme = this._scheme;
    const style = await buildStyle(mapConfig, this._scheme);
    if (generation !== this._generation) return;
    // Nothing to draw: the configured style would not load and there is no
    // fallback left. Say so rather than constructing a map with no cartography
    // in it, which looks like a bug in the app instead of in the setting.
    if (!style) {
      this.showMapUnavailable(new Error("map style unavailable"));
      this.setAttribute("data-ready", "");
      return;
    }

    const mapEl = this.shadowRoot.getElementById("map");

    let map;
    try {
      map = new maplibre.Map({
        container: mapEl,
        style,
        // MapLibre needs a view up front, where Leaflet tolerated a map with
        // none until setView. This is the same world view the empty trip map
        // and the coordinate-less picker settle on anyway.
        center: [0, 20],
        zoom: 2,
        // Off here, and added by hand below, because the credit does not live
        // over the map any more - see mountAttribution. Turning it off is not
        // dropping it.
        attributionControl: false,
        // The wheel is handled entirely in bindGestureGate below - see the
        // reasoning there. The library's own handler being off is what makes
        // the gesture deterministic: there is exactly one piece of code
        // deciding what a wheel does. This also sidesteps MapLibre's
        // cooperative-gesture bypass key, which is metaKey only on a Mac user
        // agent and so would drop Meta support everywhere else.
        scrollZoom: false,
        // "One finger scrolls the page, two fingers work the map" - the other
        // half of Stage 07's "the map swallows the page scroll" fix, and now a
        // supported option rather than something assembled from handler flags.
        // Note it is NOT dragPan: false, which was the obvious translation of
        // Leaflet's dragging: false and is wrong: MapLibre routes touch
        // panning of *any* finger count through dragPan, so turning it off
        // would take the two-finger pan with it. cooperativeGestures raises
        // the handler's minimum to two touches and puts touch-action on the
        // canvas container so the browser scrolls the page for the first one.
        // It applies on every pointer type, so the matchMedia check the old
        // code needed is gone; mouse dragging is unaffected.
        cooperativeGestures: true,
        // Rotation and pitch are capabilities Leaflet never had, on by default
        // here, and nothing in this app wants them: a tilted map with north
        // somewhere other than up is a new way to be lost, and two fingers on
        // a phone would reach for it constantly.
        dragRotate: false,
        touchPitch: false,
        pitchWithRotate: false,
        rollEnabled: false,
      });
    } catch (err) {
      // Failing here must still finish the render: data-ready is what every
      // route sweep in the UI suite blocks on, so leaving it off would turn a
      // missing GPU into a 15s timeout on every page with a map rather than
      // into a message.
      this.showMapUnavailable(err);
      this.setAttribute("data-ready", "");
      return;
    }

    // MapLibre v6 requires WebGL2, and this used to assume that meant the
    // constructor throws when it cannot get a context. It does not: it emits
    // an `error` event and hands back a half-built Map. Nothing catches that,
    // so the first property access below -- touchZoomRotate -- threw an
    // uncaught TypeError, render() died there, and the `load` handler that
    // sets data-ready was never even registered. The exact 15s-timeout-per-map
    // failure the catch above was written to prevent, and what had the CI ui
    // job red with 121 identical timeouts for five weeks (Stage 40).
    //
    // So the check is of what construction *returned* rather than of how it
    // finished. Handlers are assembled only once there is a context, which
    // makes their absence the honest signal; `error` cannot be used instead,
    // because it is emitted during construction, before there is an object to
    // subscribe to.
    if (!map.touchZoomRotate || !map.keyboard) {
      this.showMapUnavailable(new Error("no WebGL2 context"));
      this.setAttribute("data-ready", "");
      // Nothing holds a reference yet, so releasing it is this component's job
      // rather than destroyMap's - and a half-built map is still holding a
      // canvas and a resize observer. remove() is asked first and its failure
      // ignored, because a map that never finished being built does not
      // reliably survive being torn down either; emptying the container is
      // what actually guarantees the result, since a context-less canvas left
      // in place is a full-size rectangle drawing nothing, underneath the
      // sentence explaining why there is no map.
      try {
        map.remove();
      } catch {
        // Nothing useful to do about it here - the line below is the fallback.
      }
      mapEl.replaceChildren();
      return;
    }

    this._map = map;
    this.mountAttribution(maplibre, map);
    map.touchZoomRotate.disableRotation();
    map.keyboard.disableRotation();

    // Has the person moved this map themselves? syncPickMarker needs to know,
    // so that typing coordinates into the form can zoom to them without
    // yanking a map somebody has already positioned.
    //
    // Real input events rather than the map's own move/zoom events, and that
    // is the point: jumpTo fires those too, so a flag fed by them would be set
    // by our *own* recentring - including the world view this very component
    // takes when it opens with no coordinates, which would disable the
    // feature outright. mousedown covers dragging, the zoom buttons and
    // double-click zoom; wheel covers wheel zoom; touchstart covers pinch;
    // keydown covers the arrow and +/- keys. None of them can be raised by
    // jumpTo.
    const noteUserMovedMap = () => {
      this._userMovedMap = true;
      // Touching the map is how you say you want to look at something else.
      // The marker keeps updating -- that is a reading, and suppressing it
      // would be hiding information -- but the camera is yours from here.
      this._followCamera = false;
    };
    mapEl.addEventListener("mousedown", noteUserMovedMap);
    mapEl.addEventListener("touchstart", noteUserMovedMap, { passive: true });
    mapEl.addEventListener("keydown", noteUserMovedMap);
    // A wheel counts only when it is the zoom gesture. Since Milestone 6 a
    // plain wheel scrolls the *page* and deliberately leaves the map alone,
    // so treating it as "the person positioned this map" would be wrong -- and
    // would quietly undo Milestone 5, by stopping typed coordinates from
    // zooming for anyone who had scrolled the page past the map first.
    mapEl.addEventListener(
      "wheel",
      (e) => {
        if (e.ctrlKey || e.metaKey) noteUserMovedMap();
      },
      { passive: true }
    );

    this.bindGestureGate(mapEl);

    // Where this map is looking, announced so the page can remember it across
    // a navigation (trip-detail-page.js parks it in history.state, and hands
    // it back as initial-view). moveend covers zooming too - a zoom is a
    // camera move - and it fires for our own fitBounds as well, which is what
    // makes the very first view worth restoring and not just the ones the
    // person chose. Only the trip-wide map has a camera worth keeping: the
    // single-marker embed and pick mode each have exactly one view.
    if (!chromeless) {
      map.on("moveend", () => {
        if (generation !== this._generation) return;
        // A camera that follows the device would otherwise write a history
        // entry about once a second, since trip-detail-page.js turns every one
        // of these into a replaceState. Only follow-pans are swallowed: the
        // comment above is right that our own fitBounds is worth restoring,
        // and the initial fit on a locate still announces itself.
        const { lng, lat } = map.getCenter();
        const followed = this._followTarget;
        if (
          followed &&
          Math.abs(lng - followed.lng) < 1e-9 &&
          Math.abs(lat - followed.lat) < 1e-9 &&
          map.getZoom() === followed.zoom
        ) {
          return;
        }
        this.dispatchEvent(
          new CustomEvent("map-view-change", {
            bubbles: true,
            composed: true,
            detail: { lng, lat, zoom: map.getZoom() },
          })
        );
      });
    }

    this.plotMarkers();

    // Delegated, because popup DOM is built and destroyed on demand - there is
    // nothing to bind to until a marker is clicked.
    //
    // The router's own [data-link] interception cannot reach this. It is a
    // listener on `document` doing e.target.closest("[data-link]"), and a
    // click inside a shadow root retargets e.target to the <map-view>
    // host, so the link would never be found and the click would fall
    // through to a full page load. Hence a listener on this side of the
    // boundary, dispatching the same "item-open" contract location-card.js
    // uses, which trip-detail-page.js turns into a navigation.
    mapEl.addEventListener("click", (e) => {
      const link = e.target.closest?.("[data-item-id]");
      if (!link) return;
      // A real <a href> on purpose (the reason itinerary-tab.js gives for its
      // entry links): middle-click, open-in-new-tab, "copy link address" and
      // the status bar all come free with a link and none of them with a
      // button. So only a plain left-click is intercepted - a modified one
      // falls through to the browser, which resolves the route fine.
      if (e.button !== 0 || e.metaKey || e.ctrlKey || e.shiftKey || e.altKey) return;
      e.preventDefault();
      this.dispatchEvent(
        new CustomEvent("item-open", {
          bubbles: true,
          composed: true,
          detail: { itemId: link.dataset.itemId },
        })
      );
    });

    if (this._pick) {
      // Click anywhere to place or move the point. The map's own click event
      // rather than a DOM listener, because it hands over the coordinate
      // already projected - and it does not fire on a marker drag, which has
      // its own handler in syncPickMarker.
      map.on("click", (e) => this.emitPick(e.lngLat));
    }

    // The touch half of the gesture hint, now driven by the handler that
    // actually made the decision rather than by a touchmove listener guessing
    // at it: MapLibre raises this whenever cooperative gestures swallow a
    // one-finger pan. The wheel half stays in bindGestureGate, because
    // scrollZoom is off and so no wheel_zoom event is ever emitted.
    map.on("cooperativegestureprevented", (e) => {
      if (e.gestureType === "touch_pan") this.showGestureHint(t("map.twoFingerHint"));
    });

    if (this.hasAttribute("locate")) this.bindLocate();
    if (this.hasAttribute("fullscreen-toggle")) this.bindFullscreen();

    // Sources and layers do not survive a style change, so everything the
    // component draws through the style is re-added from here. Bound before
    // anything can restyle the map, and it fires for the initial style too -
    // harmless, since there is nothing to re-add until a position is taken.
    map.on("style.load", () => {
      this._styleReady = true;
      this.applyOverlays();
    });

    // The map is drawn from a style document, so it cannot follow data-theme
    // the way the rest of the app does - it has to be told. Removed again in
    // destroyMap, or a navigated-away map would go on being restyled.
    this._onMapThemeChanged = () => this.restyle();
    eventBus.addEventListener("map-theme-changed", this._onMapThemeChanged);

    // Ready means "the style is loaded and the first frame is on screen",
    // which is what `load` reports. That is a stronger claim than this
    // attribute used to make: it was set as soon as the map object existed,
    // and the honest version is what the UI suite wanted all along.
    map.on("load", () => {
      if (generation !== this._generation) return;
      this.setAttribute("data-ready", "");
    });

    if (!chromeless) {
      this.shadowRoot.querySelectorAll("[data-category]").forEach((cb) => {
        cb.addEventListener("change", () => {
          const cat = cb.getAttribute("data-category");
          if (cb.checked) this._activeCategories.add(cat);
          else this._activeCategories.delete(cat);
          // keepView: filtering is about what is on the map, not where the
          // map is looking. Until Stage 39 a toggle refit the bounds to
          // whatever was left, which meant unchecking a category could zoom
          // and pan the map out from under you - deliberate, and in practice
          // more disorienting than useful. The camera is now the person's.
          this.plotMarkers({ keepView: true });
        });
      });
    }
  }

  plotMarkers({ keepView = false } = {}) {
    const maplibre = this._maplibre;
    if (!maplibre || !this._map) return;
    this._markers.forEach((m) => m.remove());
    this._markers = [];

    if (this._pick) {
      this.syncPickMarker({ initial: true });
      return;
    }

    if (this._singleMarker) {
      const { lat, lng, title, address, category } = this._singleMarker;
      const mapsUrl = googleMapsUrl(lat, lng, title, address);
      const marker = new maplibre.Marker({ element: markerElement(category) })
        .setLngLat([lng, lat])
        .setPopup(
          this.popup(
            `<strong>${escapeHtml(title)}</strong><br/><a href="${escapeAttr(mapsUrl)}" target="_blank" rel="noopener">${t("map.viewOnGoogleMaps")}</a>`
          )
        )
        .addTo(this._map);
      this._markers.push(marker);
      // jumpTo, not a flyTo - this is the first and only view this embed ever
      // takes, so there is nothing to animate *from*; the animation only
      // produced a transient wide layout (see .map-wrap's overflow note).
      this._map.jumpTo({ center: [lng, lat], zoom: SINGLE_MARKER_ZOOM });
      return;
    }

    const visible = this._items.filter((item) => this._activeCategories.has(item.category));

    // The trip-wide popup's primary action is opening the location *in
    // Caravel*; Google Maps is the secondary one. Until Stage 13 Milestone 2
    // only the latter existed, so a marker was a dead end - the payload has
    // carried item.id all along (mapItemResponse in internal/httpapi/map.go),
    // the popup just never used it.
    //
    // The single-marker branch above deliberately gets no such link: it is
    // embedded on that location's own page, so it would link to itself.
    const tripId = this.getAttribute("trip-id");

    for (const item of visible) {
      const marker = new maplibre.Marker({ element: markerElement(item.category) })
        .setLngLat([item.lng, item.lat])
        .setPopup(
          this.popup(
            `<strong>${escapeHtml(item.title)}</strong>` +
              // alt="" on purpose: the title is right above it, so the photo
              // carries nothing a screen reader has not already been told.
              // loading="lazy" because setHTML builds the content up front for
              // every marker while only the opened popup is ever in the DOM.
              (item.image_url
                ? `<img class="popup-image" src="${escapeAttr(item.image_url)}" alt="" loading="lazy" />`
                : "") +
              `<a class="popup-link" data-item-id="${escapeAttr(item.id)}" href="${escapeAttr(`/trips/${tripId}/locations/${item.id}`)}">${t("map.openLocation")}</a>` +
              `<a class="popup-link" href="${escapeAttr(item.google_maps_url)}" target="_blank" rel="noopener">${t("map.viewOnGoogleMaps")}</a>`
          )
        )
        .addTo(this._map);
      this._markers.push(marker);
    }

    if (keepView) return;

    const initialView = this.consumeInitialView();
    if (initialView) {
      this._map.jumpTo(initialView);
    } else if (visible.length) {
      const bounds = visible.reduce(
        (b, i) => b.extend([i.lng, i.lat]),
        new maplibre.LngLatBounds([visible[0].lng, visible[0].lat], [visible[0].lng, visible[0].lat])
      );
      // padding is a number here, not Leaflet's [x, y] pair.
      //
      // maxZoom matters for a single marker (or several at the same spot): the
      // bounds are then zero-size, and an unbounded fit would zoom past the
      // deepest zoom the style has data for - an empty rectangle with one dot
      // on it. 14 is the same zoom the single-marker branch above picks
      // deliberately.
      this._map.fitBounds(bounds, { padding: 32, maxZoom: SINGLE_MARKER_ZOOM, animate: false });
    } else {
      this._map.jumpTo({ center: [0, 20], zoom: 2 });
    }
  }

  // A camera handed in by the page (see the map-view-change event above), used
  // once and then forgotten: it is the view to *open* at, not a view to snap
  // back to. A legend toggle re-plots with keepView, so it never reaches this
  // at all; consumed rather than re-read so that survives any other re-plot.
  consumeInitialView() {
    if (this._initialViewUsed) return null;
    this._initialViewUsed = true;
    const parts = (this.getAttribute("initial-view") || "").split(",").map(Number);
    if (parts.length !== 3 || parts.some((n) => !Number.isFinite(n))) return null;
    const [lng, lat, zoom] = parts;
    return { center: [lng, lat], zoom };
  }

  // Popups, with the two defaults this app disagrees with.
  //
  // focusAfterOpen: false because moving focus into a popup on open scrolls
  // the page to it, and these open from a marker the person just clicked -
  // they are already looking at it. setHTML does no sanitising, so escapeHtml
  // and escapeAttr at the call sites remain the only escaping, exactly as they
  // were under Leaflet's bindPopup.
  //
  // maxWidth is 200px against MapLibre's own 240: the vendor rule pads the
  // content box by 10px a side, so this is the ceiling that lets .popup-image
  // be exactly 180px wide without the box growing past it. A cap, not a width
  // - a popup with no photo and a short title still shrinks to its text. The
  // single-marker embed shares this and has no image; a long enough title
  // there is now wrapped 20px earlier than it used to be.
  popup(html) {
    return new this._maplibre.Popup({ offset: 12, focusAfterOpen: false, maxWidth: "200px" }).setHTML(html);
  }

  // Pick mode's one marker. Deliberately not part of plotMarkers' other two
  // branches: it is the only marker that can be moved, and it has to survive
  // a lat/lng change instead of being torn down and rebuilt with the map.
  syncPickMarker({ initial = false } = {}) {
    const maplibre = this._maplibre;
    if (!maplibre || !this._map) return;

    const lat = readCoordinate(this, "lat");
    const lng = readCoordinate(this, "lng");

    if (lat === null || lng === null) {
      // Nothing chosen yet, or the field was cleared. The world view is the
      // same "we don't know where you mean" view the empty trip map takes.
      this._pickMarker?.remove();
      this._pickMarker = null;
      if (initial) this._map.jumpTo({ center: [0, 20], zoom: 2 });
      return;
    }

    const creating = !this._pickMarker;
    if (this._pickMarker) {
      this._pickMarker.setLngLat([lng, lat]);
    } else {
      // The marker is a control, so it has to be reachable and describable
      // without a mouse. Leaflet had keyboard/title/alt options for that;
      // MapLibre adds no accessibility affordances to an element it is handed,
      // so they go on the element directly.
      const el = pickMarkerElement();
      el.tabIndex = 0;
      el.setAttribute("role", "button");
      el.setAttribute("aria-label", t("map.pickMarkerLabel"));
      el.title = t("map.pickMarkerLabel");
      this._pickMarker = new maplibre.Marker({ element: el, draggable: true })
        .setLngLat([lng, lat])
        .addTo(this._map);
      this._pickMarker.on("dragend", () => this.emitPick(this._pickMarker.getLngLat()));
    }

    // Three cases, and the middle one is the fix for a real complaint.
    //
    // The old rule was "recentre on the first render, and afterwards only if
    // the point has moved out of sight". That left a hole exactly where it
    // mattered: an editor opened with no coordinates sits at the world view,
    // zoom 2, where *every* point on Earth is inside the bounds. So filling in
    // a latitude and longitude moved a pin the person could not see, on a map
    // that never zoomed in.
    //
    // So the marker first appearing on a map the person has not moved
    // themselves is treated like a first render. Not a zoom-level test: a
    // deliberate zoom-out to 2 would look identical to the untouched world
    // view. And not on every update either -- once the marker exists, the
    // out-of-sight rule takes over, so typing does not yank the map with each
    // keystroke.
    //
    // Coordinates that came *from* the map - a click, a marker drag - never
    // reach the first branch, because placing them required a mousedown on
    // the map, which is exactly what noteUserMovedMap watches for. That is
    // deliberate: somebody who clicks a spot at the zoom they chose should
    // keep that zoom.
    if (initial || (creating && !this._userMovedMap)) {
      this._map.jumpTo({ center: [lng, lat], zoom: SINGLE_MARKER_ZOOM });
    } else if (!this._map.getBounds().contains([lng, lat])) {
      this._map.jumpTo({ center: [lng, lat], zoom: this._map.getZoom() });
    }
  }

  // The locate control. Failing honestly is the requirement here, not an
  // edge case: over plain HTTP on a phone the geolocation API exists and
  // simply never calls back, so an unguarded button would spin forever with
  // nothing to show. locateUnavailableReason() answers that up front and the
  // button is disabled with the reason spelled out instead.
  bindLocate() {
    const button = this.shadowRoot.querySelector('[data-action="locate"]');
    const status = this.shadowRoot.querySelector(".locate-status");

    const say = (key, params) => {
      status.textContent = key ? t(key, params) : "";
      status.hidden = !key;
    };

    const blocked = locateUnavailableReason();
    if (blocked) {
      button.disabled = true;
      say(locateErrorKey(blocked));
      return;
    }

    this._locateButton = button;
    this._locateSay = say;

    button.addEventListener("click", () => {
      // The compass first, and synchronously: iOS only considers the request
      // if it is made inside the gesture itself, and anything below can await.
      // A no-op everywhere that does not ask, and after the reader has
      // answered. Not for the picker, which never shows a direction.
      if (!this.hasAttribute("pick")) {
        requestCompassPermission().then((allowed) => {
          if (allowed && this._trackingIntent && !this._compassWatch) this.startCompass();
        });
      }
      // A press while tracking is "bring me back", not "start again": it takes
      // the camera back and recentres, and deliberately does not restart
      // acquisition or stop the watch. Leaving the map tab, reloading, or the
      // page going to the background are what end a watch -- see the recorded
      // trade-off in plans/stage-36.md.
      this._followCamera = true;
      if (this._locateWatch) {
        const fix = this._lastFix;
        // Re-fit rather than plain recentre, so a fix that has degraded since
        // the first one is framed for what it is now.
        if (fix) this.showPosition(fix.lat, fix.lng, fix.accuracy, "fit");
        return;
      }
      this._locateSettled = false;
      this.startLocateWatch(true);
    });

    // A watch running while the screen is off, or while the reader is in
    // another tab, is pure battery cost for something nobody can see. The
    // intent survives, so coming back resumes rather than requiring a press.
    this._onVisibilityChange = () => {
      if (!this._trackingIntent) return;
      if (document.hidden) {
        this._locateWatch?.cancel();
        this._locateWatch = null;
        this.stopCompass();
      } else if (!this._locateWatch) {
        // Not a fresh press: no "position-found" is announced, and because
        // _locateSettled is left alone the resumed watch's fixes are treated
        // as tracking rather than re-fitting the camera under the reader.
        this.startLocateWatch(false);
      }
    };
    document.addEventListener("visibilitychange", this._onVisibilityChange);
  }

  // One press, one watch. Also the resume path, which is why `fromPress`
  // exists: a resumed watch must not re-announce a position the page already
  // acted on.
  startLocateWatch(fromPress) {
    const button = this._locateButton;
    const say = this._locateSay;
    this._trackingIntent = true;

    // Only an acquisition says it is working. A resume already has a position
    // on screen, and blanking it back to "Finding your location" every time
    // the reader glanced at another tab would be noise.
    if (!this._locateSettled) {
      button.disabled = true;
      say("map.locate.searching");
    }

    // Continuous everywhere but the editor's picker. That one is recording
    // where a *place* is, not where you are going, so a live watch would fight
    // the reader's own pin drags -- and would re-announce a position over
    // coordinates they had since adjusted.
    const continuous = !this.hasAttribute("pick");

    const watch = watchPosition({
      continuous,
      // The picker blocks a form, so it does not hold out as long. It still
      // wants a *place*-grade fix and keeps refining towards one -- pressing
      // this while standing somewhere is the case it exists for -- but after
      // fifteen seconds it takes the best it has and says how good that is,
      // rather than leaving somebody watching a disabled button. The trip
      // map can afford the longer default because it keeps tracking
      // afterwards, so a slow lock costs nothing there.
      settleDeadlineMs: continuous ? undefined : 15000,
      onUpdate: (fix) => {
        this._lastFix = fix;
        this.showPosition(
          fix.lat,
          fix.lng,
          fix.accuracy,
          !this._locateSettled ? "fit" : this._followCamera ? "follow" : "none"
        );
        this.sayAccuracy(fix);
        if (continuous) this.showCourse(fix);
      },
      // Set here rather than off the promise, because it has to be true before
      // the *next* fix arrives and a promise continuation is a microtask away.
      onSettled: () => {
        this._locateSettled = true;
      },
    });
    this._locateWatch = watch;
    if (continuous) this.startCompass();

    watch.promise.then(
      (position) => {
        // Not say(null): a settled fix can still be a poor one, and going
        // quiet is what made a coast pin look like an answer in the first
        // place. sayAccuracy decides whether there is anything to admit.
        this.sayAccuracy(position);
        button.disabled = false;
        this.setTracking(continuous);
        if (!fromPress) return;
        // The page decides what a position *means*: the trip map only shows
        // it, while the editor's picker takes it as the point being set. Same
        // control, one event, no second button to keep in step. Fired once, on
        // the settled fix, so the editor is never handed a coordinate that is
        // about to be improved on.
        this.dispatchEvent(
          new CustomEvent("position-found", {
            bubbles: true,
            composed: true,
            detail: position,
          })
        );
      },
      (err) => {
        if (this._locateWatch === watch) this._locateWatch = null;
        button.disabled = false;
        // Cancelled is not a failure and has no message: it means this element
        // went away, or the page was backgrounded. Saying "your location could
        // not be determined" for either would be a lie.
        if (err.reason === LOCATE_CANCELLED) return;
        this._trackingIntent = false;
        this.stopCompass();
        this.setTracking(false);
        // Denied, unavailable and timed out are three different situations
        // and get three different sentences; anything else would tell the
        // user nothing about what to try next.
        say(locateErrorKey(err.reason || "unavailable"));
      }
    );
  }

  // How good the reading is, in words.
  //
  // The ring says the same thing in pixels and is the better answer when it is
  // visible -- but it is a faint tint over whatever the map is showing, and
  // over open water that is faint teal on blue. A sentence does not depend on
  // the cartography underneath it.
  //
  // Nothing is said for a good fix. A permanent line restating the obvious is
  // noise, and noise is what stops people reading the line that matters.
  sayAccuracy(fix) {
    const say = this._locateSay;
    if (!say) return;
    const distance = formatDistance(fix?.accuracy);
    if (!distance) {
      if (this._locateSettled) say(null);
      return;
    }
    if (!this._locateSettled) {
      say("map.locate.refining", { distance });
      return;
    }
    say(fix.accuracy > COARSE_ACCURACY_M ? "map.locate.coarse" : null, { distance });
  }

  // The button is the only indicator that tracking is on, so it carries both
  // halves: a tint (the same accent the filter menus use for "this control is
  // doing something"), and an accessible name that says what a press will now
  // do.
  //
  // The *visible* label deliberately does not change. It is real text rather
  // than a tooltip, so swapping it for a longer sentence would resize a map
  // control mid-use -- and at 324px in German there is no room for that.
  // aria-label supersedes it for a screen reader, and still contains it.
  setTracking(on) {
    const button = this._locateButton;
    if (!button) return;
    if (on) {
      button.setAttribute("data-tracking", "");
      button.setAttribute("aria-label", t("map.locate.recentre"));
      button.title = t("map.locate.recentre");
    } else {
      button.removeAttribute("data-tracking");
      button.removeAttribute("aria-label");
      button.removeAttribute("title");
    }
  }

  // "You are here": the point plus the accuracy the browser reported, drawn
  // as a ring. The ring is not decoration - a 2km fix and a 5m fix look
  // identical without it, and only one of them is worth acting on.
  //
  // `camera` is what the three phases come down to on screen:
  //
  //   "fit"    - frame the accuracy ring. Used for every fix while the answer
  //              is still being acquired, so a wrong marker is honestly framed
  //              the moment it appears and the framing tightens as it improves.
  //   "follow" - pan to the position at whatever zoom is already set. Used
  //              while tracking, because by then the zoom is the one the
  //              initial fit chose and re-fitting on every fix would make the
  //              map breathe in and out as the accuracy wobbled.
  //   "none"   - draw and do not touch the camera. Used once the reader has
  //              panned or zoomed themselves.
  showPosition(lat, lng, accuracy, camera = "fit") {
    const maplibre = this._maplibre;
    if (!maplibre || !this._map) return;

    // Not draggable, unlike the pick marker: this is a reading, not a choice,
    // so dragging it would claim to move the device. A plain element takes no
    // focus and no pointer events of its own, so Leaflet's interactive: false
    // and keyboard: false have nothing to translate to.
    //
    // Created once and moved after (Stage 42). It used to be rebuilt on every
    // fix, which was harmless while it was a dot with nothing on it; now the
    // element carries the direction it is showing, and a rebuild would drop
    // it. destroyMap() nulls it, so a rebuilt map still gets a fresh one.
    if (this._hereMarker) {
      this._hereMarker.setLngLat([lng, lat]);
    } else {
      this._hereMarker = new maplibre.Marker({ element: hereMarkerElement() })
        .setLngLat([lng, lat])
        .addTo(this._map);
      // The compass can answer before the first fix does.
      this.showFacing(this._facing, this._facingAccuracy);
    }

    this._hereAccuracy = Number.isFinite(accuracy) && accuracy > 0 ? accuracy : null;
    this._hereRingAt = this._hereAccuracy ? { lat, lng } : null;
    this.applyOverlays();

    if (camera === "none") return;
    if (camera === "follow") {
      // Remember where following put the camera, and let the moveend handler
      // recognise it. Two simpler versions were tried and both were flaky
      // under load, for the same underlying reason -- moveend is not promised
      // to arrive synchronously, or exactly once:
      //
      //   a flag cleared on setTimeout(0) - the timer could win the race and
      //   let a follow-pan through;
      //   a count of moves to swallow - it only stays balanced if every jumpTo
      //   produces exactly one moveend, and an extra or a coalesced one leaves
      //   the count wrong from then on.
      //
      // Comparing the camera instead is idempotent: any number of moveends at
      // the follow target are swallowed, none anywhere else is, and there is
      // no state to get out of step. A reader panning to precisely this centre
      // and zoom by hand would be missed; that is not reachable in practice.
      const zoom = this._map.getZoom();
      this._followTarget = { lng, lat, zoom };
      this._map.jumpTo({ center: [lng, lat], zoom });
      return;
    }
    this._map.jumpTo({ center: [lng, lat], zoom: this.zoomForAccuracy(lat, lng) });
  }

  // Dot or arrow, and which way the arrow points (Stage 42).
  //
  // Only the continuous watch calls this -- the editor's picker records where a
  // place is, not where you are going -- so a pick map never shows an arrow.
  // The direction is the platform's direction of *travel*, from GNSS: it says
  // nothing about which way the phone is facing, and is only worth showing
  // once you are moving fast enough for it to mean something. See
  // MOVING_ON_MPS for why there are two thresholds, and COURSE_HOLD_MS for
  // why one fix that disagrees does not end an arrow.
  showCourse(fix) {
    const el = this._hereMarker?.getElement();
    if (!el) return;

    const speed = fix?.speed;
    const moving =
      Number.isFinite(fix?.heading) &&
      Number.isFinite(speed) &&
      speed > (this._moving ? MOVING_OFF_MPS : MOVING_ON_MPS);

    if (!moving) {
      // Held, not dropped: the arrow keeps its last angle and the poll below
      // ends it once nothing has supported it for COURSE_HOLD_MS. Checked
      // here too, so a held arrow does not wait for the next poll tick.
      if (this._moving && Date.now() - this._courseAt >= COURSE_HOLD_MS) this.clearCourse();
      return;
    }

    this._moving = true;
    this._courseAt = Date.now();
    // Relative to the map's bearing, which is always 0 today -- rotation is
    // disabled -- but an arrow that ignored it would point the wrong way the
    // day it is not.
    const angle = (((fix.heading - this._map.getBearing()) % 360) + 360) % 360;
    el.style.setProperty("--course", String(angle));
    el.dataset.course = String(Math.round(fix.heading));
    el.setAttribute("data-moving", "");

    // Polled rather than a single timeout per fix, and against Date.now rather
    // than elapsed timer time: a phone suspends timers with the screen, and
    // what matters on waking is how old the last direction is, not how many
    // ticks were missed. Runs only while an arrow is showing.
    if (!this._courseTimer) {
      this._courseTimer = setInterval(() => {
        if (Date.now() - this._courseAt >= COURSE_HOLD_MS) this.clearCourse();
      }, COURSE_STALE_CHECK_MS);
    }
  }

  // The compass, alongside the position watch and on the same lifecycle:
  // started with tracking, stopped when the page is hidden, the watch fails or
  // the element goes. Only where it could receive anything -- on iOS that is
  // after the reader said yes, which the click handler picks up.
  startCompass() {
    if (this._compassWatch || !compassAllowed()) return;
    this._compassWatch = watchCompass({ onUpdate: (deg, accuracy) => this.showFacing(deg, accuracy) });
  }

  stopCompass() {
    this._compassWatch?.cancel();
    this._compassWatch = null;
    // A paused compass is not a reading, and the cone must not outlive it.
    this.showFacing(null);
  }

  // The cone: which way the phone faces, or nothing. Kept in _facing as well
  // as on the element, so a marker created after the compass answered still
  // gets it.
  showFacing(deg, accuracy = null) {
    this._facing = Number.isFinite(deg) ? deg : null;
    this._facingAccuracy = Number.isFinite(accuracy) ? accuracy : null;
    const el = this._hereMarker?.getElement();
    if (!el) return;
    if (this._facing === null) {
      el.removeAttribute("data-facing");
      return;
    }
    // Relative to the map's bearing, as the arrow is. See showCourse.
    const angle = (((this._facing - this._map.getBearing()) % 360) + 360) % 360;
    el.style.setProperty("--facing", String(angle));
    el.dataset.facing = String(Math.round(this._facing) % 360);
    const spread =
      this._facingAccuracy === null
        ? CONE_SPREAD_DEFAULT
        : Math.min(CONE_SPREAD_MAX, Math.max(CONE_SPREAD_MIN, this._facingAccuracy));
    el.style.setProperty("--spread", String(spread));
    el.dataset.spread = String(Math.round(spread));
  }

  // Back to the dot: stopped, no direction, or no news for too long.
  clearCourse() {
    this._moving = false;
    clearInterval(this._courseTimer);
    this._courseTimer = null;
    const el = this._hereMarker?.getElement();
    if (!el) return;
    el.removeAttribute("data-moving");
    delete el.dataset.course;
  }

  // How far to zoom in on a fix: close, but never closer than the fix deserves.
  //
  // The ring exists so that a 2km reading and a 5m reading do not look alike,
  // and a fixed zoom defeated it completely -- at HERE_ZOOM a kilometre-scale
  // ring is wider than the screen, so the one thing on the map that admitted
  // the position was a guess was the one thing not on the map. Fitting the
  // ring instead means a coarse fix arrives already zoomed out far enough to
  // see how coarse it is. Nothing about the geometry changed; it is the camera
  // that was lying.
  //
  // HERE_ZOOM stays the ceiling, so a good fix is framed exactly as before.
  zoomForAccuracy(lat, lng) {
    const radius = this._hereAccuracy;
    if (!radius) return HERE_ZOOM;

    // The ring's own geometry rather than a second piece of arithmetic that
    // could disagree with it: accuracyRing() already knows that a degree of
    // longitude is a different distance at every latitude.
    const ring = accuracyRing(lat, lng, radius).geometry.coordinates[0];
    const lngs = ring.map((c) => c[0]);
    const lats = ring.map((c) => c[1]);
    const bounds = [
      [Math.min(...lngs), Math.min(...lats)],
      [Math.max(...lngs), Math.max(...lats)],
    ];

    // cameraForBounds answers undefined for a map that has not been laid out
    // yet, which is a real state here -- the locate button can be pressed
    // before the first style has loaded. Falling back to the old behaviour is
    // right: an un-laid-out map has no viewport to fit anything to.
    const camera = this._map.cameraForBounds(bounds, { padding: ACCURACY_FIT_PADDING });
    return Number.isFinite(camera?.zoom) ? Math.min(camera.zoom, HERE_ZOOM) : HERE_ZOOM;
  }

  // Everything the *style* owns, (re-)built from the component's own state.
  //
  // This indirection is the whole point rather than tidiness. Markers and
  // popups are DOM and belong to the map, so they outlive anything done to the
  // style; sources and layers belong to the style and are destroyed outright
  // by setStyle(). So the accuracy ring cannot be drawn once and forgotten -
  // it has to be reconstructible from state the component holds, and something
  // has to call for it again afterwards. render() binds this to the map's
  // `style.load`, which fires for the first style and for every replacement,
  // so a restyle re-adds whatever was on the map without the caller knowing a
  // restyle happened. Milestone 5 swaps the style on every light/dark change
  // and needs exactly that.
  //
  // Safe to call at any time: with no position taken it removes what is not
  // there and returns.
  applyOverlays() {
    const map = this._map;
    if (!map) return;
    // Sources and layers cannot be added to a style that is still parsing --
    // MapLibre throws "Style is not done loading" outright. Nothing is lost by
    // waiting: this method is bound to `style.load` (see render), so the ring
    // is rebuilt from component state the moment the style is ready, which is
    // the same mechanism that survives a restyle.
    //
    // Reachable in ordinary use since Stage 36: a fix can now arrive within a
    // second or two of the press, so pressing locate as the page settles can
    // land a position before the first style has parsed.
    //
    // Our own flag rather than map.isStyleLoaded(), which is a stricter
    // question than the one being asked: it also requires every *source* to
    // have loaded, so it answers false during the very `style.load` callback
    // that exists to re-add this ring. Using it here silently stopped the ring
    // coming back after a light/dark swap.
    if (!this._styleReady) return;

    const at = this._hereRingAt;
    const radius = this._hereAccuracy;
    if (!at || !radius) {
      this._hereRing = null;
      this.removeAccuracyRing();
      return;
    }

    // Recomputed rather than cached, because the ring is in degrees and a
    // degree of longitude is a different distance at every latitude - so the
    // geometry is a function of *where* it is, not only how big it is.
    //
    // Kept on the component as well as handed to the source: MapLibre offers
    // no public read-back of a GeoJSON source's data, and the accuracy test
    // needs the geometry that is actually on the map rather than the number it
    // was derived from.
    const data = (this._hereRing = accuracyRing(at.lat, at.lng, radius));
    const existing = map.getSource(ACCURACY_SOURCE);
    if (existing) {
      existing.setData(data);
      return;
    }
    map.addSource(ACCURACY_SOURCE, { type: "geojson", data });
    map.addLayer({
      id: `${ACCURACY_SOURCE}-fill`,
      type: "fill",
      source: ACCURACY_SOURCE,
      paint: { "fill-color": HERE_MARKER_COLOR, "fill-opacity": 0.12 },
    });
    map.addLayer({
      id: `${ACCURACY_SOURCE}-line`,
      type: "line",
      source: ACCURACY_SOURCE,
      paint: { "line-color": HERE_MARKER_COLOR, "line-width": 1 },
    });
  }

  // The fullscreen toggle (Stage 44). Our own button rather than MapLibre's
  // FullscreenControl: that one brings a background-image icon, English-only
  // strings and its own control box, next to a locate button that has none of
  // those. What it would have done for us is small enough to do here.
  //
  // The element that goes fullscreen is .map-stage, not the map: see the CSS
  // for why the locate status and the credit come along and the legend does
  // not.
  bindFullscreen() {
    const button = this.shadowRoot.querySelector('[data-action="fullscreen"]');
    const stage = this.shadowRoot.querySelector(".map-stage");
    if (!button || !stage) return;
    button.addEventListener("click", () => {
      if (this.hasAttribute("data-fullscreen")) this.exitFullscreen();
      else this.enterFullscreen();
    });
    // The Fullscreen API's state is the browser's, not ours: Esc, Android's
    // back and F11-style exits all end it without asking. So the button reads
    // the state from the event rather than assuming its own click did it.
    // fullscreenElement on the *shadow root*, because the document's is
    // retargeted to the <map-view> host.
    this._onFullscreenChange = () => {
      if (this._pseudoFullscreen) return;
      this.applyFullscreenState(this.shadowRoot?.fullscreenElement === stage);
    };
    document.addEventListener("fullscreenchange", this._onFullscreenChange);
  }

  enterFullscreen() {
    const stage = this.shadowRoot.querySelector(".map-stage");
    if (!stage) return;
    if (document.fullscreenEnabled && stage.requestFullscreen) {
      // A refusal (a permissions policy, an iframe without allowfullscreen)
      // still gets the reader a big map, just with the browser's bars.
      stage.requestFullscreen().catch(() => this.enterPseudoFullscreen());
      return;
    }
    this.enterPseudoFullscreen();
  }

  // The fallback for browsers without element fullscreen, which in practice
  // means iPhone Safari: the stage is fixed over the viewport by CSS instead.
  // It gets Esc by hand, since only the real thing has it for free, and the
  // page underneath is kept from scrolling.
  enterPseudoFullscreen() {
    this._pseudoFullscreen = true;
    this._onFullscreenKey = (e) => {
      if (e.key === "Escape") this.exitFullscreen();
    };
    document.addEventListener("keydown", this._onFullscreenKey);
    this._rootOverflow = document.documentElement.style.overflow;
    document.documentElement.style.overflow = "hidden";
    this.applyFullscreenState(true);
  }

  exitFullscreen() {
    if (this._pseudoFullscreen) {
      this.leavePseudoFullscreen();
      this.applyFullscreenState(false);
      return;
    }
    const stage = this.shadowRoot?.querySelector(".map-stage");
    if (stage && this.shadowRoot.fullscreenElement === stage) {
      document.exitFullscreen().catch(() => {});
    }
  }

  leavePseudoFullscreen() {
    this._pseudoFullscreen = false;
    if (this._onFullscreenKey) {
      document.removeEventListener("keydown", this._onFullscreenKey);
      this._onFullscreenKey = null;
    }
    document.documentElement.style.overflow = this._rootOverflow ?? "";
  }

  // The one place both ways in and out land, so the button, the host
  // attribute and the map's size cannot disagree.
  applyFullscreenState(on) {
    if (on === this.hasAttribute("data-fullscreen")) return;
    // The stage leaves the flow while fullscreen, which would collapse the
    // host to just its legend: the page would get shorter under the reader and
    // exiting would land them at a different scroll position. Holding the
    // host at its current height keeps the page where it was.
    if (on) this.style.minHeight = `${this.offsetHeight}px`;
    else this.style.minHeight = "";
    this.toggleAttribute("data-fullscreen", on);
    const button = this.shadowRoot?.querySelector('[data-action="fullscreen"]');
    if (button) {
      const label = t(on ? "map.fullscreen.exit" : "map.fullscreen.enter");
      button.setAttribute("aria-label", label);
      button.title = label;
      button.innerHTML = icon(on ? "minimize" : "maximize");
    }
    // The map's ResizeObserver would catch the new size a frame later; asking
    // now saves a frame of stretched canvas.
    this._map?.resize();
  }

  // Called from destroyMap, so a re-render or a navigation never leaves the
  // page locked or a listener behind. Native fullscreen also ends by itself
  // when its element leaves the DOM, but only once the element has actually
  // gone, and destroyMap runs first.
  teardownFullscreen() {
    if (this._onFullscreenChange) {
      document.removeEventListener("fullscreenchange", this._onFullscreenChange);
      this._onFullscreenChange = null;
    }
    if (this._pseudoFullscreen) this.leavePseudoFullscreen();
    const stage = this.shadowRoot?.querySelector(".map-stage");
    if (stage && this.shadowRoot.fullscreenElement === stage) {
      document.exitFullscreen().catch(() => {});
    }
    this.style.minHeight = "";
    this.removeAttribute("data-fullscreen");
  }

  removeAccuracyRing() {
    const map = this._map;
    if (!map || !map.getSource(ACCURACY_SOURCE)) return;
    for (const suffix of ["-fill", "-line"]) {
      const id = `${ACCURACY_SOURCE}${suffix}`;
      if (map.getLayer(id)) map.removeLayer(id);
    }
    map.removeSource(ACCURACY_SOURCE);
  }

  // A scroll that happens to pass under the cursor must not zoom the map.
  //
  // A stock scroll-wheel handler zooms on *any* wheel event, so a page scroll
  // that crossed the map turned into a zoom - and on a map that is most of the
  // screen, that is most scrolls. The embedded-Google-Maps
  // convention is the fix: the wheel zooms only while Ctrl (or Meta, for the
  // Mac) is held, and a plain wheel says so and scrolls the page.
  //
  // MapLibre has its own version of this (cooperativeGestures also gates the
  // wheel), and it is deliberately not used for the wheel half. Its bypass key
  // is metaKey only when the user agent says Mac, so Meta would stop working
  // everywhere else; and handing the wheel back to a library is re-opening the
  // failure that produced zoomByWheel below. cooperativeGestures stays on for
  // the *touch* half, where it is the only supported way to get one-finger
  // page scroll with two-finger pan.
  //
  // The listener sits on .map-wrap, in the capture phase, and that placement
  // is load-bearing. The library registers its own listeners on the map
  // container itself, and on a shared target the capture/bubble distinction
  // does not decide the order - registration order does. Capturing on the
  // *parent* means this runs first whatever the library did.
  bindGestureGate(mapEl) {
    const wrap = this.shadowRoot.querySelector(".map-wrap");
    if (!wrap) return;

    wrap.addEventListener(
      "wheel",
      (e) => {
        if (e.ctrlKey || e.metaKey) {
          // Ctrl + wheel is bound to page zoom in every browser, so this has
          // to be cancelled here, in the capture phase, before anything else
          // looks at it.
          e.preventDefault();
          this.zoomByWheel(e);
          return;
        }
        // A plain wheel is deliberately *not* prevented: the whole point is
        // that the page scrolls the way it would anywhere else. Only the map
        // is kept from seeing it.
        e.stopPropagation();
        this.showGestureHint(t("map.ctrlZoomHint"));
      },
      // passive: false explicitly. A wheel listener is one of the types
      // browsers may treat as passive by default, and a passive listener's
      // preventDefault is ignored - silently, apart from a console warning.
      { capture: true, passive: false }
    );

    // The touch half used to live here as a touchmove listener guarded by a
    // matchMedia check. It now hangs off the map's own
    // cooperativegestureprevented event in render(), which fires from the code
    // that actually swallowed the pan - no guessing at pointer type, and no
    // risk of the hint and the behaviour disagreeing.
  }

  // Zoom the map for a Ctrl-held wheel.
  //
  // The library's own scroll-wheel handler is switched off and this replaces
  // it, after two rounds of the gesture not working on the reporter's machine
  // while working everywhere it could be measured. Carried over to MapLibre
  // unchanged, deliberately: its wheel handler uses the same
  // accumulate-normalise-sigmoid shape Leaflet's did, so the swap is no reason
  // to believe the original problem would not come back.
  //
  // Be honest about what is and is not known here. The failure was real and
  // reproducible for them - Ctrl + wheel zoomed nothing, in both the version
  // that passed the event to Leaflet untouched and the version that only
  // cancelled the browser default first - and it could not be reproduced here
  // at all: driven through Playwright, Leaflet zooms correctly for line,
  // pixel and page deltas, with or without a horizontal component. So the
  // mechanism inside Leaflet is *unknown*. Two theories were checked against
  // the running code and both were wrong: getWheelDelta does not discard an
  // event carrying deltaX (the deltaY branches are tested first), and
  // _performZoom's sigmoid cannot round a nonzero delta to no zoom while
  // zoomSnap is 1. Do not repeat either as the explanation.
  //
  // What this does instead is shrink the surface. Leaflet decides how far to
  // zoom from an accumulated, normalised, sigmoid-shaped magnitude; this uses
  // only the *direction*, which every device and every deltaMode agrees on.
  // Magnitude decides pacing and nothing else.
  // Deltas are normalised to pixels with Leaflet's own factors, accumulated,
  // and every 60px is one zoom level -- so one notch of a mouse wheel is one
  // level, and a trackpad glides rather than leaping. The accumulator is
  // clamped so a flick cannot bank a dozen levels, and reset after each step
  // so the next notch starts fresh.
  zoomByWheel(e) {
    const map = this._map;
    if (!map) return;

    // Lines and pages converted with Leaflet's own factors, so the feel is
    // unchanged for the devices that were already working.
    const px = e.deltaMode === 1 ? e.deltaY * 20 : e.deltaMode === 2 ? e.deltaY * 60 : e.deltaY;
    if (!px) return;

    const banked = (this._wheelAccum || 0) + px;
    this._wheelAccum = Math.max(-WHEEL_ACCUM_CAP, Math.min(WHEEL_ACCUM_CAP, banked));
    if (Math.abs(this._wheelAccum) < WHEEL_PX_PER_ZOOM) return;

    // deltaY is positive scrolling *down*, which is zooming out.
    const step = this._wheelAccum > 0 ? -1 : 1;
    this._wheelAccum = 0;

    // `around` rather than a plain zoom: the point under the cursor stays
    // under the cursor, which is what makes wheel zoom feel like a map rather
    // than a slideshow. It is Leaflet's setZoomAround under another name, with
    // the container-relative point worked out by hand because MapLibre has no
    // mouseEventToContainerPoint.
    //
    // duration must stay well under the 350ms the wheel specs wait before
    // measuring; the library's default ease is 300ms, which would race them.
    const rect = map.getCanvasContainer().getBoundingClientRect();
    map.easeTo({
      zoom: map.getZoom() + step,
      around: map.unproject([e.clientX - rect.left, e.clientY - rect.top]),
      duration: 150,
    });
  }

  // Show the overlay, and take it away again. Re-triggering while it is up
  // restarts the clock rather than stacking timers, so holding a scroll does
  // not leave it flickering.
  showGestureHint(message) {
    const hint = this.shadowRoot.querySelector(".gesture-hint");
    if (!hint) return;
    hint.textContent = message;
    hint.hidden = false;
    clearTimeout(this._hintTimer);
    this._hintTimer = setTimeout(() => {
      hint.hidden = true;
    }, GESTURE_HINT_MS);
  }

  // The component's one output in pick mode. Composed, because it has to
  // cross the shadow boundary to reach the page that mounted it.
  emitPick(lngLat) {
    // Panning the world sideways gives longitudes past +/-180; wrap() folds
    // them back, so a click three worlds to the right still stores a
    // coordinate a database and a map tile server both accept.
    const { lat, lng } = new this._maplibre.LngLat(lngLat.lng, lngLat.lat).wrap();
    const round = (n) => Math.round(n * PICK_PRECISION) / PICK_PRECISION;
    this.dispatchEvent(
      new CustomEvent("location-picked", {
        bubbles: true,
        composed: true,
        detail: { lat: round(lat), lng: round(lng) },
      })
    );
  }
}

// A coordinate attribute as a number, or null when it is absent, blank or
// unparseable. Number("") and Number(null) are both 0 - a real place in the
// Gulf of Guinea - so the emptiness check cannot be skipped.
function readCoordinate(el, name) {
  const raw = el.getAttribute(name);
  if (raw == null || raw.trim() === "") return null;
  const n = Number(raw);
  return Number.isFinite(n) ? n : null;
}

function escapeHtml(s) {
  return String(s).replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[c]);
}

function escapeAttr(s) {
  return escapeHtml(s);
}

customElements.define("map-view", MapView);
