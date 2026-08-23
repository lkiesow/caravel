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
import { login, resolveScenarioTrips, gotoRoute, openAs, fetchTrips, SCENARIO_TITLES, OTHER_AUTH_STATE_FILE } from "./helpers/scenarios.js";

// Phone width, where the More menu is the only way to reach the overflow
// sections: the tab bar shows four tabs plus More under 640px.
const MOBILE = { width: 324, height: 756 };

// The sections that live behind More, and the copy each locale shows for them.
// Hard-coded rather than read from web/js/trip-tabs.js so a wrong *translation*
// is a failure and not just a mirror of the source.
const OVERFLOW_LABELS = {
  en: { more: "More", items: ["Files", "Expenses", "Members", "Settings"] },
  de: { more: "Mehr", items: ["Dateien", "Ausgaben", "Mitreisende", "Einstellungen"] },
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
//
// Driven on the `cascade` scenario rather than `full` since Stage 14 Milestone
// 7. That milestone gave a file row's menu a visibility radio group *on a
// shared trip*, and `full` is shared (the seeder puts `other` on it), so this
// test's whole premise — a menu with no selection in it — stopped being true
// there. `cascade` is a solo trip with two seeded files, which is still exactly
// the actions-only case. The shared variant is asserted separately below rather
// than folded in here, so each mode has a test that fails for one reason.
const FILE_MENU_LABELS = {
  en: { actions: "File actions", items: ["Edit note", "Delete"] },
  de: { actions: "Dateiaktionen", items: ["Notiz bearbeiten", "Löschen"] },
};

// The same row menu on a *shared* trip, where the list is grouped by visibility
// and the uploader's own rows offer the one move available to them.
//
// The menu deliberately holds no *state*: an earlier version put a
// personal/trip radio group here, which made renderMenu echo the selection onto
// the trigger — every row's ⋮ read "Everyone on the trip". Asserting the trigger
// is silent is what pins that.
const FILE_VISIBILITY_LABELS = {
  en: {
    sections: ["Everyone on the trip", "Only you"],
    uploadChoices: ["Everyone on the trip", "Only me"],
    ownShared: ["Make visible to only me", "Edit note", "Delete"],
    othersShared: ["Edit note", "Delete"],
  },
  de: {
    sections: ["Alle auf der Reise", "Nur du"],
    uploadChoices: ["Alle auf der Reise", "Nur ich"],
    ownShared: ["Nur für mich sichtbar machen", "Notiz bearbeiten", "Löschen"],
    othersShared: ["Notiz bearbeiten", "Löschen"],
  },
};

for (const locale of ["en", "de"]) {
  test.describe(`file row overflow menu (${locale})`, () => {
    test.use({ viewport: MOBILE, locale });

    test(`renders actions, not a selection (${locale})`, async ({ page }) => {
      await login(page);
      const trips = await resolveScenarioTrips(page);
      await gotoRoute(page, `/trips/${trips.cascade}/files`);
      const copy = FILE_MENU_LABELS[locale];

      // The `cascade` scenario seeds two files: one on the trip, one on a
      // location. Both rows carry their own menu.
      const rows = page.locator(".files > li");
      await expect(
        rows,
        "the cascade trip should show exactly its two seeded files — a different count means the dev database has drifted from the seed (leftover manual test data; run `make dev-reset FORCE=1`)"
      ).toHaveCount(2);

      // No visibility UI at all on a solo trip: personal versus trip-visible is
      // a question with one possible answer there. One unlabelled group, no
      // upload selector, and no section titles.
      await expect(page.locator('[name="uploadVisibility"]')).toHaveCount(0);
      await expect(page.locator(".file-section__title")).toHaveCount(0);
      await expect(page.locator(".file-section")).toHaveCount(1);
      // The note field is not about sharing, so it is there either way — an
      // upload has always been able to carry one, it just had nowhere to type it.
      await expect(page.locator('[name="uploadNote"]')).toHaveCount(1);

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

for (const locale of ["en", "de"]) {
  test.describe(`file row menu on a shared trip (${locale})`, () => {
    test.use({ viewport: MOBILE, locale });

    test(`groups by visibility and offers the move as an action (${locale})`, async ({ page }) => {
      await login(page);
      const trips = await resolveScenarioTrips(page);
      await gotoRoute(page, `/trips/${trips.full}/files`);
      const copy = FILE_VISIBILITY_LABELS[locale];

      // The drop zone carries the choice for whatever is uploaded next, since
      // it is made before the file is sent rather than after.
      const choices = page.locator(".file-upload__options .setting-choice span");
      await expect(choices).toHaveText(copy.uploadChoices);
      // Trip-visible by default, deliberately — see stage-14.md.
      await expect(page.locator('[name="uploadVisibility"][value="trip"]')).toBeChecked();
      // Both upload options live in one group with the drop zone, so neither is
      // loose under the list.
      const group = page.locator(".file-upload");
      await expect(group.locator(".file-drop")).toHaveCount(1);
      await expect(group.locator('[name="uploadNote"]')).toHaveCount(1);
      await expect(group.locator('[name="uploadVisibility"]')).toHaveCount(2);

      // Both groups, addressed by their own data attribute rather than by
      // position. Milestone 10 seeded a personal file on this trip, so the
      // second group is now deterministic — before that only the trip-visible
      // one could be asserted, and the private section had never been rendered
      // by anything in this suite. Still not asserting the *number* of
      // `.file-section` elements: a leftover manual upload in the dev database
      // should not read as a broken component, and "an empty group renders
      // nothing" is pinned on the solo trip above, where the data is known.
      const shared = page.locator('.file-section[data-visibility="trip"]');
      await expect(shared).toHaveCount(1);
      await expect(shared.locator(".file-section__title")).toHaveText(copy.sections[0]);
      const personal = page.locator('.file-section[data-visibility="personal"]');
      await expect(personal, "the private files group should render").toHaveCount(1);
      await expect(personal.locator(".file-section__title")).toHaveText(copy.sections[1]);
      // The list takes its name from that label rather than from a heading,
      // which would have put a hole in the page's outline.
      const labelId = await shared.locator(".file-section__title").getAttribute("id");
      await expect(shared.locator(".files")).toHaveAttribute("aria-labelledby", labelId);

      const row = shared.locator(".files > li").first();
      const trigger = row.locator(".menu__trigger");
      await trigger.click();
      const dropdown = row.locator(".menu__dropdown");
      await expect(dropdown).toBeVisible();

      // The trigger says nothing. This is the assertion that would have caught
      // the radio-group version, whose trigger echoed the selected visibility.
      await expect(row.locator(".menu__label")).toHaveText("");
      await expect(dropdown.locator('[role="menuitemradio"]')).toHaveCount(0);
      await expect(dropdown.locator("[aria-checked]")).toHaveCount(0);

      // These files belong to the seeded demo user, who the suite runs as, so
      // the move is offered. A file someone else uploaded would show only the
      // two actions — the server refuses that change too.
      await expect(dropdown.locator('[role="menuitem"]')).toHaveText(copy.ownShared);

      // Nothing is clicked: changing a visibility would mutate the shared seed,
      // which this spec has no isolation for. The Go tests cover the effect.
      await page.keyboard.press("Escape");
      await expect(dropdown).toBeHidden();
    });
  });
}

// The tab bar reads in the same order at both widths (Stage 14 Milestone 9).
//
// Desktop shows every tab in TRIP_TABS order; a phone shows the primary ones in
// a row and the rest behind More. Those two are only consistent if every
// overflow tab comes after every primary one in the array — and for a while
// files did not, so desktop read "... itinerary, files, checklists ..." while
// the phone row read "... itinerary, checklists" with files hidden in More. The
// same two tabs in opposite relative order depending on the width, which is the
// kind of thing nobody notices until they use both.
//
// Asserted as row-then-menu equals the desktop order, because that equality is
// the actual claim. The expected list is spelled out rather than imported from
// trip-tabs.js, for the reason this file spells out its labels: importing the
// source cannot disagree with it.
const TAB_ORDER = {
  en: ["Locations", "Map", "Itinerary", "Checklists", "Files", "Expenses", "Members", "Settings"],
  de: ["Orte", "Karte", "Reiseplan", "Checklisten", "Dateien", "Ausgaben", "Mitreisende", "Einstellungen"],
};

for (const locale of ["en", "de"]) {
  test.describe(`tab bar order (${locale})`, () => {
    test.use({ locale });

    test(`reads the same at both widths (${locale})`, async ({ page }) => {
      await login(page);
      const trips = await resolveScenarioTrips(page);
      const expected = TAB_ORDER[locale];

      // Desktop: every tab in the row, none behind More.
      await page.setViewportSize({ width: 1280, height: 800 });
      await gotoRoute(page, `/trips/${trips.full}/locations`);
      const visibleTabs = page.locator(".trip-tabs button[data-tab]:visible");
      await expect(visibleTabs).toHaveText(expected);

      // Phone: the row plus the More menu, concatenated, must be the same list.
      await page.setViewportSize({ width: 324, height: 756 });
      const row = await page.locator(".trip-tabs button[data-tab]:visible").allTextContents();
      await page.locator(".trip-tabs__more-slot .menu__trigger").click();
      const more = await page.locator('.trip-tabs__more-slot [role="menuitemradio"]').allTextContents();
      const mobileOrder = [...row, ...more].map((s) => s.trim());
      expect(mobileOrder, "phone row + More menu should read in the desktop order").toEqual(expected);

      // And the part that was asked for by name: checklists before files.
      expect(mobileOrder.indexOf(expected[3])).toBeLessThan(mobileOrder.indexOf(expected[4]));
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

// The checklist card's ⋮ menu, and the three visibility groups it moves between
// (Stage 14 Milestone 8, pinned in Milestone 10).
//
// Milestone 8 verified this by hand plus Go tests, which left the *grouping* —
// the thing that carries the state now that no card wears a badge — with no
// automated check at all. It could only be written once Milestone 10 seeded a
// list in each state: before that the `full` scenario held one `shared` list, so
// two of the three sections had never rendered anywhere, in this spec or in the
// route sweeps.
const CHECKLIST_LABELS = {
  en: {
    actions: "List actions",
    sections: { shared: "Everyone can tick", trip: "Everyone can see", personal: "Only you" },
    moves: { shared: ["Show to everyone", "Make private"], personal: ["Let everyone tick", "Show to everyone"] },
    duplicate: "Duplicate",
    tail: ["Rename", "Delete"],
  },
  de: {
    actions: "Listenaktionen",
    sections: { shared: "Alle können abhaken", trip: "Alle können sehen", personal: "Nur du" },
    moves: { shared: ["Für alle sichtbar machen", "Privat machen"], personal: ["Von allen abhakbar machen", "Für alle sichtbar machen"] },
    duplicate: "Duplizieren",
    tail: ["Umbenennen", "Löschen"],
  },
};

for (const locale of ["en", "de"]) {
  test.describe(`checklist card menu on a shared trip (${locale})`, () => {
    test.use({ viewport: MOBILE, locale });

    test(`groups by visibility and offers only the moves that apply (${locale})`, async ({ page }) => {
      await login(page);
      const trips = await resolveScenarioTrips(page);
      await gotoRoute(page, `/trips/${trips.full}/checklists`);
      const copy = CHECKLIST_LABELS[locale];

      // All three groups render, each addressed by its data attribute rather
      // than by position, and each titled. A missing group here means the seed
      // stopped covering that state — which is exactly the regression this
      // exists to catch.
      for (const [key, title] of Object.entries(copy.sections)) {
        const section = page.locator(`.checklist-section[data-visibility="${key}"]`);
        await expect(section, `the ${key} checklist group should render`).toHaveCount(1);
        await expect(section.locator(".checklist-section__title")).toHaveText(title);
        await expect(section.locator(".checklist-card")).toHaveCount(1);
      }

      // The moves offered depend on where the card already is: a card never
      // offers the state it is in. Asserting two different cards, because a
      // menu built from a constant list would pass on one of them.
      for (const key of ["shared", "personal"]) {
        const card = page.locator(`.checklist-section[data-visibility="${key}"] .checklist-card`);
        // The card's own menu, not its items': every checklist *item* carries a
        // ⋮ too, so an unscoped `.menu__trigger` under the card matches all of
        // them. Scoped to the header's actions slot.
        const actions = card.locator(".checklist-card__actions");
        const trigger = actions.locator(".menu__trigger");
        await expect(trigger).toHaveAttribute("aria-label", copy.actions);
        await trigger.click();
        const dropdown = actions.locator(".menu__dropdown");
        await expect(dropdown).toBeVisible();
        // Silent trigger, no selection: the same rule the file row menu follows,
        // and the same bug (renderMenu echoing an activeValue) it guards against.
        await expect(actions.locator(".menu__label")).toHaveText("");
        await expect(dropdown.locator('[role="menuitemradio"]')).toHaveCount(0);
        await expect(dropdown.locator('[role="menuitem"]')).toHaveText([...copy.moves[key], copy.duplicate, ...copy.tail]);
        // Nothing is clicked: a move would mutate the shared seed, which this
        // spec has no isolation for. The Go tests cover the effect.
        await page.keyboard.press("Escape");
        await expect(dropdown).toBeHidden();
      }
    });

    // The case Stage 15 Milestone 1 added, and the reason its authorization is
    // the *read* rule: somebody else's trip-visible list. `other` is an editor
    // on `full`, so they can see demo's "Route plan" and can neither tick nor
    // rename it - before Duplicate existed their ⋮ held nothing and so was not
    // rendered at all. Now it holds exactly one item.
    test(`a non-author's menu on a trip-visible list holds only Duplicate (${locale})`, async ({ browser }) => {
      const { context, page: theirPage } = await openAs(browser, OTHER_AUTH_STATE_FILE, MOBILE);
      try {
        // Navigated first: fetchTrips fetches a relative URL inside the page,
        // and a context that has never navigated has no origin to resolve it
        // against. And resolved by title rather than through
        // resolveScenarioTrips, which insists on all seven scenarios - `other`
        // is a member of two, so that helper is the demo user's.
        await theirPage.goto("/trips");
        const theirTrips = await fetchTrips(theirPage);
        const fullId = theirTrips.find((trip) => trip.title === SCENARIO_TITLES.full)?.id;
        expect(fullId, `\`other\` should be a member of ${SCENARIO_TITLES.full}`).toBeTruthy();
        await gotoRoute(theirPage, `/trips/${fullId}/checklists`);
        const copy = CHECKLIST_LABELS[locale];

        const card = theirPage.locator('.checklist-section[data-visibility="trip"] .checklist-card');
        await expect(card, "demo's trip-visible list should be visible to an editor").toHaveCount(1);
        // The premise: not theirs, and not tickable by them. If either of these
        // stops holding, this test is measuring a different situation.
        await expect(card.locator('.checklist-items input[type="checkbox"]').first()).toBeDisabled();
        await expect(card.locator(".checklist-item-form")).toHaveCount(0);

        const actions = card.locator(".checklist-card__actions");
        await actions.locator(".menu__trigger").click();
        const dropdown = actions.locator(".menu__dropdown");
        await expect(dropdown).toBeVisible();
        await expect(dropdown.locator('[role="menuitem"]')).toHaveText([copy.duplicate]);
        // Not clicked: this trip is the shared seed. The Go tests cover what the
        // click does, including that the copy becomes the duplicator's own.
        await theirPage.keyboard.press("Escape");
        await expect(dropdown).toBeHidden();
      } finally {
        await context.close();
      }
    });
  });
}
