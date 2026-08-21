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
// locations filter, the file row's actions and the header's user menu all
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
  en: { more: "More", items: ["Files", "Members", "Settings"] },
  de: { more: "Mehr", items: ["Dateien", "Mitreisende", "Einstellungen"] },
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

// The same component in its *other* mode: the per-file overflow menu on the
// Files tab (Stage 11 Milestone 3). Its items are actions, not a selection, so
// the assertions here are the mirror image of the ones above - role="menuitem"
// and no aria-checked anywhere, where the tab bar has role="menuitemradio" and
// exactly one checked row.
//
// Nothing here clicks an action: Edit note and Delete both mutate the shared
// seed, which the suite has no isolation for yet (see todo.md). Everything up
// to the click is still worth holding: the roles, the copy in both locales, the
// destructive tint, and the trigger's tap target.
const FILE_MENU_LABELS = {
  en: { actions: "File actions", items: ["Edit note", "Delete"] },
  de: { actions: "Dateiaktionen", items: ["Notiz bearbeiten", "Löschen"] },
};

for (const locale of ["en", "de"]) {
  test.describe(`file row overflow menu (${locale})`, () => {
    test.use({ viewport: MOBILE, locale });

    test(`renders actions, not a selection (${locale})`, async ({ page }) => {
      await login(page);
      const trips = await resolveScenarioTrips(page);
      await gotoRoute(page, `/trips/${trips.full}/files`);
      const copy = FILE_MENU_LABELS[locale];

      // The `full` scenario seeds two files: one on the trip, one on a
      // location. Both rows carry their own menu.
      const rows = page.locator(".files > li");
      await expect(
        rows,
        "the full trip should show exactly its two seeded files — a different count means the dev database has drifted from the seed (leftover manual test data; run `make dev-reset FORCE=1`)"
      ).toHaveCount(2);

      const trigger = rows.first().locator(".menu__trigger");
      const dropdown = rows.first().locator(".menu__dropdown");

      // An icon-only trigger, so the accessible name is the only thing naming
      // it - and it has to clear the tap floor at 324px like every other
      // control in a row.
      await expect(trigger).toHaveAttribute("aria-label", copy.actions);
      await expect(trigger).toHaveAttribute("aria-expanded", "false");
      const box = await trigger.boundingBox();
      expect(box.height, "row menu trigger height").toBeGreaterThanOrEqual(44);
      expect(box.width, "row menu trigger width").toBeGreaterThanOrEqual(44);

      await trigger.click();
      await expect(dropdown).toBeVisible();
      await expect(trigger).toHaveClass(/menu__trigger--open/);

      // The whole point of the mode: these are menuitems, and nothing in here
      // claims a checked state. "Delete" is not a state the menu is now in.
      await expect(dropdown.locator('[role="menuitem"]')).toHaveText(copy.items);
      await expect(dropdown.locator('[role="menuitemradio"]')).toHaveCount(0);
      await expect(dropdown.locator("[aria-checked]")).toHaveCount(0);

      // The destructive one is tinted, and only it. Asserting the computed
      // color rather than the class, because the class was silently losing to
      // `.menu__dropdown button` on specificity when this was first written.
      const items = dropdown.locator('[role="menuitem"]');
      const danger = await items.last().evaluate((el) => getComputedStyle(el).color);
      const plain = await items.first().evaluate((el) => getComputedStyle(el).color);
      expect(danger, "Delete is tinted").not.toBe(plain);
      await expect(items.last()).toHaveClass(/menu__action--danger/);
      await expect(items.first()).not.toHaveClass(/menu__action--danger/);

      // Popup behaviour is the component's, so it must hold here too.
      await page.keyboard.press("Escape");
      await expect(dropdown).toBeHidden();
      await expect(trigger).toHaveAttribute("aria-expanded", "false");
    });
  });
}

// The header's user menu, third mode of the same component: one action item
// like the file row above, but with a pinned label and an avatar in the
// trigger (Stage 12 Milestone 1 folded it onto menu.js; before that it was a
// second, hand-rolled popup implementation).
//
// Nothing here clicks Log out: it would end the session the whole suite shares
// from auth.setup.js. Everything up to the click is covered — the roles, both
// locales, the avatar, and that the old markup really is gone.
//
// Three items, not two: the seeded demo user is an administrator (Stage 14
// Milestone 6), so the menu carries Administration between Settings and Log
// out. That the list is spelled out here rather than read from user-menu.js is
// what turned the seeder change into a failure instead of a silent pass — worth
// keeping in mind before "fixing" this by generating it.
const USER_MENU_LABELS = {
  en: { menu: "User menu", items: ["Account settings", "Administration", "Log out"] },
  de: { menu: "Benutzermenü", items: ["Kontoeinstellungen", "Verwaltung", "Abmelden"] },
};

for (const locale of ["en", "de"]) {
  test.describe(`header user menu (${locale})`, () => {
    test.use({ viewport: MOBILE, locale });

    test(`is one action item on the shared popup component (${locale})`, async ({ page }) => {
      await login(page);
      await gotoRoute(page, "/trips");
      const copy = USER_MENU_LABELS[locale];

      const slot = page.locator(".user-menu-slot");
      const trigger = slot.locator(".menu__trigger");
      const dropdown = slot.locator(".menu__dropdown");

      // The migration's actual claim: this menu is now the shared component,
      // and the old hand-rolled markup is not merely unused but absent.
      await expect(page.locator(".user-menu__dropdown")).toHaveCount(0);
      await expect(slot.locator(".menu--user")).toHaveCount(1);

      // The trigger names itself (the display name beside the avatar is
      // hidden at this width) and clears the tap floor.
      await expect(trigger).toHaveAttribute("aria-label", copy.menu);
      await expect(trigger).toHaveAttribute("aria-expanded", "false");
      await expect(dropdown).toBeHidden();
      await expect(slot.locator(".user-menu__avatar")).toHaveText(/^[A-Z?]$/);
      const box = await trigger.boundingBox();
      expect(box.height, "user menu trigger height").toBeGreaterThanOrEqual(44);
      expect(box.width, "user menu trigger width").toBeGreaterThanOrEqual(44);

      // Log out is something the menu does, so: menuitem, nothing checked.
      await trigger.click();
      await expect(dropdown).toBeVisible();
      await expect(trigger).toHaveClass(/menu__trigger--open/);
      await expect(dropdown.locator('[role="menuitem"]')).toHaveText(copy.items);
      await expect(dropdown.locator("[aria-checked]")).toHaveCount(0);

      // Popup behaviour, inherited rather than reimplemented: toggle shut,
      // outside click, Escape.
      await trigger.click();
      await expect(dropdown).toBeHidden();
      await trigger.click();
      // Clicked at the heading's left edge, not its centre: this dropdown is
      // right-aligned under the header, and at 324px the German label makes it
      // wide enough to cover the middle of the h1 - a centre click then lands
      // on the menu instead of outside it, which is how this first failed.
      await page.locator(".page__header h1").click({ position: { x: 2, y: 2 } });
      await expect(dropdown).toBeHidden();
      await trigger.click();
      await page.keyboard.press("Escape");
      await expect(dropdown).toBeHidden();
      await expect(trigger).toHaveAttribute("aria-expanded", "false");

      // The item that has somewhere to go, goes there — and the menu closes
      // behind it. This is the only clickable item in here (Log out would end
      // the shared session), so it is also the proof that action items in this
      // menu fire at all.
      await trigger.click();
      await dropdown.locator('[role="menuitem"]').first().click();
      await expect(dropdown).toBeHidden();
      await expect(page).toHaveURL("/settings");
      await expect(page.locator("h1")).toHaveText(copy.items[0]);
    });
  });
}

// The display name is part of the trigger on a wide screen and drops out under
// 640px — a rule that used to hang off .user-menu__name and now has to keep
// working against menu.js's own .menu__label.
test.describe("header user menu label collapse", () => {
  test("shows the display name at desktop width and hides it on a phone", async ({ page }) => {
    await page.setViewportSize({ width: 1280, height: 800 });
    await login(page);
    await gotoRoute(page, "/trips");
    const label = page.locator(".user-menu-slot .menu__label");
    await expect(label).toBeVisible();
    await expect(label).not.toBeEmpty();
    await page.setViewportSize(MOBILE);
    await expect(label).toBeHidden();
  });
});
