// Is it daylight at a point on the Earth right now?
//
// Used by the map's "day/night" appearance mode, and deliberately kept apart
// from it: this file knows about the sun and nothing about maps or settings.
//
// The NOAA low-accuracy solar position equations, which are good to about a
// minute -- far past what is needed to decide whether to draw a light or a
// dark map. No dependency: the alternative was a date library for something
// that is forty lines of arithmetic, in a project that vendors what it uses.
//
// The one case worth naming, because it is the case a naive sunrise/sunset
// formula gets wrong and this app is a *travel* app: above the polar circles
// the sun can fail to rise or set at all for weeks. A formula that solves for
// a sunrise time has no answer there and typically returns NaN, which then
// reads as "night" and gives a Norwegian summer a dark map at noon. This
// computes the sun's *altitude* instead -- is it above the horizon, right now
// -- which is always defined and is the question actually being asked.

const RAD = Math.PI / 180;

// Civil twilight, not the geometric horizon. The sky is still light for a
// while after the sun has set, and switching the map to its night colouring
// while it is plainly dusk outside looks broken; -6 degrees is the standard
// definition of the point where it stops being usefully light.
const CIVIL_TWILIGHT_DEGREES = -6;

// Days since the J2000.0 epoch, the reference moment these equations are
// written against.
function julianDaysSinceEpoch(date) {
  return date.getTime() / 86400000 - 10957.5;
}

// The sun's height above the horizon, in degrees. Negative is below.
export function solarAltitude(lat, lng, date = new Date()) {
  const d = julianDaysSinceEpoch(date);

  // Mean anomaly of the Earth's orbit, and the ecliptic longitude that follows
  // from it once the orbit's eccentricity is accounted for.
  const meanAnomaly = (357.5291 + 0.98560028 * d) * RAD;
  const eclipticLongitude =
    meanAnomaly +
    (1.9148 * Math.sin(meanAnomaly) +
      0.02 * Math.sin(2 * meanAnomaly) +
      0.0003 * Math.sin(3 * meanAnomaly)) *
      RAD +
    102.9372 * RAD +
    Math.PI;

  // Obliquity of the ecliptic: the tilt that gives the Earth its seasons.
  const obliquity = 23.4397 * RAD;
  const declination = Math.asin(Math.sin(eclipticLongitude) * Math.sin(obliquity));
  const rightAscension = Math.atan2(
    Math.sin(eclipticLongitude) * Math.cos(obliquity),
    Math.cos(eclipticLongitude)
  );

  // Where the observer is relative to the sun, once the Earth's rotation and
  // their longitude are taken into account.
  const siderealTime = (280.16 + 360.9856235 * d) * RAD + lng * RAD;
  const hourAngle = siderealTime - rightAscension;

  const phi = lat * RAD;
  return (
    Math.asin(
      Math.sin(phi) * Math.sin(declination) +
        Math.cos(phi) * Math.cos(declination) * Math.cos(hourAngle)
    ) / RAD
  );
}

// The question the map actually asks.
export function isDaylight(lat, lng, date = new Date()) {
  return solarAltitude(lat, lng, date) > CIVIL_TWILIGHT_DEGREES;
}

// When to look again.
//
// Rather than solving for the next sunrise or sunset -- which, per the note at
// the top, has no answer inside a polar day -- this walks forward in coarse
// steps until the answer changes, then bisects. It always terminates: if
// nothing changes within the horizon it gives up and says "check again then",
// which is the right answer for a place where the sun will not set this week.
const STEP_MINUTES = 15;
const HORIZON_HOURS = 24;

export function msUntilDaylightChanges(lat, lng, date = new Date()) {
  const now = date.getTime();
  const current = isDaylight(lat, lng, date);
  const step = STEP_MINUTES * 60000;
  const horizon = HORIZON_HOURS * 3600000;

  let previous = now;
  for (let t = now + step; t <= now + horizon; t += step) {
    if (isDaylight(lat, lng, new Date(t)) !== current) {
      // Bisect the fifteen minutes we just crossed, down to a minute.
      let lo = previous;
      let hi = t;
      while (hi - lo > 60000) {
        const mid = (lo + hi) / 2;
        if (isDaylight(lat, lng, new Date(mid)) === current) lo = mid;
        else hi = mid;
      }
      return hi - now;
    }
    previous = t;
  }
  return horizon;
}
