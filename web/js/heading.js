// The direction the device faces, from its compass (Stage 42).
//
// Not the direction of travel: that comes with every geolocation fix and is
// shown as the arrow (see showCourse in map-view.js). This is where the phone
// is *pointing*, which is known standing still and is what the cone around the
// marker shows. The two are deliberately never merged into one number -- that
// would assume the phone points the way you walk, which is false the moment it
// is in a pocket.
//
// Three spellings of the same reading:
//
//   deviceorientationabsolute  alpha relative to north. Chrome/Android, and
//                              Firefox exposes it too. No permission prompt.
//   webkitCompassHeading       on iOS Safari's deviceorientation, already
//                              relative to north -- behind
//                              DeviceOrientationEvent.requestPermission(),
//                              which only works from a user gesture.
//   deviceorientation with     the fallback for an engine that has neither:
//   absolute: true             the event says itself that alpha is absolute.
//
// Plain deviceorientation with absolute false is relative to wherever the
// device happened to point when it started, and is refused outright: an
// arbitrary zero drawn as a compass is worse than no compass.
//
// A missing or refused compass is not an error and has no message. The cone
// simply never appears; the marker not claiming a direction is the honest
// state, and the position half of the control works regardless.

// Exponential smoothing, per reading. The sensor reports at up to 60Hz and
// jitters by a few degrees; this settles in a handful of readings, which is
// well under a tenth of a second.
const SMOOTHING = 0.25;

// Changes smaller than this are not worth a style write.
const MIN_CHANGE_DEG = 1;

// No reading for this long and the direction is withdrawn. Some platforms go
// quiet rather than say the sensor stopped; a cone frozen at its last angle
// would then be a claim nobody is making any more.
const STALE_MS = 5000;
const STALE_CHECK_MS = 1000;

// The iOS answer, once there is one: true, false, or null for not yet asked
// (or asked in a way the platform refused to consider, such as outside a
// gesture -- which is not the reader saying no).
let permission = null;

function permissionNeeded() {
  return typeof globalThis.DeviceOrientationEvent?.requestPermission === "function";
}

// Ask for the compass, where the platform wants asking.
//
// Must be called synchronously from inside a click handler, before anything is
// awaited: iOS checks that the call itself is user-initiated, and a call one
// microtask late is refused. Resolves to whether the compass may be used.
// Asks at most once per page: a reader who answered has answered.
export function requestCompassPermission() {
  if (!permissionNeeded()) return Promise.resolve(true);
  if (permission !== null) return Promise.resolve(permission);
  let asked;
  try {
    asked = DeviceOrientationEvent.requestPermission();
  } catch {
    return Promise.resolve(false);
  }
  return Promise.resolve(asked).then(
    (state) => (permission = state === "granted"),
    () => false
  );
}

// Whether a watch started now could receive anything. On iOS that needs an
// answered "yes"; everywhere else there is nothing to ask.
export function compassAllowed() {
  return !permissionNeeded() || permission === true;
}

// The compass, continuously. Returns {cancel}. onUpdate receives degrees
// clockwise from north for the top of the *screen*, smoothed, at most once a
// frame -- or null when the direction has gone stale -- and, as its second
// argument, how far off the platform says that could be, in degrees, or null
// where it does not say. Only iOS does (webkitCompassAccuracy); Android and
// Firefox expose nothing.
export function watchCompass({ onUpdate }) {
  const type = "ondeviceorientationabsolute" in window ? "deviceorientationabsolute" : "deviceorientation";

  // The smoothed direction as a unit vector rather than an angle: averaging
  // 350 and 10 as angles gives 180, the one answer that is certainly wrong.
  // As vectors they average to north, and the wrap needs no special case.
  let x = null;
  let y = null;
  let lastAt = 0;
  let delivered = null;
  let accuracy = null;
  let deliveredAccuracy = null;
  let frame = 0;

  const deliver = () => {
    frame = 0;
    if (x === null) return;
    const deg = ((Math.atan2(x, y) * 180) / Math.PI + 360) % 360;
    const sameAccuracy = accuracy === deliveredAccuracy;
    if (delivered !== null && sameAccuracy && angleBetween(deg, delivered) < MIN_CHANGE_DEG) return;
    delivered = deg;
    deliveredAccuracy = accuracy;
    onUpdate(deg, accuracy);
  };

  const withdraw = () => {
    const had = delivered !== null;
    x = y = delivered = deliveredAccuracy = null;
    if (had) onUpdate(null, null);
  };

  const onReading = (event) => {
    // iOS reports -1 while the compass is uncalibrated: it is saying the
    // heading means nothing, and it gets believed. Withdrawn at once rather
    // than left to go stale, since the reading is arriving -- it is just
    // worthless.
    const reported = event.webkitCompassAccuracy;
    if (Number.isFinite(reported) && reported < 0) return withdraw();
    accuracy = Number.isFinite(reported) ? reported : null;

    const raw = compassHeading(event, type);
    if (raw === null) return;
    // The sensor reports for the top of the *device*. In landscape the top of
    // the screen is a quarter turn away from it, and the cone is drawn on the
    // screen.
    const deg = (raw + screenAngle()) % 360;
    const rad = (deg * Math.PI) / 180;
    if (x === null) {
      x = Math.sin(rad);
      y = Math.cos(rad);
    } else {
      x += (Math.sin(rad) - x) * SMOOTHING;
      y += (Math.cos(rad) - y) * SMOOTHING;
    }
    lastAt = Date.now();
    if (!frame) frame = requestAnimationFrame(deliver);
  };

  // Against Date.now rather than a timeout per reading, for the same reason as
  // the arrow's staleness check: what matters is how old the last reading is.
  const staleTimer = setInterval(() => {
    if (delivered === null || Date.now() - lastAt < STALE_MS) return;
    withdraw();
  }, STALE_CHECK_MS);

  window.addEventListener(type, onReading);

  return {
    cancel() {
      window.removeEventListener(type, onReading);
      clearInterval(staleTimer);
      if (frame) cancelAnimationFrame(frame);
      frame = 0;
    },
  };
}

// Degrees clockwise from north for the top of the device, or null when this
// event cannot say. See the platform table at the top of this file.
function compassHeading(event, type) {
  if (Number.isFinite(event.webkitCompassHeading) && event.webkitCompassHeading >= 0) {
    return event.webkitCompassHeading % 360;
  }
  // alpha is null on a device with no sensor -- desktop Chrome fires exactly
  // such an event -- and it counts anticlockwise, hence 360 minus.
  if (!Number.isFinite(event.alpha)) return null;
  if (type !== "deviceorientationabsolute" && event.absolute !== true) return null;
  return (360 - event.alpha) % 360;
}

function screenAngle() {
  const angle = screen.orientation?.angle ?? window.orientation ?? 0;
  return ((Number(angle) || 0) % 360 + 360) % 360;
}

function angleBetween(a, b) {
  const d = Math.abs(a - b) % 360;
  return d > 180 ? 360 - d : d;
}
