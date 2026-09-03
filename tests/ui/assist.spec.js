// The AI assistant, end to end against the stub provider.
//
// # What makes this testable at all
//
// CARAVEL_LLM_URL=stub selects an in-process fake that returns a scripted
// sequence of tool calls and then a structured answer. Only the outbound HTTP
// call to a model is faked: the agent loop, the tool dispatch, the validation,
// the link liveness checks, the geocoding and the SSE transport all run for
// real. So a pass here means the whole pipeline works, not that a fixture
// rendered.
//
// # How the links and sources lists get here
//
// They used to be untestable. The stub's URLs pointed at example.invalid, so
// the proposed link was correctly dropped as dead and the failed page fetch
// recorded no source -- both right, and together they meant neither list could
// ever appear in this spec. A counting bug in the sources list shipped because
// of exactly that.
//
// The stub now starts its own fixture host on loopback and the fetcher is
// given an allowlist holding that one address. It is a narrow weakening of the
// SSRF guard and internal/assist/stub_fixture.go says so at length; what it
// buys is that a run here produces a real link that survived the liveness
// check and a real list of the pages it read.
//
// # Isolation
//
// This spec writes, so it follows the shape files.spec.js established: its own
// trip, created in beforeEach and deleted in afterEach, so the shared seeded
// scenarios every other spec depends on are never touched.
import { test, expect } from "@playwright/test";
import { login } from "./helpers/scenarios.js";

test.describe("AI assistant", () => {
  let tripId;

  test.beforeEach(async ({ page }) => {
    await login(page);

    // The capability is server configuration, not seed data — the first thing
    // in this suite that is. Skipping rather than failing is deliberate: a
    // developer running `make test-ui` against their ordinary dev server has
    // not broken anything, and a red suite that means "you did not set an env
    // var" trains people to ignore red suites.
    const me = await (await page.request.get("/api/auth/me")).json();
    test.skip(!me.capabilities.assist, "needs a server started with CARAVEL_LLM_URL=stub");

    const res = await page.request.post("/api/trips", { data: { title: "UI suite: assist spec" } });
    expect(res.status(), "create the spec's own trip").toBe(201);
    tripId = (await res.json()).id;
  });

  test.afterEach(async ({ page }) => {
    if (tripId) await page.request.delete(`/api/trips/${tripId}`);
    tripId = null;
  });

  test("builds a location from a prompt, one suggestion at a time", async ({ page }) => {
    await page.goto(`/trips/${tripId}/locations/new`);

    const panel = page.locator(".assist");
    const status = page.locator(".assist__status");
    const bar = page.locator(".assist__bar");
    const error = page.locator(".assist__error");

    await expect(panel).toBeVisible();

    // Idle: no spinner, no Cancel, no bulk actions. This is the state that
    // shipped broken — .assist__status sets display:flex, which beats the UA's
    // [hidden] rule — so it is asserted before anything else happens.
    await expect(status).toBeHidden();
    await expect(bar).toBeHidden();
    await expect(error).toBeHidden();

    // Prompt mode with an empty prompt is refused in the browser, without a
    // request: a run with nothing to look for would spend real money finding
    // that out.
    await page.locator('[data-action="assist-run"]').click();
    await expect(error).toBeVisible();
    await expect(status).toBeHidden();

    await page.locator(".assist__prompt").fill("a cheap hostel near Hallgrimskirkja");
    await page.locator('[data-action="assist-run"]').click();

    // Suggestions land in the slot under the field each one is about, which is
    // the whole point of the layout: a suggested title three cards from the
    // title box cannot be compared with what is in the box.
    const titleSuggestion = page.locator('[data-assist-field="title"] .assist-suggestion');
    await expect(titleSuggestion).toBeVisible({ timeout: 60_000 });
    await expect(error).toBeHidden();
    // The run is over, so the spinner and its Cancel have gone again.
    await expect(status).toBeHidden();

    for (const field of ["title", "category", "tags", "notes", "address", "coordinates", "links", "cover"]) {
      await expect(
        page.locator(`[data-assist-field="${field}"] .assist-suggestion`),
        `a suggestion under ${field}`
      ).toHaveCount(1);
    }
    await expect(bar).toBeVisible();
    await expect(page.locator(".assist__count")).toHaveText("8 suggestions");

    // The pages the run actually read, listed so the proposal can be judged.
    // Not a suggestion: there is nothing here to accept, and counting it as
    // one is what used to leave the bar stuck on "1 suggestion" forever.
    const sources = page.locator(".assist-sources");
    await expect(sources).toBeVisible();
    await expect(sources.locator("li")).toHaveCount(2);
    await expect(sources.locator("li a").first()).toHaveText(/Kex Hostel/);

    // Accepting fills the field and takes the suggestion away, because it has
    // become the field above it.
    await titleSuggestion.getByRole("button", { name: "Accept" }).click();
    await expect(page.locator('input[name="title"]')).toHaveValue("Kex Hostel");
    await expect(titleSuggestion).toHaveCount(0);
    await expect(page.locator(".assist__count")).toHaveText("7 suggestions");

    // Rejecting takes it away and leaves the field alone. Tags are the case
    // where "the field" is not an input: the proposal is one comma-separated
    // string and the editor splits it into chips, so rejecting must leave the
    // chip list empty rather than an input blank.
    const tagsSuggestion = page.locator('[data-assist-field="tags"] .assist-suggestion');
    await tagsSuggestion.getByRole("button", { name: "Reject" }).click();
    await expect(page.locator(".tag-field__chip")).toHaveCount(0);
    await expect(tagsSuggestion).toHaveCount(0);
    await expect(page.locator(".assist__count")).toHaveText("6 suggestions");

    // Coordinates go through the Location card's own handler, so the map
    // marker moves exactly as it does when a pin is dragged.
    await page.locator('[data-assist-field="coordinates"] .assist-suggestion')
      .getByRole("button", { name: "Accept" }).click();
    await expect(page.locator('.location-form [name="lat"]')).not.toHaveValue("");
    await expect(page.locator("map-view")).toHaveAttribute("lat", /\d/);

    // The cover: the one suggestion whose value cannot be judged as text, so
    // it shows the picture rather than a URL, with where it came from.
    const cover = page.locator('[data-assist-field="cover"] .assist-cover');
    await expect(cover.locator("img")).toBeVisible();
    await expect(cover.locator("img")).toHaveAttribute("alt", "");
    await expect(cover.locator(".assist-cover__meta a")).toHaveText(/127\.0\.0\.1:\d+/);

    // Accept the rest in one go, and the bar retires itself. This is the
    // regression the milestone exists for: the sources box used to sit in the
    // counted list with an accept that removed its node without forgetting the
    // entry, so the count floored at one and the bar never went away.
    await page.locator('[data-action="assist-accept-all"]').click();
    await expect(page.locator(".assist-suggestion")).toHaveCount(0);
    await expect(bar).toBeHidden();
    await expect(page.locator('select[name="category"]')).toHaveValue("stay");
    await expect(page.locator('textarea[name="notes"]')).not.toHaveValue("");
    // The accepted link is in the form's own list now.
    await expect(page.locator(".link-list a", { hasText: "Official site" })).toHaveCount(1);
    // Accepting the suggestions does not throw away the account of where they
    // came from: the sources are an explanation, not an outstanding decision.
    await expect(sources).toBeVisible();

    // The run trace: what the assistant actually did. Closed by default -- it
    // explains the suggestions rather than competing with them -- and always
    // present, with no setting gating it.
    const trace = page.locator(".assist-trace");
    await expect(trace).toBeVisible();
    await expect(trace).not.toHaveAttribute("open", /.*/);
    await expect(trace.locator(".assist-trace__title")).toHaveText("What the assistant did");

    // The heading counts what the list holds. A trace that contradicts itself
    // on its own first line is worse than no trace.
    const rows = trace.locator(".assist-trace__step");
    const rowCount = await rows.count();
    expect(rowCount, "a step per model turn and per tool call").toBeGreaterThanOrEqual(3);
    const meta = trace.locator(".assist-trace__meta");
    await expect(meta).toContainText(`${rowCount} steps`);
    // A number, not a literal "{count}". t() used to drop a `count` supplied
    // in params whenever the plural argument was absent, which is exactly the
    // shape this line uses.
    await expect(meta).toContainText(/\d+ tokens/);

    await trace.locator("summary").click();
    await expect(trace).toHaveAttribute("open", /.*/);

    // The stub reads two pages, so the trace names both hosts it fetched --
    // the whole point of the list is that it says what was actually read.
    await expect(rows.filter({ hasText: "Read " })).toHaveCount(2);
    // Every row carries a duration, which is what makes it a trace rather than
    // a list of things that happened.
    for (const ms of await trace.locator(".assist-trace__ms").allTextContents()) {
      expect(ms, "a step duration").toMatch(/^[\d.]+ s$/);
    }

    // Accepting the cover fills the image field, exactly as pasting a URL
    // into that card would -- including on a location that does not exist yet,
    // where the pick is staged until Save.
    await expect(page.locator(".image-field__preview")).toBeVisible();

    // Nothing has been written yet. Saving is what commits, exactly as if
    // every field had been typed.
    await page.locator('[data-action="save"]').click();
    await expect(page).toHaveURL(new RegExp(`/trips/${tripId}/locations/[0-9a-f-]+$`));
    await expect(page.locator("h1")).toHaveText("Kex Hostel");

    // The cover survived the staged-upload flush and is served from this
    // instance rather than hotlinked.
    await expect(page.locator(".location-view__image")).toHaveAttribute("src", /^\/api\/media\//);
  });

  // The credit, which is the whole reason the provenance columns exist: a
  // freely licensed photograph is not an unencumbered one, and an image saved
  // with no record of whose it is cannot be credited afterwards.
  test("shows the credit for an image that came with one", async ({ page }) => {
    const itemRes = await page.request.post(`/api/trips/${tripId}/items`, {
      data: { title: "Heger Tor", category: "site", tags: ["landmark"] },
    });
    const itemId = (await itemRes.json()).id;

    // The image itself comes from the assistant's own fixture host, which is
    // the only image this suite can reach; the provenance is what a Wikimedia
    // cover would carry.
    const me = await (await page.request.get("/api/auth/me")).json();
    void me;
    const runRes = await page.request.post(`/api/trips/${tripId}/assist/location`, {
      data: { mode: "enrich", title: "Kex Hostel", locale: "en" },
    });
    const stream = await runRes.text();
    const proposal = JSON.parse(
      stream.split("\n").filter((l) => l.startsWith("data:")).map((l) => l.slice(5).trim()).at(-1)
    );
    expect(proposal.cover?.url, "the stub proposes a cover").toBeTruthy();

    const media = await page.request.post(`/api/trips/${tripId}/media/url`, {
      data: {
        url: proposal.cover.url,
        source_url: "https://de.wikipedia.org/wiki/Waterloo-Tor",
        credit: "MrsMyer",
        license: "CC BY-SA 3.0",
      },
    });
    expect(media.status()).toBe(201);
    const assetId = (await media.json()).id;
    expect((await page.request.put(`/api/items/${itemId}/image`, { data: { media_asset_id: assetId } })).status()).toBe(200);

    await page.goto(`/trips/${tripId}/locations/${itemId}`);
    const credit = page.locator(".image-credit");
    await expect(credit).toBeVisible();
    await expect(credit).toContainText("MrsMyer");
    await expect(credit).toContainText("CC BY-SA 3.0");
    // Linked to the page it came from, which is what makes it an attribution
    // rather than a caption.
    await expect(credit.locator("a")).toHaveAttribute("href", "https://de.wikipedia.org/wiki/Waterloo-Tor");
  });

  test("marks an overwrite, and rejecting one leaves the text alone", async ({ page }) => {
    // A location with notes somebody wrote by hand: the case the whole
    // per-field review exists to protect.
    const handwritten = "My own note, written from memory. Must not vanish.";
    const res = await page.request.post(`/api/trips/${tripId}/items`, {
      data: { title: "Kex Hostel", category: "site", tags: ["guesthouse"], notes: handwritten },
    });
    expect(res.status()).toBe(201);
    const itemId = (await res.json()).id;

    await page.goto(`/trips/${tripId}/locations/${itemId}/edit`);
    await page.locator('[data-action="assist-run"]').click();

    const notesSuggestion = page.locator('[data-assist-field="notes"] .assist-suggestion');
    await expect(notesSuggestion).toBeVisible({ timeout: 60_000 });

    // Marked as an overwrite, and saying so in words as well as in colour.
    await expect(notesSuggestion).toHaveClass(/assist-suggestion--overwrite/);
    await expect(notesSuggestion.locator(".assist-suggestion__badge")).toBeVisible();

    // The location is already named, so no title is proposed: renaming is not
    // enrichment.
    await expect(page.locator('[data-assist-field="title"] .assist-suggestion')).toHaveCount(0);

    // Tags are the exception to the overwrite marking, because they are a set:
    // the proposal is what is already there plus what the run found, so
    // accepting it adds and never removes. Marked as an overwrite it would be
    // claiming to replace a tag it in fact keeps.
    const tagsSuggestion = page.locator('[data-assist-field="tags"] .assist-suggestion');
    await expect(tagsSuggestion.locator(".assist-suggestion__value")).toHaveText(
      "guesthouse, hostel, harbour, city centre",
    );
    await expect(tagsSuggestion).not.toHaveClass(/assist-suggestion--overwrite/);

    // Dismiss all applies nothing at all, and takes the sources with it: they
    // belong to a proposal nobody is looking at any more. The trace stays --
    // the run still happened, and "why was that useless?" is the likeliest
    // question at exactly this moment.
    await expect(page.locator(".assist-sources")).toBeVisible();
    await page.locator('[data-action="assist-dismiss-all"]').click();
    await expect(page.locator(".assist-suggestion")).toHaveCount(0);
    await expect(page.locator(".assist-sources")).toHaveCount(0);
    await expect(page.locator(".assist-trace")).toBeVisible();
    await expect(page.locator(".assist__bar")).toBeHidden();
    await expect(page.locator('textarea[name="notes"]')).toHaveValue(handwritten);

    // And the note is still intact in the database, not merely on screen.
    const after = await (await page.request.get(`/api/items/${itemId}`)).json();
    expect(after.notes, "the handwritten note survives a dismissed proposal").toBe(handwritten);
  });

  test("is absent entirely when the server has no assistant", async ({ page }) => {
    // Nothing to assert against a stub-enabled server, so this one proves the
    // other half: the client hides the control on the capability alone. The
    // capability is faked off rather than a second server being started.
    await page.route("**/api/auth/me", async (route) => {
      const response = await route.fetch();
      const body = await response.json();
      // The nested object has to be spread on its own: a shallow spread of the
      // payload would carry the original capabilities through untouched.
      await route.fulfill({
        response,
        json: { ...body, capabilities: { ...body.capabilities, assist: false } },
      });
    });

    await page.goto(`/trips/${tripId}/locations/new`);
    await expect(page.locator('input[name="title"]')).toBeVisible();
    await expect(page.locator(".assist-slot")).toBeHidden();
    await expect(page.locator('[data-action="assist-run"]')).toHaveCount(0);
  });
});
