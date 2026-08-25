// "Search for an image": the picker, and the one image feature with no LLM in
// it.
//
// # What is faked, and what is not
//
// CARAVEL_WIKIMEDIA_URL=stub starts an in-process fixture encyclopaedia that
// answers the two API calls the search makes and serves real PNGs from the
// URLs it returns (internal/wikimedia/stub.go). Only the encyclopaedia is
// faked: the endpoint, the authorization, the limiter, the filter that keeps
// icons out, the grid, the hotlinked thumbnails and the store-on-pick all run
// for real. Picking a result really does fetch and store an image.
//
// The second group comes from CARAVEL_SEARCH_PROVIDER=stub, which offers one
// picture that loads and one that does not -- the second pointing at
// example.invalid, which cannot resolve. That is how the "a dead thumbnail
// must not leave an invisible cell" case gets tested at all.
//
// # Isolation
//
// Writes, so it owns its trip: created in beforeEach, deleted in afterEach.
import { test, expect } from "@playwright/test";
import { login } from "./helpers/scenarios.js";

test.describe("image search", () => {
  let tripId;

  test.beforeEach(async ({ page }) => {
    await login(page);

    const me = await (await page.request.get("/api/auth/me")).json();
    test.skip(!me.image_search, "needs a server started with CARAVEL_WIKIMEDIA_URL=stub");

    // The suite's blanket interceptor answers *any* off-origin .png/.jpg with a
    // 1x1 transparent PNG, so map tiles never reach a third party. That is
    // exactly wrong here: this spec is about what happens when a thumbnail does
    // not load, and a stand-in that always loads makes the dead one look alive.
    // These two handlers are added after login(), so they win - Playwright runs
    // the most recently registered route first.
    //
    // The fixture encyclopaedia is on loopback and is ours, so its images go
    // through for real (including the one it deliberately 404s); example.invalid
    // is what a host that cannot be reached looks like, so it is refused rather
    // than quietly substituted.
    await page.route("**/img/Stub_*.png", (route) => route.continue());
    await page.route("**/example.invalid/**", (route) => route.abort());

    const res = await page.request.post("/api/trips", { data: { title: "UI suite: image search" } });
    expect(res.status(), "create the spec's own trip").toBe(201);
    tripId = (await res.json()).id;
  });

  test.afterEach(async ({ page }) => {
    if (tripId) await page.request.delete(`/api/trips/${tripId}`);
    tripId = null;
  });

  test("finds pictures, labels where each came from, and stores the one you pick", async ({ page }) => {
    await page.goto(`/trips/${tripId}/locations/new`);
    await page.locator('input[name="title"]').fill("Stub Article");

    const panel = page.locator(".image-search");
    await expect(panel).toBeHidden();

    await page.locator('[data-action="search-image"]').click();

    // Seeded from the title already typed, and searched without a second
    // press: a control that opens with the answer filled in and then waits is
    // asking for a press it does not need.
    await expect(panel.locator('input[name="q"]')).toHaveValue("Stub Article");

    // Two groups, each named. The labelling is the point: one source can say
    // what may be done with its pictures and the other cannot.
    const groups = panel.locator(".image-search__group");
    await expect(groups).toHaveCount(2, { timeout: 30_000 });
    await expect(groups.nth(0).locator("h4")).toHaveText("From Wikipedia");
    await expect(groups.nth(1).locator("h4")).toHaveText(/From the web/);
    await expect(groups.nth(1).locator(".image-search__note")).toBeVisible();
    // ...and the group that can credit its pictures does not carry that
    // warning, or it would say nothing at all.
    await expect(groups.nth(0).locator(".image-search__note")).toHaveCount(0);

    // Both groups are offered three and two candidates, and in each exactly
    // one points at a file nothing serves. Those cells remove themselves
    // rather than staying as invisible things that still click -- the bug the
    // image field's own preview already had once.
    const wikiResults = groups.nth(0).locator(".image-search__result");
    const webResults = groups.nth(1).locator(".image-search__result");
    await expect(wikiResults).toHaveCount(2);
    await expect(webResults).toHaveCount(1);

    // Every cell still standing holds a picture that actually loaded, not an
    // empty box: a zero-width img is exactly what a dead one looked like.
    for (const cell of [...(await wikiResults.all()), ...(await webResults.all())]) {
      expect(await cell.locator("img").evaluate((el) => el.naturalWidth)).toBeGreaterThan(0);
    }

    // What each group can honestly say about its pictures. A Wikipedia result
    // states its licence; a web result states only the host it was found on,
    // because that is the whole of what a search engine knows.
    await expect(wikiResults.first().locator(".image-search__meta")).toHaveText(/CC BY/);
    await expect(webResults.first().locator(".image-search__meta")).toHaveText("example.invalid");

    // Picking one closes the picker and sets the field. The image is fetched
    // and stored server-side, so what ends up on the page is served by us --
    // nothing on a saved page is hotlinked.
    await wikiResults.first().click();
    await expect(panel).toBeHidden();
    const preview = page.locator(".image-field__preview");
    await expect(preview).toBeVisible();

    // A brand-new location has nowhere to attach an asset yet, so the pick is
    // staged and flushed on Save -- the same path a pasted URL takes.
    await page.locator('[data-action="save"]').click();
    await expect(page).toHaveURL(/\/locations\/[0-9a-f-]+$/, { timeout: 15_000 });
    const saved = page.locator(".location-view img").first();
    await expect(saved).toBeVisible();
    await expect(saved).toHaveAttribute("src", /^\/api\/media\//);
    // And the credit travelled with it, which is the whole reason the
    // Wikipedia half exists.
    await expect(page.locator(".image-credit")).toContainText("Photographer");
  });

  test("says so when there is nothing to offer", async ({ page }) => {
    await page.goto(`/trips/${tripId}/locations/new`);
    await page.locator('[data-action="search-image"]').click();

    const panel = page.locator(".image-search");
    await panel.locator('input[name="q"]').fill("nothing at all");
    await panel.locator('.image-search__form button[type="submit"]').click();

    await expect(panel.locator(".image-search__status")).toHaveText("No images found. Try a different search term.");
    await expect(panel.locator(".image-search__result")).toHaveCount(0);
  });
});
