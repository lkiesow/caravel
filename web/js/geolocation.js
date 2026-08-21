// The device's own position, with the failure cases treated as first-class.
//
// Shared rather than inlined into the map component, because the locations
// list wants the same position and, more importantly, the same vocabulary for
// why it could not be had. Every caller must be able to say *which* of these
// happened, since they call for different responses from the user.
export const LOCATE_UNSUPPORTED = "unsupported";
export const LOCATE_INSECURE = "insecure";
export const LOCATE_DENIED = "denied";
export const LOCATE_UNAVAILABLE = "unavailable";
export const LOCATE_TIMEOUT = "timeout";

// i18n key for a reason, so a caller renders a message without a switch of
// its own.
//
// Spelled out rather than composed as `map.locate.${reason}`: a new reason
// then cannot be added without a string to go with it, the fallback is
// explicit rather than an accidental miss, and - the practical reason -
// scripts/i18n.py's unused-key scan can see these. Its dynamic-prefix rule
// only fires for a template literal *inside* a t() call, so composing the key
// in this module made all five look unreferenced.
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

// Resolves to {lat, lng, accuracy} - accuracy in metres, as the browser
// reports it. Rejects with an Error carrying .reason, one of the constants
// above, so callers never have to know about GeolocationPositionError codes.
export async function getCurrentPosition({ timeoutMs = 10000 } = {}) {
  const blocked = locateUnavailableReason();
  if (blocked) throw locateError(blocked);

  // A permission the user refused earlier is knowable without waiting for
  // anything, so say so immediately instead of timing out. Guarded because
  // the Permissions API is not everywhere, and an unanswerable query must
  // fall through to asking rather than block the feature.
  if (await permissionAlreadyDenied()) throw locateError(LOCATE_DENIED);

  return new Promise((resolve, reject) => {
    // Our own timer, and it is not belt-and-braces: PositionOptions.timeout
    // is NOT honoured while the permission prompt is outstanding. Measured in
    // Firefox with {timeout: 3000}, the call was still pending after six
    // seconds - so a user who ignores the prompt would otherwise leave the
    // button disabled forever, which is the exact failure this whole
    // milestone claims to prevent. Whatever the browser does, this settles.
    let settled = false;
    const finish = (fn, value) => {
      if (settled) return;
      settled = true;
      clearTimeout(timer);
      fn(value);
    };
    const timer = setTimeout(() => finish(reject, locateError(LOCATE_TIMEOUT)), timeoutMs);

    navigator.geolocation.getCurrentPosition(
      (position) =>
        finish(resolve, {
          lat: position.coords.latitude,
          lng: position.coords.longitude,
          accuracy: position.coords.accuracy,
        }),
      (error) => finish(reject, locateError(reasonFromError(error))),
      // enableHighAccuracy is deliberately off: it costs battery and seconds
      // for a precision nothing here needs - a map view and a distance
      // filter in kilometres. maximumAge lets a position taken moments ago be
      // reused, which makes a second press feel instant.
      { timeout: timeoutMs, maximumAge: 60000, enableHighAccuracy: false }
    );
  });
}

async function permissionAlreadyDenied() {
  try {
    const status = await navigator.permissions?.query?.({ name: "geolocation" });
    return status?.state === "denied";
  } catch {
    return false;
  }
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
