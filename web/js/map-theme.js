import { eventBus } from "./eventbus.js";
import { resolveTheme } from "./theme.js";
import { isDaylight, msUntilDaylightChanges } from "./sun.js";

// How the *map* is lit, which is not the same question as how the app is.
//
// The app has three states (auto / light / dark, see theme.js) and the map has
// four, because "follow the app" is itself one of the choices rather than the
// only behaviour:
//
//   auto   - light while the sun is up, dark otherwise. The default.
//   app    - whatever the interface is doing.
//   light  - always the light cartography.
//   dark   - always the dark one.
//
// A light map under a dark interface is a legitimate preference rather than a
// mistake: the map is the one thing on screen you look *at* rather than read,
// and a bright map is easier to read terrain from even at night. "auto" exists
// because the app's own auto follows the operating system, which is a setting
// somebody chose once; where the sun actually is is a fact about right now,
// and on a trip it is frequently not the same answer.
//
// Same storage shape as theme.js, including "the default is stored as the
// absence of a key", so a browser that has never been told anything and one
// told "follow the app" are the same state.
const STORAGE_KEY = "caravel.mapTheme";
const POSITION_KEY = "caravel.lastPosition";
export const MAP_THEMES = ["auto", "app", "light", "dark"];

// Day/night rather than "follow the app", because it is the answer that is
// right more often: the interface's own auto follows the operating system,
// which is a switch somebody set at home, while where the sun actually is is a
// fact about now and about the place on screen. On a trip those disagree
// regularly, and the map is the surface where the difference shows.
//
// It degrades to the app's answer rather than to a guess whenever there is no
// coordinate to work from, so the worst case is the behaviour this used to
// default to. See resolveMapTheme.
const DEFAULT_MAP_THEME = "auto";

// A remembered fix goes stale as a position but not as a *timezone*: somebody
// who located themselves in Reykjavik a week ago is still much more likely to
// be near Reykjavik than at longitude 0. Kept long enough to survive a trip,
// short enough that moving continents eventually corrects itself even if the
// locate control is never used again.
const POSITION_MAX_AGE_MS = 14 * 24 * 3600 * 1000;

export function getMapTheme() {
  try {
    const stored = localStorage.getItem(STORAGE_KEY);
    return MAP_THEMES.includes(stored) ? stored : DEFAULT_MAP_THEME;
  } catch {
    return DEFAULT_MAP_THEME;
  }
}

export function setMapTheme(preference) {
  if (!MAP_THEMES.includes(preference)) return;
  try {
    if (preference === DEFAULT_MAP_THEME) localStorage.removeItem(STORAGE_KEY);
    else localStorage.setItem(STORAGE_KEY, preference);
  } catch {
    // Unpersisted but still applied for this page, as theme.js reasons: a
    // control that visibly does nothing is worse than one that forgets.
  }
  announce();
}

// Remembered whenever the locate control succeeds, so "auto" has somewhere to
// start from without ever asking a question of its own.
export function rememberPosition({ lat, lng }) {
  try {
    localStorage.setItem(POSITION_KEY, JSON.stringify({ lat, lng, at: Date.now() }));
  } catch {
    // Then "auto" falls back to the map's own centre. Not worth failing over.
  }
  announce();
}

export function lastKnownPosition() {
  try {
    const raw = JSON.parse(localStorage.getItem(POSITION_KEY) || "null");
    if (!raw || !Number.isFinite(raw.lat) || !Number.isFinite(raw.lng)) return null;
    if (Date.now() - raw.at > POSITION_MAX_AGE_MS) return null;
    return raw;
  } catch {
    return null;
  }
}

// "light" or "dark", never "app" or "auto".
//
// Synchronous on purpose: a map cannot be constructed without a style, so a
// resolution that had to await something would mean either a map that flashes
// the wrong cartography or one that waits on a permission prompt before it
// draws. Everything asynchronous happens in primeMapTheme() below, out of
// band, and announces itself if the answer turns out to have changed.
//
// `near` is the map's own viewport centre, offered by the caller as the last
// coordinate guess before giving up. It is a surprisingly good one for this
// app: somebody looking at a map of Patagonia is usually asking about
// Patagonia.
// How long until the day/night answer changes for a coordinate the caller
// supplied, or null when there is nothing worth waking up for.
//
// This exists because the module's own timer can only schedule against a
// *remembered* position, and the common case for a fresh browser is that there
// is not one -- the map passes the place it is showing instead. Returns null
// when a remembered fix exists, since scheduleNextTransition already covers
// that and two timers would fight.
export function msUntilMapThemeChanges({ near } = {}) {
  if (getMapTheme() !== "auto") return null;
  if (lastKnownPosition() || !near) return null;
  return msUntilDaylightChanges(near.lat, near.lng) + 1000;
}

export function resolveMapTheme({ near } = {}) {
  const preference = getMapTheme();
  if (preference === "light" || preference === "dark") return preference;
  if (preference === "app") return resolveTheme();

  const where = lastKnownPosition() || near;
  // No idea where the reader is and no map to take a hint from - the app's own
  // answer is better than picking one.
  if (!where) return resolveTheme();
  return isDaylight(where.lat, where.lng) ? "light" : "dark";
}

// Refreshes what "auto" has to work with, and schedules the next change.
//
// The geolocation call is gated on the Permissions API reporting "granted",
// which is the whole point: a granted permission answers silently, while an
// ungranted one raises a prompt. Tinting a map is nowhere near a good enough
// reason to ask for somebody's location, so this only ever uses an answer they
// have already given - to the locate control, most likely.
export async function primeMapTheme() {
  scheduleNextTransition();

  if (getMapTheme() !== "auto") return;
  if (!("geolocation" in navigator) || !window.isSecureContext) return;
  try {
    const status = await navigator.permissions?.query({ name: "geolocation" });
    if (status?.state !== "granted") return;
  } catch {
    // No Permissions API means no way to tell a silent answer from a prompt,
    // so do not risk the prompt.
    return;
  }
  navigator.geolocation.getCurrentPosition(
    (p) => rememberPosition({ lat: p.coords.latitude, lng: p.coords.longitude }),
    () => {},
    { timeout: 10000, maximumAge: 3600000 }
  );
}

// Sunrise and sunset are the only inputs here that change on their own, so
// rather than polling, work out when the answer next flips and wake up then.
// Re-armed each time, and re-armed from scratch whenever the preference or the
// position changes.
let transitionTimer = null;

function scheduleNextTransition() {
  clearTimeout(transitionTimer);
  if (getMapTheme() !== "auto") return;
  const where = lastKnownPosition();
  if (!where) return;
  // Plus a second, so the timer lands after the transition rather than on it.
  const ms = msUntilDaylightChanges(where.lat, where.lng) + 1000;
  transitionTimer = setTimeout(() => {
    announce();
  }, ms);
}

function announce() {
  scheduleNextTransition();
  eventBus.dispatchEvent(new CustomEvent("map-theme-changed"));
}

// Called once at boot, after primeMapTheme. The map follows the *app* theme in
// the default mode, so a change there is a change here.
export function initMapTheme() {
  eventBus.addEventListener("theme-changed", () => {
    if (getMapTheme() === "app") eventBus.dispatchEvent(new CustomEvent("map-theme-changed"));
  });
  primeMapTheme();
}
