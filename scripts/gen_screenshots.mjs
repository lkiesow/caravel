// Captures the documentation screenshots. Driven by scripts/gen_screenshots.sh,
// which owns the throwaway server this talks to -- run that, not this.
//
// Two jobs, in order. First "dress the set": the seeded data is built for the UI
// suite, so it is titled "Demo: ..." and describes itself as not real, and its
// images are deliberately-poor 343x200 crops of a test sheet. All of that is
// right for a test fixture and wrong for a screenshot, so this rewrites the copy
// through the API and uploads real photographs over the fixtures. Second, walk
// the app and save PNGs.
//
// The photographs are never committed and never published as files -- only the
// PNG captures are, and a PNG re-render carries none of a JPEG's metadata.
// (Caravel strips it anyway: every upload is re-encoded from decoded pixels, so
// EXIF, GPS included, never reaches disk. See internal/imaging.)
import { firefox } from "playwright";
import { readdir, mkdir } from "node:fs/promises";
import { readFileSync } from "node:fs";
import path from "node:path";

const BASE = process.env.CARAVEL_TEST_URL || "http://127.0.0.1:8099";
const OUT = process.env.SCREENSHOT_OUT || "docs/assets/screenshots";
const PHOTOS = process.env.SCREENSHOT_PHOTOS || "images";

const DESKTOP = { width: 1280, height: 860 };
// The mobile convention from CLAUDE.md: the author's phone's native resolution.
const MOBILE = { width: 324, height: 756 };

// Which photograph plays which part. Keyed by filename because the choice is
// about *content* -- a cathedral captioned "hut traverse" is the kind of
// mismatch a reader notices -- and content is not something this script can
// work out for itself.
//
// The photographs are not in the repository (they are the author's own and are
// deliberately not committed), so this mapping is a preference, not a
// requirement: anything not named here is assigned positionally, and a
// completely different photo directory still produces a full set of
// screenshots. Only the exact images in the committed ones depend on this.
const CASTING = {
  cover: "IMG20250714121210.jpg",       // northern archipelago, for the Iceland trip
  location: "IMG20220910200609.jpg",    // misty pinnacles, as a viewpoint
  trier: "IMG20240820213641.jpg",       // Trier cathedral at night
  tenerife: "IMG20250415102029.jpg",    // volcanic islet off Tenerife
};

async function photos() {
  try {
    const names = (await readdir(PHOTOS))
      .filter((n) => /\.(jpe?g|png)$/i.test(n))
      .sort();
    return names.map((n) => path.join(PHOTOS, n));
  } catch {
    return [];
  }
}

// Resolves a cast role to a file, falling back to the nth photo when the named
// one is not in this directory.
function cast(pics, role, nth) {
  const wanted = CASTING[role];
  const named = pics.find((p) => path.basename(p) === wanted);
  return named ?? pics[nth % pics.length];
}

// Every API call goes through this. The first version of this script did not
// check statuses, and two calls were failing with 400 in silence: the trip body
// field is `subtitle`, not `description`, and readJSON refuses unknown fields --
// so the set was never dressed and the screenshots showed "Demo: Iceland Ring
// Road" and a one-card trips list. A generator that half-works quietly is worse
// than one that stops.
async function must(what, promise) {
  const res = await promise;
  if (!res.ok()) {
    throw new Error(`${what}: HTTP ${res.status()} ${(await res.text()).slice(0, 200)}`);
  }
  return res;
}

// Uploads one image to a trip's media and returns the asset id.
async function uploadMedia(ctx, tripId, file) {
  const res = await must(`upload ${file}`, ctx.request.post(`/api/trips/${tripId}/media`, {
    multipart: {
      file: { name: path.basename(file), mimeType: "image/jpeg", buffer: readFileSync(file) },
    },
  }));
  return (await res.json()).id;
}

async function shoot(page, name, opts = {}) {
  // A short settle: the map needs its tiles and the images their decode, and a
  // screenshot taken mid-load is the one failure this script cannot detect
  // itself.
  await page.waitForLoadState("networkidle").catch(() => {});
  await page.waitForTimeout(opts.settle ?? 400);

  // Bring the tab content up. Without this every tab screenshot was the cover
  // banner and the trip title with a sliver of the actual feature underneath --
  // the map shot was one pin and a strip of coastline. Scrolling to the tab bar
  // rather than the panel keeps the tabs in frame, which is what tells a reader
  // where they are.
  if (opts.scrollTo) {
    await page.evaluate((sel) => {
      const el = document.querySelector(sel);
      if (el) window.scrollTo({ top: el.getBoundingClientRect().top + window.scrollY - 24 });
    }, opts.scrollTo);
    await page.waitForTimeout(250);
  }

  const file = path.join(OUT, `${name}.png`);
  if (opts.element) {
    // Clipped to one component rather than the viewport. For a card that sits at
    // the very bottom of a page, scrolling cannot bring it to the top -- the
    // page simply runs out -- so the balances shot was three-quarters empty
    // "Add an expense" form. An element screenshot is also just tighter for
    // something a paragraph is pointing at.
    await page.locator(opts.element).screenshot({ path: file, scale: "css" });
  } else {
    await page.screenshot({ path: file, fullPage: !!opts.fullPage, scale: "css" });
  }
  console.log(`  ${file}`);
}

const TABS = ".trip-tabs";

async function main() {
  await mkdir(OUT, { recursive: true });
  const pics = await photos();

  const browser = await firefox.launch();
  const ctx = await browser.newContext({ baseURL: BASE, viewport: DESKTOP, deviceScaleFactor: 2 });

  // Through the API: one request rather than a form fill, and login is rate
  // limited to 10/min/IP so plumbing should not spend that budget.
  await must("login", ctx.request.post("/api/auth/login", {
    data: { username: "demo", password: "demo1234" },
  }));

  const trips = await (await ctx.request.get("/api/trips")).json();
  const full = trips.find((t) => t.title.includes("Iceland"));
  if (!full) throw new Error("the seeded Iceland trip is missing — did the seed run?");

  console.log("dressing the set");

  // The seeded title and subtitle announce themselves as test data ("Demo: ..."
  // and "nothing here is real"), which is right for a fixture and wrong for a
  // screenshot. Note the field is `subtitle`; there is no `description`.
  await must("retitle the trip", ctx.request.patch(`/api/trips/${full.id}`, {
    data: {
      title: "Iceland Ring Road",
      start_date: full.start_date,
      end_date: full.end_date,
      subtitle: "Anticlockwise from Keflavik, chasing waterfalls.",
    },
  }));

  if (pics.length) {
    // A cover photo, a photo on the first location, and the rest uploaded as
    // trip files so the Files tab shows a gallery rather than three text files.
    const coverId = await uploadMedia(ctx, full.id, cast(pics, "cover", 0));
    await must("set the cover photo", ctx.request.put(`/api/trips/${full.id}/preview-image`, {
      data: { media_asset_id: coverId },
    }));

    const items = await (await ctx.request.get(`/api/trips/${full.id}/items`)).json();
    const kirkjufell = items.find((i) => i.title.includes("Kirkjufell")) || items[0];
    if (kirkjufell) {
      const itemImageId = await uploadMedia(ctx, full.id, cast(pics, "location", 1));
      await must("set the location image", ctx.request.put(`/api/items/${kirkjufell.id}/image`, {
        data: { media_asset_id: itemImageId },
      }));
    }

    // The rest become trip files, so the Files tab shows a gallery rather than
    // the seeder's three text files. Skipping the ones already cast above, so a
    // photo does not appear twice in one trip.
    const alreadyCast = new Set(Object.values(CASTING));
    // Renamed on the way in. The camera's own filenames (IMG20251027211809.jpg)
    // read as test data in a screenshot and put the capture date in the frame;
    // these are the names a trip's shared photos would actually have.
    const asUploaded = [
      { name: "kirkjufell-sunrise.jpg", note: "Worth the 05:30 alarm." },
      { name: "route-1-north.jpg", note: "" },
      { name: "godafoss.jpg", note: "Spray got everywhere — dry the lens next time." },
      { name: "harbour-reykjavik.jpg", note: "" },
      { name: "last-evening.jpg", note: "" },
    ];
    const spare = pics.filter((f) => !alreadyCast.has(path.basename(f)));
    for (const [i, file] of spare.slice(0, asUploaded.length).entries()) {
      const { name, note } = asUploaded[i];
      await must(`file upload ${file}`, ctx.request.post(`/api/trips/${full.id}/files`, {
        multipart: {
          file: { name, mimeType: "image/jpeg", buffer: readFileSync(file) },
          visibility: "trip",
          ...(note ? { note } : {}),
        },
      }));
    }
    console.log(`  ${Math.min(pics.length, 7)} photo(s) placed`);
  }

  // A couple more trips, so the trips list is a list rather than one card. The
  // dates are relative, so the list keeps showing an upcoming and a past trip
  // whenever this is re-run.
  const year = new Date().getUTCFullYear();
  // The seeded expenses are dated relative to the seeder's own reference, which
  // puts them months outside the trip's window -- a June receipt on an August
  // trip is exactly the detail that makes a screenshot look like test data.
  // Re-dated through the API rather than by changing the seeder, which the UI
  // suite depends on.
  if (full.start_date) {
    const members = await (await ctx.request.get(`/api/trips/${full.id}/members`)).json();
    const memberCount = (members.members ?? members).length;
    const ledger = await (await ctx.request.get(`/api/trips/${full.id}/expenses`)).json();
    const rows = [...(ledger.expenses ?? [])].reverse();
    for (const [i, expense] of rows.entries()) {
      const on = new Date(`${full.start_date}T00:00:00Z`);
      on.setUTCDate(on.getUTCDate() + i);
      // share_user_ids always comes back as the *effective* set -- everyone on
      // the trip when no shares are stored -- so echoing it back verbatim would
      // silently convert a "split with everyone" expense into one pinned to
      // today's members, and the subset row that makes the balances worth
      // showing would stop being distinguishable. Forward it only when it is a
      // genuine subset.
      const shares = expense.share_user_ids ?? [];
      const isSubset = shares.length > 0 && shares.length < memberCount;
      await must(`re-date ${expense.title}`, ctx.request.patch(`/api/expenses/${expense.id}`, {
        data: {
          title: expense.title,
          amount_minor: expense.amount_minor,
          spent_on: on.toISOString().slice(0, 10),
          payer_user_id: expense.payer_user_id ?? null,
          ...(isSubset ? { share_user_ids: shares } : {}),
        },
      }));
    }
  }

  // Named after what is actually in their cover photographs, rather than
  // inventing a destination and pairing it with an unrelated picture.
  const extras = [
    { role: "tenerife", title: "Tenerife: the north coast",
      start_date: `${year}-04-05`, end_date: `${year}-04-16`,
      subtitle: "Anaga, Garachico, and as little of the south as possible." },
    { role: "trier", title: "Trier and the Moselle",
      start_date: `${year + 1}-08-15`, end_date: `${year + 1}-08-21`,
      subtitle: "Roman ruins, steep vineyards, and a slow river." },
  ];
  for (const [i, { role, ...trip }] of extras.entries()) {
    const res = await must(`create ${trip.title}`, ctx.request.post("/api/trips", { data: trip }));
    if (pics.length) {
      const created = await res.json();
      const id = await uploadMedia(ctx, created.id, cast(pics, role, i + 2));
      await must("set cover", ctx.request.put(`/api/trips/${created.id}/preview-image`,
        { data: { media_asset_id: id } }));
    }
  }

  console.log("capturing");
  const page = await ctx.newPage();

  await page.goto("/trips");
  await shoot(page, "trips-list");

  await page.goto(`/trips/${full.id}`);
  await shoot(page, "trip-overview");

  await page.goto(`/trips/${full.id}/locations`);
  await shoot(page, "locations", { scrollTo: TABS });

  const items = await (await ctx.request.get(`/api/trips/${full.id}/items`)).json();
  const kirkjufell = items.find((i) => i.title.includes("Kirkjufell")) || items[0];
  await page.goto(`/trips/${full.id}/locations/${kirkjufell.id}`);
  await shoot(page, "location-detail");

  await page.goto(`/trips/${full.id}/map`);
  // Tiles come from a third party and are the slowest thing here.
  await shoot(page, "map", { settle: 2500, scrollTo: TABS });

  await page.goto(`/trips/${full.id}/itinerary`);
  await shoot(page, "itinerary", { scrollTo: TABS });

  await page.goto(`/trips/${full.id}/files`);
  await shoot(page, "files", { scrollTo: TABS });

  await page.goto(`/trips/${full.id}/checklists`);
  await shoot(page, "checklists", { scrollTo: TABS });

  await page.goto(`/trips/${full.id}/expenses`);
  await shoot(page, "expenses", { scrollTo: TABS });
  await shoot(page, "balances", { element: ".expenses__summary-card" });

  await page.goto(`/trips/${full.id}/members`);
  await shoot(page, "members", { scrollTo: TABS });

  // The assistant, mid-review. This one needs a real interaction: the panel only
  // has anything in it after a run, so the button is pressed and the suggestion
  // rows are waited for. The provider is the in-process stub
  // (CARAVEL_LLM_URL=stub), so no model is called and no key is needed -- the
  // same fake the UI suite uses.
  await page.goto(`/trips/${full.id}/locations/new`);
  // The editor renders asynchronously, so the panel has to be waited for rather
  // than probed for -- a bare count() immediately after goto() found nothing and
  // silently skipped this shot.
  const assistRun = page.getByRole("button", { name: "Search via AI" });
  const assistReady = await assistRun
    .waitFor({ state: "visible", timeout: 15_000 })
    .then(() => true)
    .catch(() => false);
  if (assistReady) {
    // The prompt matches what the stub actually answers with (see
    // internal/assist/stub.go). The fake returns the same canned place whatever
    // it is asked, so asking about a waterfall and being offered a hostel put a
    // visible non-sequitur in the screenshot.
    await page.getByPlaceholder(/Describe the place/).fill("Kex Hostel, Reykjavik");
    await assistRun.click();
    // The run streams; the accept/reject rows are the thing worth showing.
    await page.locator(".assist__bar").waitFor({ state: "visible", timeout: 60_000 }).catch(() => {});
    // The viewport, not the panel: the suggestions themselves are placed into
    // the [data-assist-field] slots scattered through the form, so a shot
    // clipped to .assist shows "6 suggestions" and none of them.
    await shoot(page, "assistant", { scrollTo: ".assist", settle: 800 });
  } else {
    console.log("  (skipped assistant: the panel did not render — is CARAVEL_LLM_URL set?)");
  }

  // A few phone captures, where the layout is genuinely different rather than
  // simply narrower.
  await page.setViewportSize(MOBILE);
  await page.goto(`/trips/${full.id}/map`);
  await shoot(page, "mobile-map", { settle: 2500, scrollTo: TABS });
  await page.goto(`/trips/${full.id}/itinerary`);
  await shoot(page, "mobile-itinerary", { scrollTo: TABS });
  await page.goto("/trips");
  await shoot(page, "mobile-trips-list");

  await browser.close();
}

main().catch((err) => {
  console.error(`screenshots: ${err.message}`);
  process.exit(1);
});
