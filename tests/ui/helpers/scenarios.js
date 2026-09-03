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
// The account settings.spec.js is allowed to break: changing a password deletes
// every session that account holds, so the spec that does it must not share an
// account with a spec that holds a saved session (sharing.spec.js holds one for
// OTHER_USER). Kept deliberately free of trips and memberships.
export const PASSWORD_USER = { username: "pwtest", password: "pwtest1234" };

// Where auth.setup.js parks the saved sessions for the rest of the run.
// Gitignored: they hold live session tokens, and they are regenerated every run.
//
// scripts/ui_test.sh points this at its own temp directory. It has to: the
// files hold cookies for that run's server, cookies are not scoped by port, and
// two runs sharing one directory would hand each other a token their own server
// has never issued -- every spec then failing as if logged out.
const AUTH_DIR = process.env.CARAVEL_TEST_AUTH_DIR || "tests/ui/.auth";
export const AUTH_STATE_FILE = `${AUTH_DIR}/demo.json`;
// A second saved session, for the specs that need two people looking at the
// same trip (Stage 14 Milestone 9). Saved once per run for the same reason the
// first one is: login is limited to 10/min/IP, and a spec that switched users by
// logging in would spend that budget on plumbing.
export const OTHER_AUTH_STATE_FILE = `${AUTH_DIR}/other.json`;

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

export const TRIP_TABS = ["locations", "map", "itinerary", "checklists", "files", "expenses", "members", "settings"];

export const VIEWPORTS = [
  { name: "desktop", width: 1280, height: 800 },
  // The user's phone's native resolution — the convention in CLAUDE.md.
  { name: "mobile", width: 324, height: 756 },
];

export const COLOR_SCHEMES = ["light", "dark"];

// Blocks every request that isn't to the app itself, and serves a 1x1 PNG in
// place of any off-origin image.
//
// Without this the Map route fetches map data from whichever provider the
// instance is configured with, which makes the suite slow, flaky, dependent on
// a third party being reachable, and rude to a free service — and in CI it
// would be the main reason for random failures.
//
// Since Stage 30 the default provider is vector rather than raster, so what
// gets blocked has changed shape: tiles are .pbf fetches made from MapLibre's
// worker, and the glyphs and sprites the labels need are off-origin too. All
// of them are aborted. That is deliberately fine — a source whose fetch fails
// is marked loaded rather than left pending (MapLibre does this on purpose),
// so the map still fires `load`, still lays out, still places its markers and
// still answers questions about the camera. What the suite sees is a map with
// correct geometry and no cartography drawn under it, which is exactly the
// part every assertion here reads. The vendored style itself is same-origin
// and loads for real.
//
// The PNG fulfilment stays for off-origin images generally (photo fixtures,
// and a raster tile provider if an operator's config is under test).
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
// equivalent of networkidle.
//
// Map tiles used to be exempt for free, because raster tiles are <img> and not
// fetch. Vector tiles are fetch — but they are issued from MapLibre's worker,
// which has its own unpatched fetch, so they still do not reach this counter.
// The style document does, and should: it is same-origin and the map is not
// ready without it.
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

// Opens a second browser context as another user, from their saved session.
//
// For the specs where the point *is* that two people see different things: one
// page stays the suite's usual demo session and this one is somebody else,
// looking at the same trip at the same time. Costs no login — the session comes
// from auth.setup.js — and the caller closes the context when done.
//
// Applies the same two page-level hooks login() does, since a second page needs
// the fetch tracker for gotoRoute and the external-request block for map tiles
// just as much as the first.
export async function openAs(browser, storageStateFile, viewport) {
  const context = await browser.newContext(viewport ? { storageState: storageStateFile, viewport } : { storageState: storageStateFile });
  const page = await context.newPage();
  await installFetchTracker(page);
  await blockExternalRequests(page);
  return { context, page };
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
    { path: "/settings", label: "account settings" },
    // Swept as the seeded demo user, who is an administrator — so this route
    // renders the real screen rather than its not-found fallback.
    { path: "/admin", label: "administration" },
    { path: "/trips/new", label: "new trip" },
    { path: `/trips/${fullTrip}/locations/new`, label: "new location" },
    { path: `/trips/${fullTrip}/locations/${itemId}/edit`, label: "edit location" },
    { path: `/trips/${fullTrip}/locations/${itemId}`, label: "view location" },
    // Renders the real page only where the server has an assistant, which
    // with_server.sh arranges; elsewhere it is the not-found page, which is
    // still a page and still worth sweeping.
    { path: `/trips/${fullTrip}/suggest`, label: "suggest locations" },
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
  // A map is lazily imported *after* its route's fetches settle, so the
  // condition above can be true while the library is still loading. Without
  // this the sweeps intermittently measured a half-built map and reported its
  // un-sized controls as content overflowing .map-wrap - a failure that only
  // ever appeared under a full parallel run. map-view.js sets data-ready on
  // itself once the map has laid out - since Stage 30 on the map's own `load`
  // event, meaning the style is parsed and the first frame is drawn, which is
  // a stronger guarantee than the attribute used to carry.
  await page.waitForFunction(
    () => [...document.querySelectorAll("map-view")].every((el) => el.hasAttribute("data-ready")),
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
