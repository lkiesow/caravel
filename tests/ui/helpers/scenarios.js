// Login and route resolution against the seeded scenarios.
//
// The suite drives the data `make dev-reset` seeds (see cmd/seed/main.go). Those
// rows have deterministic UUIDs, so routes are stable run to run — but we still
// resolve them by title rather than hard-coding UUIDs here, so the seed's
// namespace can change without editing the tests. If the seed hasn't been run,
// every spec fails with one clear message instead of a pile of confusing ones.
import { expect } from "@playwright/test";

export const DEMO_USER = { username: "demo", password: "demo1234" };
export const OTHER_USER = { username: "other", password: "other1234" };

// Where auth.setup.js parks the demo user's session for the rest of the run.
// Gitignored: it holds a live session token, and it is regenerated every run.
export const AUTH_STATE_FILE = "tests/ui/.auth/demo.json";

// Scenario name -> seeded trip title (cmd/seed/main.go's titlePrefix + title).
export const SCENARIO_TITLES = {
  full: "Demo: Iceland Ring Road",
  "one-pin": "Demo: Single Pin",
  "start-only": "Demo: Start Date Only",
  "year-boundary": "Demo: New Year Crossing",
  "no-dates": "Demo: No Dates Yet",
  "out-of-range-days": "Demo: Days Outside The Range",
  cascade: "Demo: Delete Me (Cascade)",
};

export const TRIP_TABS = ["locations", "map", "itinerary", "files", "checklists", "settings"];

export const VIEWPORTS = [
  { name: "desktop", width: 1280, height: 800 },
  // The user's phone's native resolution — the convention in CLAUDE.md.
  { name: "mobile", width: 324, height: 756 },
];

export const COLOR_SCHEMES = ["light", "dark"];

// Blocks every request that isn't to the app itself, and serves a 1x1 PNG in
// place of map tiles.
//
// Without this the Map route fetches a dozen tiles from tile.openstreetmap.org,
// which makes the suite slow, flaky, dependent on a third party being reachable,
// and rude to a free service — and in CI it would be the main reason for random
// failures. Tiles are replaced rather than merely aborted so Leaflet still lays
// out and still creates its markers, which is what the checks actually look at.
const TRANSPARENT_PNG = Buffer.from(
  "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNkYPhfDwAChwGA60e6kgAAAABJRU5ErkJggg==",
  "base64"
);

// Same resolution as playwright.config.js — not read off the context, whose
// baseURL is only exposed through private API.
export const APP_ORIGIN = new URL(process.env.CARAVEL_TEST_URL || "http://localhost:8080").origin;

// Tracks in-flight fetch() calls so a test can wait for the app to be *done*,
// not merely mounted.
//
// This exists because the obvious readiness check isn't one. Routes render a
// shell into #app immediately and fill it in when their fetches resolve, so
// "#app has children" is true well before the page has content — which made the
// heading spec report "no headings at all" on the location view page that in fact
// has a perfectly good h1/h2/h2 outline. Counting fetches is the app-level
// equivalent of networkidle, minus the map-tile noise (tiles are <img>, not
// fetch).
const FETCH_TRACKER = `
  window.__caravelFetches = { pending: 0, completed: 0 };
  const origFetch = window.fetch;
  window.fetch = function (...args) {
    window.__caravelFetches.pending++;
    return origFetch.apply(this, args).finally(() => {
      window.__caravelFetches.pending--;
      window.__caravelFetches.completed++;
    });
  };
`;

export async function installFetchTracker(page) {
  await page.addInitScript(FETCH_TRACKER);
}

export async function blockExternalRequests(page) {
  const appOrigin = APP_ORIGIN;
  await page.route("**/*", async (route) => {
    const url = new URL(route.request().url());
    if (url.origin === appOrigin || url.protocol === "data:" || url.protocol === "blob:") {
      return route.continue();
    }
    if (/\.(png|jpg|jpeg|webp)$/i.test(url.pathname)) {
      return route.fulfill({ status: 200, contentType: "image/png", body: TRANSPARENT_PNG });
    }
    return route.abort();
  });
}

// Prepares a page for the seeded app: request interception, the fetch tracker,
// and a first navigation.
//
// The demo user is *already* authenticated by the time this runs - the session
// cookie arrives with the browser context from auth.setup.js via storageState,
// one login for the whole run instead of one per spec (see that file for the
// 429 this fixes). The name stays `login` because that is what callers mean,
// and because any other user still logs in here: only the default one is cached.
export async function login(page, user = DEMO_USER) {
  await installFetchTracker(page);
  await blockExternalRequests(page);

  if (user !== DEMO_USER) {
    // page.request rather than an in-page fetch: it needs no loaded document,
    // and it shares the context's cookie jar, so the session applies to the
    // navigation below.
    const res = await page.request.post("/api/auth/login", { data: user });
    expect(
      res.status(),
      res.status() === 429
        ? `login as ${user.username} was rate limited (HTTP 429), not rejected — wait a minute and re-run`
        : `login as ${user.username} failed — has \`make dev-reset FORCE=1\` been run?`
    ).toBe(200);
  }

  await page.goto("/");
}

export async function fetchTrips(page) {
  return page.evaluate(async () => {
    const res = await fetch("/api/trips");
    if (!res.ok) throw new Error(`GET /api/trips returned ${res.status}`);
    return res.json();
  });
}

// Resolves scenario name -> trip id, failing loudly and specifically when the
// seed is missing.
export async function resolveScenarioTrips(page) {
  const trips = await fetchTrips(page);
  const byTitle = new Map(trips.map((t) => [t.title, t.id]));
  const resolved = {};
  const missing = [];
  for (const [scenario, title] of Object.entries(SCENARIO_TITLES)) {
    const id = byTitle.get(title);
    if (id) resolved[scenario] = id;
    else missing.push(`${scenario} (${title})`);
  }
  expect(
    missing,
    `seeded scenarios missing: ${missing.join(", ")}. Run \`make dev-reset FORCE=1\` first.`
  ).toEqual([]);
  return resolved;
}

// Every route the suite sweeps, as {path, label}. Built from web/js/app.js's
// route patterns with the :tripId / :itemId holes filled from the seed.
export async function buildRoutes(page) {
  const trips = await resolveScenarioTrips(page);
  const fullTrip = trips.full;

  const items = await page.evaluate(async (tripId) => {
    const res = await fetch(`/api/trips/${tripId}/items`);
    return res.json();
  }, fullTrip);
  expect(items.length, "the `full` seed scenario should have locations").toBeGreaterThan(0);
  const itemId = items[0].id;

  const routes = [
    { path: "/trips", label: "trips list" },
    { path: "/trips/new", label: "new trip" },
    { path: `/trips/${fullTrip}/locations/new`, label: "new location" },
    { path: `/trips/${fullTrip}/locations/${itemId}/edit`, label: "edit location" },
    { path: `/trips/${fullTrip}/locations/${itemId}`, label: "view location" },
  ];
  for (const tab of TRIP_TABS) {
    routes.push({ path: `/trips/${fullTrip}/${tab}`, label: `trip ${tab}` });
  }
  // The scenarios exist to be looked at, not just seeded — sweep each one's
  // itinerary, which is where date-shape differences actually show up.
  for (const [scenario, id] of Object.entries(trips)) {
    if (scenario === "full") continue;
    routes.push({ path: `/trips/${id}/itinerary`, label: `${scenario} itinerary` });
  }
  // Both ways to reach the not-found page (Stage 09 Milestone 5): a URL that
  // matches no route at all, and a well-formed one whose resource is gone.
  // They render the same page but through different paths - the router's
  // catch-all, and a page's own failed fetch - and it's a real screen now, so
  // it gets swept for overflow, headings and accessible names like any other.
  routes.push({ path: "/no-such-page", label: "not found (unmatched URL)" });
  routes.push({ path: `/trips/${MISSING_UUID}/locations`, label: "not found (missing trip)" });
  return routes;
}

// A syntactically valid UUID that is guaranteed not to be a real ID.
const MISSING_UUID = "00000000-0000-0000-0000-000000000000";

// Navigates and asserts we landed where we meant to.
//
// This assertion is not ceremony. It used to guard against the router
// silently redirecting any unmatched path to /trips, which made a typo'd
// route pass trivially against the wrong page — exactly what happened during
// Stage 04, where a manual sweep tested /trips for several milestones while
// believing it was testing something else. Stage 09 Milestone 5 replaced that
// redirect with a real not-found page, so a typo now shows up as a route with
// no content rather than as a false pass; keeping the assertion still catches
// the reverse mistake, a path that redirects somewhere on purpose ("/" -> "/trips").
export async function gotoRoute(page, path) {
  await page.goto(path);

  // Wait for the app to be *done*, not merely mounted: #app populated, at least
  // one fetch completed, and none still in flight. See FETCH_TRACKER above for
  // why the DOM check alone is not enough.
  await page.waitForFunction(
    () => {
      const app = document.getElementById("app");
      if (!app || app.children.length === 0 || /starting up/i.test(app.textContent)) return false;
      const f = window.__caravelFetches;
      return f && f.completed > 0 && f.pending === 0;
    },
    undefined,
    { timeout: 15000 }
  );
  // One frame for the render that follows the settled fetch.
  await page.evaluate(() => new Promise((r) => requestAnimationFrame(() => requestAnimationFrame(r))));

  const landed = await page.evaluate(() => window.location.pathname);
  expect(
    landed,
    `navigating to ${path} landed on ${landed} — this route pattern is probably wrong, or it redirects on purpose`
  ).toBe(path);
}
