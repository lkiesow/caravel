// The trips list's toolbar (Stage 15 Milestone 2): search + sort, both applied
// in memory over the one GET /trips the page makes.
//
// Read-only throughout, so this spec needs none of files.spec.js's
// create-own-trip isolation: searching and sorting write nothing, and every
// assertion is about the order and count of cards the shared seed already
// renders. That is also why the sort assertions check a *property* -
// non-decreasing dates, undated last - rather than a hard-coded list of titles:
// the `full` scenario's dates are relative to today, so its position in a
// date-ordered list moves as real time passes, and a literal expected order
// would rot a few weeks after it was written.
import { test, expect } from "@playwright/test";
import { login, gotoRoute } from "./helpers/scenarios.js";

const MOBILE = { width: 324, height: 756 };
const MIN_TAP_TARGET_PX = 44;

// A word that appears in exactly one seeded title, and one that appears in
// exactly one seeded *subtitle* and in no title - the second is what proves the
// search reaches past the card's visible text.
const TITLE_ONLY_MATCH = { query: "iceland", title: "Demo: Iceland Ring Road" };
const SUBTITLE_ONLY_MATCH = {
  query: "december",
  title: "Demo: New Year Crossing",
};

const SORT_LABELS = {
  en: { newest: "Newest first", title: "By name", start: "By start date" },
  de: {
    newest: "Neueste zuerst",
    title: "Nach Name",
    start: "Nach Startdatum",
  },
};

function cardTitles(page) {
  return page
    .locator("trip-card")
    .evaluateAll((cards) => cards.map((c) => c.getAttribute("title")));
}

// Both locales: the toolbar is three controls on one deliberately non-wrapping
// row at 324px, and German is the longer language - the case most likely to
// overflow. routes.spec.js sweeps /trips for overflow and tap targets, but only
// in one locale.
for (const locale of ["en", "de"]) {
  test.describe(`trips toolbar (${locale})`, () => {
    test.use({ viewport: MOBILE, locale });

    test(`fits one row at 324px with every control tappable (${locale})`, async ({
      page,
    }) => {
      await login(page);
      await gotoRoute(page, "/trips");

      const toolbar = page.locator(".list-toolbar");
      await expect(toolbar).toBeVisible();

      // One row, not two: the row's height is one control's height. A wrap
      // would double it, which is the failure this catches.
      const geometry = await toolbar.evaluate((bar) => {
        const rows = [...bar.children].map((c) => c.getBoundingClientRect());
        return {
          height: Math.round(bar.getBoundingClientRect().height),
          scrollWidth: bar.scrollWidth,
          clientWidth: bar.clientWidth,
          controlCount: rows.length,
          sizes: rows.map((r) => [Math.round(r.width), Math.round(r.height)]),
          docOverflow:
            document.documentElement.scrollWidth -
            document.documentElement.clientWidth,
        };
      });

      expect(geometry.controlCount, "search, sort and New trip").toBe(3);
      expect(
        geometry.scrollWidth,
        "the toolbar must not scroll horizontally",
      ).toBeLessThanOrEqual(geometry.clientWidth);
      expect(
        geometry.docOverflow,
        "the page must not scroll horizontally",
      ).toBeLessThanOrEqual(0);
      expect(geometry.height, "one row, not a wrapped two").toBeLessThan(
        MIN_TAP_TARGET_PX * 2,
      );
      for (const [width, height] of geometry.sizes) {
        expect(
          height,
          `control ${width}x${height} is below the tap floor`,
        ).toBeGreaterThanOrEqual(MIN_TAP_TARGET_PX);
        expect(
          width,
          `control ${width}x${height} is below the tap floor`,
        ).toBeGreaterThanOrEqual(MIN_TAP_TARGET_PX);
      }

      // The sort trigger says which order is in force, and starts on the
      // default rather than blank.
      await expect(page.locator(".trips-sort-slot .menu__label")).toHaveText(
        SORT_LABELS[locale].newest,
      );
    });
  });
}

test.describe("trips search", () => {
  test.use({ viewport: MOBILE });

  test("narrows the grid, reaches the subtitle, and says when nothing matched", async ({
    page,
  }) => {
    await login(page);
    await gotoRoute(page, "/trips");

    const search = page.locator('.list-search input[name="q"]');
    const emptyState = page.locator(
      ".trips-empty:not(.trips-empty--no-matches)",
    );
    const noMatches = page.locator(".trips-empty--no-matches");

    const all = await cardTitles(page);
    expect(
      all.length,
      "the seed should render several trips to narrow",
    ).toBeGreaterThan(2);
    await expect(emptyState).toBeHidden();
    await expect(noMatches).toBeHidden();

    await search.fill(TITLE_ONLY_MATCH.query);
    await expect(page.locator("trip-card")).toHaveCount(1);
    expect(await cardTitles(page)).toEqual([TITLE_ONLY_MATCH.title]);

    // The half that a title-only search would pass without: this word is in one
    // trip's subtitle and in nobody's title.
    await search.fill(SUBTITLE_ONLY_MATCH.query);
    await expect(page.locator("trip-card")).toHaveCount(1);
    expect(
      await cardTitles(page),
      "the search should reach the subtitle too",
    ).toEqual([SUBTITLE_ONLY_MATCH.title]);

    // Nothing matched is its own state, and must not be confused with the
    // "you have no trips at all" copy - which stays hidden, because you do.
    await search.fill("no-such-trip-anywhere");
    await expect(page.locator("trip-card")).toHaveCount(0);
    await expect(noMatches).toBeVisible();
    await expect(
      emptyState,
      "an account with trips must not be told it has none",
    ).toBeHidden();

    // Clearing brings everything back, in the original order.
    await search.fill("");
    expect(await cardTitles(page)).toEqual(all);
    await expect(noMatches).toBeHidden();
  });
});

test.describe("trips sort", () => {
  test.use({ viewport: MOBILE });

  async function chooseSort(page, label) {
    await page.locator(".trips-sort-slot .menu__trigger").click();
    await page.getByRole("menuitemradio", { name: label, exact: true }).click();
    await expect(page.locator(".trips-sort-slot .menu__label")).toHaveText(
      label,
    );
  }

  test("reorders by name and by start date, and keeps undated trips last", async ({
    page,
  }) => {
    await login(page);
    await gotoRoute(page, "/trips");

    const newest = await cardTitles(page);

    // --- by name ---
    await chooseSort(page, SORT_LABELS.en.title);
    const byName = await cardTitles(page);
    expect(byName, "sorting must not drop or duplicate a trip").toHaveLength(
      newest.length,
    );
    expect(
      [...byName].sort((a, b) =>
        a.localeCompare(b, "en", { sensitivity: "base" }),
      ),
    ).toEqual(byName);
    expect(
      byName,
      "by name should differ from newest-first on this seed",
    ).not.toEqual(newest);

    // --- by start date ---
    await chooseSort(page, SORT_LABELS.en.start);
    const dates = await page
      .locator("trip-card")
      .evaluateAll((cards) => cards.map((c) => c.getAttribute("start-date")));
    expect(dates, "sorting must not drop a trip").toHaveLength(newest.length);

    const dated = dates.filter(Boolean);
    const undated = dates.length - dated.length;
    expect(
      undated,
      "the seed should include a trip with no start date",
    ).toBeGreaterThan(0);
    // Every undated trip sits at the end: unscheduled, not imminent.
    expect(dates.slice(dates.length - undated).every((d) => d === null)).toBe(
      true,
    );
    // ISO dates, so a string comparison is a date comparison.
    expect([...dated].sort()).toEqual(dated);

    // --- back to the default ---
    await chooseSort(page, SORT_LABELS.en.newest);
    expect(
      await cardTitles(page),
      "returning to Newest first restores the server's order",
    ).toEqual(newest);
  });
});
