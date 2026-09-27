// A scripted navigator.geolocation, for the specs that care about accuracy.
//
// Playwright's context.setGeolocation is the right tool for "the device is
// here"; it is the wrong tool for everything Stage 36 is about. It cannot set
// a *sequence* of accuracies, cannot control the interval between them, and
// cannot say whether clearWatch was ever called -- and those three are exactly
// what separates a control that works on a boat from one that does not.
//
// So this replaces the API outright and hands the test the emitter. One
// end-to-end test on the real path stays in map.spec.js, so the fake is never
// the only thing under test.
//
// Date.now is offset along with it, because the tracking guards are written in
// elapsed time and a test that waited out fifteen real seconds is a test
// nobody runs.
export async function installFakeGeolocation(page) {
  await page.addInitScript(() => {
    let offset = 0;
    const realNow = Date.now.bind(Date);
    Date.now = () => realNow() + offset;

    const state = {
      starts: 0,
      clears: 0,
      options: [],
      advance: (ms) => {
        offset += ms;
      },
    };
    let nextId = 1;
    const live = new Map();

    state.emit = (fix) => {
      for (const cb of live.values()) {
        cb.success({
          coords: { latitude: fix.lat, longitude: fix.lng, accuracy: fix.accuracy },
          timestamp: Date.now(),
        });
      }
    };
    state.emitError = (code) => {
      for (const cb of live.values()) cb.error?.({ code });
    };

    Object.defineProperty(navigator, "geolocation", {
      configurable: true,
      value: {
        watchPosition(success, error, options) {
          state.starts += 1;
          state.options.push(options);
          const id = nextId++;
          live.set(id, { success, error });
          return id;
        },
        clearWatch(id) {
          if (live.delete(id)) state.clears += 1;
        },
        getCurrentPosition() {
          // map-theme.js still uses this one, deliberately and separately --
          // see the out-of-scope note in plans/stage-36.md. It is
          // permission-gated and never fires under the suite, so reaching it
          // here means something has started asking the wrong way.
          throw new Error("getCurrentPosition must not be used for locating");
        },
      },
    });

    window.__geo = state;
  });
}

export const emitFix = (page, fix) => page.evaluate((f) => window.__geo.emit(f), fix);

export const advanceClock = (page, ms) => page.evaluate((v) => window.__geo.advance(v), ms);

export const geoCounters = (page) =>
  page.evaluate(() => ({ starts: window.__geo.starts, clears: window.__geo.clears }));

// Waits for the module to have actually registered its watch. It checks the
// permission first, asynchronously, so emitting any sooner goes nowhere.
export const waitForWatch = (page, atLeast = 1) =>
  page.waitForFunction((n) => window.__geo.starts >= n, atLeast, { timeout: 10000 });

// Coast, then closer, then the real position out on the water. The accuracies
// are the point: 2800m is roughly what tower trilateration gives you offshore,
// and it is what put the marker ashore in the first place.
export const COAST = { lat: 54.32, lng: 10.14, accuracy: 2800 };
export const NEARER = { lat: 54.4, lng: 10.3, accuracy: 300 };
export const AT_SEA = { lat: 54.45, lng: 10.42, accuracy: 18 };
