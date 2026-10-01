// The device's own position, with the failure cases treated as first-class.
//
// Shared rather than inlined into the map component, because the locations
// list wants the same position and, more importantly, the same vocabulary for
// why it could not be had. Every caller must be able to say *which* of these
// happened, since they call for different responses from the user.
import { rememberPosition } from "./map-theme.js";

export const LOCATE_UNSUPPORTED = "unsupported";
export const LOCATE_INSECURE = "insecure";
export const LOCATE_DENIED = "denied";
export const LOCATE_UNAVAILABLE = "unavailable";
export const LOCATE_TIMEOUT = "timeout";
// Not a failure and never shown: the caller went away (the map element was
// disconnected, the tab was switched). It exists so cancel() can settle the
// promise rather than leave it pending forever, and so the one caller that
// awaits can tell "you stopped this" from "this did not work".
export const LOCATE_CANCELLED = "cancelled";

// i18n key for a reason, so a caller renders a message without a switch of
// its own.
//
// Spelled out rather than composed as `map.locate.${reason}`: a new reason
// then cannot be added without a string to go with it, the fallback is
// explicit rather than an accidental miss, and - the practical reason -
// scripts/i18n.py's unused-key scan can see these. Its dynamic-prefix rule
// only fires for a template literal *inside* a t() call, so composing the key
// in this module made all five look unreferenced.
//
// LOCATE_CANCELLED is deliberately absent: it has no message because it is
// never rendered, and locateErrorKey's fallback covers a caller that shows it
// anyway.
const LOCATE_ERROR_KEYS = {
  [LOCATE_UNSUPPORTED]: "map.locate.unsupported",
  [LOCATE_INSECURE]: "map.locate.insecure",
  [LOCATE_DENIED]: "map.locate.denied",
  [LOCATE_UNAVAILABLE]: "map.locate.unavailable",
  [LOCATE_TIMEOUT]: "map.locate.timeout",
};

export function locateErrorKey(reason) {
  return LOCATE_ERROR_KEYS[reason] ?? LOCATE_ERROR_KEYS[LOCATE_UNAVAILABLE];
}

// Why the device's position cannot be asked for at all, or null if it can.
//
// The insecure case is the one that matters and the reason this is a function
// rather than a try/catch around a call: over plain HTTP on a phone,
// navigator.geolocation exists and getCurrentPosition simply *never calls
// back* - no error, no timeout, nothing. A control that silently does nothing
// forever is worse than one that is visibly unavailable and says why, so this
// is checked up front and the button is disabled with an explanation.
// localhost counts as a secure context, which is why dev and the UI suite can
// exercise the happy path at all.
export function locateUnavailableReason() {
  if (!("geolocation" in navigator)) return LOCATE_UNSUPPORTED;
  if (!window.isSecureContext) return LOCATE_INSECURE;
  return null;
}

export function canLocate() {
  return locateUnavailableReason() === null;
}

// Once tracking, a fix this much worse than the settled one is treated as
// noise rather than news -- but only for DEGRADED_GRACE_MS. See acceptFix().
const TRACKING_ACCURACY_FLOOR_M = 500;
const DEGRADED_GRACE_MS = 15000;

// Rendering pace while tracking. The platform pushes about 1Hz under GNSS,
// which is more DOM churn than anyone needs; a fix that moved meaningfully
// jumps the queue so a fast boat is not shown lagging.
const TRACK_THROTTLE_MS = 3000;
const TRACK_MOVE_M = 10;

// Our own most recent fix, for maxAgeMs. Deliberately ours rather than the
// platform's maximumAge: this one carries its own accuracy and timestamp, can
// be reasoned about from a test, and cannot silently satisfy a request we
// wanted a real acquisition for. See the note on maximumAge in watchPosition.
let lastFix = null;

// The device's position, once and then repeatedly.
//
// Returns {promise, cancel}. The promise resolves with the best fix once it
// *settles* - desiredAccuracyM met, or settleDeadlineMs elapsed - and rejects
// only if nothing arrived at all, or the platform failed before the first fix.
// onUpdate fires for every accepted fix, which is how a map redraws while the
// answer is still improving.
//
// Two phases, and the boundary between them is the whole design:
//
//   Acquiring - only *improving* fixes are accepted. Platforms deliver the
//   last network fix first and refine seconds later, so without this rule the
//   marker would bounce between a tower-trilaterated guess and a real one.
//
//   Tracking (continuous only, after settle) - every fix is accepted, because
//   by then each one is a genuinely new *position* rather than a better guess
//   at the same one. Keeping the improving-only rule here would freeze the
//   marker at the first good fix and never move it again, which is the exact
//   opposite of what continuous tracking is for.
export function watchPosition({
  onUpdate,
  onSettled,
  onError,
  desiredAccuracyM = 50,
  settleDeadlineMs = 30000,
  firstFixTimeoutMs = 10000,
  continuous = false,
  maxAgeMs = 0,
} = {}) {
  let cancelled = false;
  let finished = false;
  let watchId = null;
  let firstFixTimer = null;
  let deadlineTimer = null;

  let best = null;
  let settled = false;
  let settledAccuracy = null;
  let degradedSince = null;
  let lastDeliveredAt = 0;
  let lastDelivered = null;

  let resolvePromise;
  let rejectPromise;
  const promise = new Promise((res, rej) => {
    resolvePromise = res;
    rejectPromise = rej;
  });

  const stop = () => {
    if (watchId !== null) {
      navigator.geolocation.clearWatch(watchId);
      watchId = null;
    }
    clearTimeout(firstFixTimer);
    clearTimeout(deadlineTimer);
    firstFixTimer = null;
    deadlineTimer = null;
  };

  const fail = (reason) => {
    if (finished) return;
    finished = true;
    stop();
    rejectPromise(locateError(reason));
  };

  const settle = () => {
    if (settled || !best) return;
    settled = true;
    settledAccuracy = best.accuracy;
    // The second and last time the day/night hint is written. Never during
    // tracking: rememberPosition announces, which drives map-theme's listeners
    // and can restyle the map, and whether the sun is up does not change over
    // the course of a boat ride.
    rememberPosition(best);
    const fix = { ...best, final: true };
    onSettled?.(fix);
    if (!finished) {
      finished = true;
      resolvePromise(fix);
    }
    if (!continuous) stop();
    else clearTimeout(deadlineTimer);
  };

  // Whether this fix is news, by phase. See the two-phase note above.
  const acceptFix = (fix, now) => {
    if (!settled) return best === null || fix.accuracy < best.accuracy;

    // Tracking. A single wild reading - GNSS dropping back to the towers for
    // one sample - must not teleport the marker ashore, but refusing forever
    // would be its own lie: if the good signal is genuinely gone, the honest
    // thing is to show the bad position with its (large) ring rather than a
    // stale one with a small one. So: refuse, but only for a bounded while.
    const ceiling = Math.max(settledAccuracy * 3, TRACKING_ACCURACY_FLOOR_M);
    if (fix.accuracy <= ceiling) {
      degradedSince = null;
      return true;
    }
    if (degradedSince === null) degradedSince = now;
    // degradedSince is deliberately *not* cleared here: once the grace has
    // run out, every subsequent poor fix is accepted too, rather than each one
    // starting a fresh fifteen seconds of refusal.
    return now - degradedSince >= DEGRADED_GRACE_MS;
  };

  // Acquiring delivers every accepted fix: they are rare (each is an
  // improvement) and each one is what redraws a wrong marker. Tracking is the
  // firehose, so only that half is paced.
  const shouldDeliver = (fix, now) => {
    if (!settled || !lastDelivered) return true;
    if (now - lastDeliveredAt >= TRACK_THROTTLE_MS) return true;
    return distanceKm(lastDelivered, fix) * 1000 > TRACK_MOVE_M;
  };

  const handle = (fix) => {
    const now = Date.now();
    if (!acceptFix(fix, now)) return;

    // Unconditional, because acceptFix has already decided: acquiring, it
    // only passes improvements; tracking, the newest fix *is* the answer.
    const first = best === null;
    best = fix;
    lastFix = { ...fix, at: now };
    // Written on the first fix as well as at settle, so someone who navigates
    // away mid-acquisition is no worse off than they were before this stage.
    if (first) rememberPosition(fix);

    clearTimeout(firstFixTimer);
    firstFixTimer = null;

    if (shouldDeliver(fix, now)) {
      lastDelivered = fix;
      lastDeliveredAt = now;
      onUpdate?.({ ...fix, final: false });
    }

    if (!settled && fix.accuracy <= desiredAccuracyM) settle();
  };

  const start = () => {
    if (cancelled) return;

    firstFixTimer = setTimeout(() => fail(LOCATE_TIMEOUT), firstFixTimeoutMs);
    deadlineTimer = setTimeout(() => {
      if (best) settle();
      else fail(LOCATE_TIMEOUT);
    }, settleDeadlineMs);

    watchId = navigator.geolocation.watchPosition(
      (position) =>
        handle({
          lat: position.coords.latitude,
          lng: position.coords.longitude,
          accuracy: position.coords.accuracy,
          ...courseOf(position.coords),
          timestamp: position.timestamp,
        }),
      (error) => {
        // Before the first fix an error is fatal - there is nothing to show.
        // After it, it is weather: a boat loses and regains signal constantly,
        // and tearing the watch down on the first POSITION_UNAVAILABLE would
        // make tracking useless exactly where it is wanted.
        if (best === null) fail(reasonFromError(error));
        else onError?.(locateError(reasonFromError(error)));
      },
      // The heart of Stage 36. enableHighAccuracy is not a precision hint: it
      // decides which *sensor* answers. With it false the platform answers
      // from the network provider - wifi and cell towers - and never powers
      // GNSS, so at sea the only reachable towers are ashore and the fix lands
      // on the coast. maximumAge went with it: a cache window lets the
      // platform answer without acquiring anything at all, which would make
      // the flag half a no-op. Our own lastFix covers the "second press feels
      // instant" case that maximumAge used to, and covers it honestly.
      //
      // The old comment here argued the opposite - "a precision nothing here
      // needs - a map view and a distance filter in kilometres". That is true
      // on land and false on water, which is what makes it worth keeping as a
      // record: the mistake was reasoning about how precise the answer is
      // instead of about where it comes from.
      { timeout: settleDeadlineMs, maximumAge: 0, enableHighAccuracy: true }
    );
  };

  (async () => {
    const blocked = locateUnavailableReason();
    if (blocked) return fail(blocked);

    // A permission the user refused earlier is knowable without waiting for
    // anything, so say so immediately instead of timing out. Guarded because
    // the Permissions API is not everywhere, and an unanswerable query must
    // fall through to asking rather than block the feature.
    if (await permissionAlreadyDenied()) return fail(LOCATE_DENIED);
    if (cancelled) return;

    // Our cache, not the platform's. Only for a one-shot caller: a continuous
    // watch is asking to be told where you are *now*, repeatedly, and serving
    // it something from thirty seconds ago would be answering a different
    // question.
    if (!continuous && maxAgeMs > 0 && lastFix && Date.now() - lastFix.at <= maxAgeMs) {
      best = { lat: lastFix.lat, lng: lastFix.lng, accuracy: lastFix.accuracy };
      onUpdate?.({ ...best, final: false });
      return settle();
    }

    start();
  })();

  return {
    promise,
    cancel() {
      cancelled = true;
      stop();
      // Settles rather than leaving the promise pending forever, so an awaiting
      // caller has a path to run its finally block. Never rendered - map-view
      // recognises the reason and says nothing.
      if (!finished) {
        finished = true;
        rejectPromise(locateError(LOCATE_CANCELLED));
      }
    },
  };
}

// Resolves to {lat, lng, accuracy, heading, speed, timestamp, final} -
// accuracy in metres, as the browser reports it; heading and speed as
// courseOf() normalises them, so either may be null. Rejects with an Error carrying .reason, one of the
// constants above, so callers never have to know about
// GeolocationPositionError codes.
//
// The defaults are a one-shot caller's: today that is the locations list's
// distance filter, which asks in kilometres, so half a kilometre is already
// far better than it needs and waiting thirty seconds for a GNSS lock to
// answer "within 5 km" would be absurd. A caller that wants a tight fix says
// so.
export async function getCurrentPosition({
  timeoutMs = 10000,
  desiredAccuracyM = 500,
  deadlineMs = 12000,
  maxAgeMs = 0,
} = {}) {
  return watchPosition({
    desiredAccuracyM,
    settleDeadlineMs: deadlineMs,
    firstFixTimeoutMs: timeoutMs,
    maxAgeMs,
    continuous: false,
  }).promise;
}

async function permissionAlreadyDenied() {
  try {
    const status = await navigator.permissions?.query?.({ name: "geolocation" });
    return status?.state === "denied";
  } catch {
    return false;
  }
}

// Direction of travel and speed, as the platform reports them -- or null.
//
// heading is degrees clockwise from true north and speed is metres per
// second, both derived by the platform from GNSS, so they say which way the
// device is *moving*, never which way it is pointing. Browsers disagree on how
// they spell "not known": null, NaN, or a heading of 0 alongside a speed of 0.
// Normalised here so callers only ever see a number they can use or null.
// A heading without a positive speed is dropped for the same reason: a device
// standing still has no direction of travel, whatever number came with it.
function courseOf(coords) {
  const speed = Number.isFinite(coords.speed) && coords.speed >= 0 ? coords.speed : null;
  const heading =
    speed > 0 && Number.isFinite(coords.heading) ? ((coords.heading % 360) + 360) % 360 : null;
  return { heading, speed };
}

function reasonFromError(error) {
  switch (error?.code) {
    case 1: // PERMISSION_DENIED
      return LOCATE_DENIED;
    case 2: // POSITION_UNAVAILABLE
      return LOCATE_UNAVAILABLE;
    case 3: // TIMEOUT
      return LOCATE_TIMEOUT;
    default:
      return LOCATE_UNAVAILABLE;
  }
}

function locateError(reason) {
  const err = new Error(`geolocation unavailable: ${reason}`);
  err.reason = reason;
  return err;
}

// Great-circle distance in kilometres. Enough for a "within 5 km" filter -
// the haversine's error against a proper geodesic is well under a percent,
// and the inputs are a phone's fix and a hand-placed pin anyway.
export function distanceKm(a, b) {
  const R = 6371;
  const toRad = (deg) => (deg * Math.PI) / 180;
  const dLat = toRad(b.lat - a.lat);
  const dLng = toRad(b.lng - a.lng);
  const h =
    Math.sin(dLat / 2) ** 2 +
    Math.cos(toRad(a.lat)) * Math.cos(toRad(b.lat)) * Math.sin(dLng / 2) ** 2;
  return 2 * R * Math.asin(Math.min(1, Math.sqrt(h)));
}
