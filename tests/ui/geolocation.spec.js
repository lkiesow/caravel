// Stage 36 Milestone 1. The position module, driven directly.
//
// Its own file, and driven through a scripted fake rather than Playwright's
// context.setGeolocation, for one reason: this milestone is *about* accuracy
// and timing, and setGeolocation can set neither a sequence of accuracies nor
// the interval between them. The fake emits exactly what the test says,
// exactly when it says it, and counts the clearWatch calls -- none of which is
// observable through the real path. One end-to-end test on the real path stays
// in map.spec.js so the fake is never the only thing under test.
import { test, expect } from "@playwright/test";
import {
  AT_SEA,
  COAST,
  NEARER,
  advanceClock,
  emitFix,
  geoCounters,
  installFakeGeolocation,
  waitForWatch,
} from "./helpers/geolocation.js";

// Starts a watch and parks the handle plus a record of everything it reported
// on window, so the test can emit fixes between assertions.
async function startWatch(page, options = {}) {
  await page.evaluate(async (opts) => {
    const m = await import("/js/geolocation.js");
    const record = { updates: [], settled: null, resolved: null, error: null, errors: [] };
    window.__record = record;
    const handle = m.watchPosition({
      ...opts,
      onUpdate: (fix) => record.updates.push(fix),
      onSettled: (fix) => {
        record.settled = fix;
      },
      onError: (err) => record.errors.push(err.reason),
    });
    window.__handle = handle;
    handle.promise.then(
      (fix) => {
        record.resolved = fix;
      },
      (err) => {
        record.error = err.reason;
      }
    );
  }, options);
  await waitForWatch(page);
}

const emit = emitFix;
const record = (page) => page.evaluate(() => window.__record);
const counters = geoCounters;

test.beforeEach(async ({ page }) => {
  await installFakeGeolocation(page);
  await page.goto("/");
});

test("asks for the sensor that works at sea", async ({ page }) => {
  // The whole bug, in one assertion. enableHighAccuracy false answers from
  // wifi and cell towers, which offshore are all ashore; maximumAge lets the
  // platform answer from that cache without acquiring anything at all.
  await startWatch(page);
  const [options] = await page.evaluate(() => window.__geo.options);
  expect(options.enableHighAccuracy, "GNSS must be asked for").toBe(true);
  expect(options.maximumAge, "a cache window would make the flag half a no-op").toBe(0);
});

test("delivers the first fix immediately, then refines", async ({ page }) => {
  await startWatch(page);

  await emit(page, COAST);
  let seen = await record(page);
  expect(seen.updates, "the first fix must not wait for a better one").toHaveLength(1);
  expect(seen.updates[0].accuracy).toBe(2800);
  expect(seen.resolved, "2800m is nowhere near good enough to settle on").toBeNull();

  await emit(page, NEARER);
  await emit(page, AT_SEA);
  await page.waitForFunction(() => window.__record.resolved !== null, null, { timeout: 5000 });

  seen = await record(page);
  expect(seen.updates.map((u) => u.accuracy)).toEqual([2800, 300, 18]);
  expect(seen.resolved.accuracy, "it settles on the good fix, not the first one").toBe(18);
  expect(seen.resolved.lat).toBeCloseTo(AT_SEA.lat, 5);
  expect(seen.resolved.final).toBe(true);
});

test("while acquiring, a worse fix is ignored", async ({ page }) => {
  // Platforms interleave the network provider and GNSS, so a worse fix
  // arriving after a better one is ordinary. Without this rule the marker
  // bounces back to the coast after having reached the water.
  await startWatch(page);
  await emit(page, NEARER);
  await emit(page, COAST);

  const seen = await record(page);
  expect(seen.updates.map((u) => u.accuracy)).toEqual([300]);
});

test("a fix that never improves still settles at the deadline", async ({ page }) => {
  await startWatch(page, { desiredAccuracyM: 50, settleDeadlineMs: 1200 });
  await emit(page, NEARER);

  await page.waitForFunction(() => window.__record.resolved !== null, null, { timeout: 5000 });
  const seen = await record(page);
  expect(seen.resolved.accuracy, "a coarse answer beats no answer").toBe(300);
  expect(seen.error, "the deadline is not a failure when something arrived").toBeNull();
  expect((await counters(page)).clears, "and the watch is released").toBe(1);
});

test("nothing at all is a timeout, not a hang", async ({ page }) => {
  await startWatch(page, { firstFixTimeoutMs: 400 });
  await page.waitForFunction(() => window.__record.error !== null, null, { timeout: 5000 });

  const seen = await record(page);
  expect(seen.error).toBe("timeout");
  expect((await counters(page)).clears).toBe(1);
});

test("an error before the first fix fails, an error after it does not", async ({ page }) => {
  await startWatch(page, { continuous: true, desiredAccuracyM: 50 });
  await emit(page, AT_SEA);
  await page.waitForFunction(() => window.__record.resolved !== null, null, { timeout: 5000 });

  // A boat loses and regains signal constantly. Tearing the watch down on the
  // first POSITION_UNAVAILABLE would make tracking useless exactly where it
  // is wanted.
  await page.evaluate(() => window.__geo.emitError(2));
  const seen = await record(page);
  expect(seen.errors, "reported, not fatal").toEqual(["unavailable"]);
  expect(seen.error, "the promise had already resolved").toBeNull();
  expect((await counters(page)).clears, "and the watch is still live").toBe(0);
});

// The phase boundary, which is the single most important thing in this
// milestone. Acquiring accepts only improvements; tracking must accept every
// plausible fix, because by then each one is a new *position* rather than a
// better guess at the same one. Keeping the improving-only rule past settle
// would freeze the marker at the first good fix forever.
test.describe("once tracking", () => {
  const settle = async (page) => {
    await startWatch(page, { continuous: true, desiredAccuracyM: 50 });
    await emit(page, AT_SEA);
    await page.waitForFunction(() => window.__record.resolved !== null, null, { timeout: 5000 });
  };

  test("a worse but plausible fix still moves you", async ({ page }) => {
    await settle(page);
    // Worse than the settled 18m, and accepted: this is the boat moving.
    await advanceClock(page, 4000);
    await emit(page, { lat: 54.5, lng: 10.6, accuracy: 60 });

    const seen = await record(page);
    const last = seen.updates.at(-1);
    expect(last.accuracy, "the improving-only rule must not survive settle").toBe(60);
    expect(last.lat).toBeCloseTo(54.5, 5);
  });

  test("one wild reading is ignored, a persistent one is believed", async ({ page }) => {
    await settle(page);
    const before = (await record(page)).updates.length;

    // A single sample dropping back to the towers must not teleport the
    // marker ashore.
    await advanceClock(page, 4000);
    await emit(page, COAST);
    expect((await record(page)).updates, "a lone 2800m fix is noise").toHaveLength(before);

    // But refusing forever would be its own lie: if the signal is genuinely
    // gone, showing the poor position with its large ring is the honest
    // answer, and a stale marker with a small ring is not.
    await advanceClock(page, 16000);
    await emit(page, COAST);
    const seen = await record(page);
    expect(seen.updates.length, "after the grace, it is believed").toBe(before + 1);
    expect(seen.updates.at(-1).accuracy).toBe(2800);
  });

  test("the watch keeps running past settle, and cancel releases it", async ({ page }) => {
    await settle(page);
    expect((await counters(page)).clears, "continuous means continuous").toBe(0);

    await page.evaluate(() => window.__handle.cancel());
    expect((await counters(page)).clears).toBe(1);
  });
});

test("a one-shot caller gets the old shape back", async ({ page }) => {
  // getCurrentPosition still resolves to a plain fix, so the distance filter
  // needs no knowledge of any of the above.
  await page.evaluate(async () => {
    const m = await import("/js/geolocation.js");
    window.__once = { done: null };
    m.getCurrentPosition().then((fix) => {
      window.__once.done = fix;
    });
  });
  await waitForWatch(page);
  await emit(page, NEARER);
  await page.waitForFunction(() => window.__once.done !== null, null, { timeout: 5000 });

  const fix = await page.evaluate(() => window.__once.done);
  // 300m settles it outright: the filter asks in kilometres, so waiting for a
  // GNSS lock to answer "within 5 km" would be absurd.
  expect(fix.accuracy).toBe(300);
  expect(fix.lat).toBeCloseTo(NEARER.lat, 5);
  expect((await counters(page)).clears, "and it does not leave a watch running").toBe(1);
});
