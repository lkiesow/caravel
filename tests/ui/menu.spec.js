// The first spec that drives an *interaction* rather than sweeping rendered
// pages.
//
// Everything else in this suite loads a route and measures what it finds, so
// nothing behind a click was covered at all: menus, dialogs and submitted forms
// were verified once by hand and then only by hope. This one takes the cheapest
// such case — the trip tab bar's "More" menu (Stage 09 Milestone 6 follow-up),
// which mutates no data, needs no cleanup and is pure clicks and computed
// styles — so the pattern exists for the rest to follow.
//
// It covers the popup behaviour that `components/menu.js` owns for all three of
// its callers: open, toggle shut, outside click, Escape, select-and-close, and
// which row is marked current. If this component regresses, the tab bar, the
// locations filter and (once it is folded onto the component) the user menu all
// regress together.
import { test, expect } from "@playwright/test";
import { login, resolveScenarioTrips, gotoRoute } from "./helpers/scenarios.js";

// Phone width, where the More menu is the only way to reach the overflow
// sections: the tab bar shows four tabs plus More under 640px.
const MOBILE = { width: 324, height: 756 };

// The sections that live behind More, and the copy each locale shows for them.
// Hard-coded rather than read from web/js/trip-tabs.js so a wrong *translation*
// is a failure and not just a mirror of the source.
const OVERFLOW_LABELS = {
  en: { more: "More", items: ["Files", "Settings"] },
  de: { more: "Mehr", items: ["Dateien", "Einstellungen"] },
};

async function openTripLocations(page) {
  await login(page);
  const trips = await resolveScenarioTrips(page);
  await gotoRoute(page, `/trips/${trips.full}/locations`);
  return trips.full;
}

// The tab bar's More menu, not the locations filter's: both are the same
// component, and the filter is the one whose trigger label tracks the
// selection, so scoping matters.
const MORE = ".trip-tabs__more-slot";

for (const locale of ["en", "de"]) {
  test.describe(`trip tab bar "More" menu (${locale})`, () => {
    test.use({ viewport: MOBILE, locale });

    test(`opens, closes every way it should, and navigates (${locale})`, async ({ page }) => {
      const tripId = await openTripLocations(page);
      const copy = OVERFLOW_LABELS[locale];

      const trigger = page.locator(`${MORE} .menu__trigger`);
      const dropdown = page.locator(`${MORE} .menu__dropdown`);

      // Closed to begin with, and saying so where it counts: `hidden` for
      // layout, aria-expanded for assistive tech. The two going out of sync is
      // the bug this component was extracted to stop.
      await expect(trigger).toBeVisible();
      await expect(trigger).toHaveAttribute("aria-expanded", "false");
      await expect(dropdown).toBeHidden();
      await expect(trigger).toHaveText(copy.more);

      // The label is pinned copy, not the current selection - "More" keeps
      // saying "More" even while a section behind it is open (menu.js's `label`
      // option). Also the trigger must clear the tap-target floor: it is one
      // cell of five in a 324px bar, which is exactly where it was 45px wide
      // inside a 58px cell before Stage 09 Milestone 6 fixed it.
      const box = await trigger.boundingBox();
      expect(box.height, "More trigger height").toBeGreaterThanOrEqual(44);
      expect(box.width, "More trigger width").toBeGreaterThanOrEqual(44);

      // 1. Open.
      await trigger.click();
      await expect(trigger).toHaveAttribute("aria-expanded", "true");
      await expect(dropdown).toBeVisible();
      // An open menu has to look open: the trigger takes menu__trigger--open
      // for every caller of the component, not only the tab bar.
      await expect(trigger).toHaveClass(/menu__trigger--open/);
      await expect(dropdown.locator('[role="menuitemradio"]')).toHaveText(copy.items);

      // 2. The trigger toggles rather than only opening.
      await trigger.click();
      await expect(dropdown).toBeHidden();
      await expect(trigger).toHaveAttribute("aria-expanded", "false");
      await expect(trigger).not.toHaveClass(/menu__trigger--open/);

      // 3. Outside click closes it. Clicking the page heading, which is a real
      // click on a real element rather than a synthetic event at 0,0.
      await trigger.click();
      await expect(dropdown).toBeVisible();
      await page.locator(".page__header h1").click();
      await expect(dropdown).toBeHidden();
      await expect(trigger).toHaveAttribute("aria-expanded", "false");

      // 4. Escape closes it, and the listener is attached only while open —
      // pressing Escape again with the menu closed must not throw or reopen it.
      await trigger.click();
      await expect(dropdown).toBeVisible();
      await page.keyboard.press("Escape");
      await expect(dropdown).toBeHidden();
      await page.keyboard.press("Escape");
      await expect(dropdown).toBeHidden();

      // 5. Selecting a section navigates, closes the menu, and leaves the
      // trigger marked as the active tab — the overflow sections have no cell
      // of their own at this width, so the trigger is the only place that can
      // show one of them is current.
      await trigger.click();
      await dropdown.locator('[role="menuitemradio"]').last().click();
      await expect(dropdown).toBeHidden();
      await expect(page).toHaveURL(`/trips/${tripId}/settings`);
      await expect(trigger).toHaveClass(/active/);

      // 6. Reopened on the settings tab, the current section is the checked row
      // — and still exactly one row is checked.
      await trigger.click();
      const checked = dropdown.locator('[role="menuitemradio"][aria-checked="true"]');
      await expect(checked).toHaveCount(1);
      await expect(checked).toHaveText(copy.items[copy.items.length - 1]);
    });
  });
}
