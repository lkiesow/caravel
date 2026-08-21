# Stage 12 — The account settings screen

## Context

Three entries in `docs/plans/todo.md` have been circling each other since Stage
02 and can only be closed together:

- **The user menu has exactly one item** ("Log out"), still rendered by
  `web/js/components/user-menu.js`'s own hand-rolled popup — hidden-attribute
  visibility, `aria-expanded` sync, outside-click and Escape listeners — plus a
  `.user-menu__dropdown` CSS block that `.menu__dropdown` already duplicates
  (`web/css/base.css:323-361`, with a comment at 363 admitting the debt). Stage
  11 Milestone 3 removed the last blocker by giving `renderMenu` its action-item
  mode; what's left is the migration, and it needs a *second* item to be worth
  doing.
- **German is unreachable from inside the app.** `i18n.js:34` has a working
  `setLocale()` with a `localStorage` cache — and zero callers. The German
  locale renders cleanly when the browser is switched, which makes this a
  missing control, not a missing translation.
- **Theming is `prefers-color-scheme`-only.** Stage 01 structured it "so a
  manual `data-theme` override can be added later"; nothing in the tree sets
  `data-theme` today (`web/css/base.css:35` and `:113` are the only two
  `prefers-color-scheme` blocks).

The settings screen is the thing all three want: a second item in the user
menu (an *action*, precisely the mode that now exists), a home for the language
and appearance controls, and a home for **set my password**, which local
accounts have no path to at all today (`internal/auth/auth.go` has Register,
Authenticate, StartSession, ValidateSession, Logout and nothing else).

**Decided with the user up front:**

1. **Per-browser persistence first.** Language and appearance live in
   `localStorage` — no `users` columns, no migration, no preferences endpoint.
   The "follows you between browsers" version stays a `todo.md` entry.
2. **No profile picture.** `media_assets.trip_id` is `NOT NULL` and cascades
   from `trips`, so a user-scoped image has no valid home; that schema decision
   stays deferred.
3. **Password change is in scope** and is the one backend milestone. No
   migration either — `auth_identities.password_hash` and `sessions` already
   exist; it needs two sqlc queries, a service method, and a route.

---

## 0. Land the plan

Copy this document to `docs/plans/stage-12.md` before any code, per `CLAUDE.md`.

---

## 1. `user-menu.js` onto `components/menu.js`

Behaviour-preserving, no new screen. Goes first so the settings entry in
Milestone 2 is added to the *migrated* menu rather than to the copy that is
about to disappear.

- Rewrite `renderUserMenu(container, user, { onLogout })`
  (`web/js/components/user-menu.js`) as a thin wrapper over `renderMenu`
  (`web/js/components/menu.js:63`): `label: user.display_name`,
  `triggerClass: "user-menu__trigger"`, `ariaLabel: "auth.userMenu"`,
  `className: "menu--user"`, one item
  `{ value: "logout", label: t("auth.logout"), iconName: "log-out", action: true }`,
  and `onSelect` dispatching to `onLogout`. The Files row ⋮
  (`web/js/components/file-list.js:179`) is the working example to copy.
- `renderMenu` has no way to put the avatar circle in the trigger. Add one
  option — `triggerPrefixHtml`, inserted before `.menu__label` — rather than
  post-processing the DOM from the caller, and document it in the option block
  at the top of `menu.js` like the others. The avatar stays
  `<span class="user-menu__avatar">` so its CSS survives untouched.
- Delete the duplicated popup machinery *and* `.user-menu__dropdown` and its
  `button` / `button:hover` children from `base.css` (323–361) plus the debt
  comment at 363–366. Keep `.user-menu-slot`, `.user-menu`,
  `.user-menu__trigger`, `.user-menu__avatar`, `.user-menu__name`, retargeted at
  the `.menu` markup where the class names moved.
- Local `escapeHtml` (`user-menu.js:64`) goes away with the template.

**Verify:** `make ci`; extend `tests/ui/menu.spec.js` (which already drives the
tab bar's More menu and the Files ⋮ in both locales) with the user menu: open,
Escape, outside click, `aria-expanded`, one `role="menuitem"`, and that
`.user-menu__dropdown` no longer exists in the DOM while `.menu__dropdown`
does.

**Done.** `renderUserMenu` is now ~35 lines over `renderMenu`: `label` pinned
to the display name, `triggerClass: "user-menu__trigger"`,
`className: "menu--user"`, and one action item for Log out. `menu.js` gained
the single new option the plan called for, named `triggerPrefixHtml` — trusted
markup at the head of the trigger, which is how the initials avatar gets there
(`iconName` can only name a sprite symbol); the caller escapes the initial. The
hand-rolled popup, its local `escapeHtml`, and `.user-menu__dropdown` with its
`button` / `button:hover` children are gone from `base.css`, along with the debt
comment that pointed at them and the now-redundant `.user-menu` root rule
(`.menu` already carries `position: relative`).

Two rules had to be retargeted rather than deleted: the ≤640px collapse of the
display name moved from `.user-menu__name` to `.menu--user .menu__label`, and a
`.menu--user .menu__dropdown { margin-top: 0.5rem }` variant keeps the roomier
gap the header had — the generic `.menu__dropdown` uses 0.25rem, which sits
flush against a trigger with no button border. One deliberate visual change: the
chevron now renders with `.menu__chevron`, so it is muted like every other
menu's.

Verified: `make ci` green; `make test-ui` green (18 tests). `menu.spec.js` grew
a third mode — the user menu in both locales (accessible name, avatar,
`role="menuitem"` with zero `aria-checked`, toggle/outside-click/Escape, 44px
trigger) asserting `.user-menu__dropdown` has count 0 while `.menu--user`
exists — plus a viewport test that the display name shows at 1280px and hides
at 324px. Log out is deliberately never clicked: it would end the session
`auth.setup.js` shares with the whole suite.

---

## 2. A `/settings` route, reached from the user menu

The shell only — the three cards land empty-ish and get their contents in
Milestones 3–5, so each control is reviewable on its own.

- `web/js/pages/settings-page.js`, `export async function
  renderSettingsPage(container)`, modelled on `pages/trips-page.js:8`:
  `div.page.settings-page > div.page__header > h1[data-i18n="settings.title"]`,
  then `div.editor-card` sections in the order the todo entry gives them —
  Language, Appearance, Password — using the same card shape as
  `pages/settings-tab.js`. `translatePage(container)` at the end.
- Route entry `{ pattern: "/settings", render: renderSettingsPage }` in
  `web/js/app.js`'s table (before the `"*"` catch-all), plus the import.
- Second user-menu item `{ value: "settings", label: t("settings.title"),
  iconName: "settings", action: true }`, above Log out, navigating via
  `navigate("/settings")` (`web/js/router.js:3`). New icon `settings` needs the
  sprite regeneration recipe in `CLAUDE.md` — add it to `ICONS` in
  `scripts/gen_icon_sprite.py` and diff for byte-identical existing symbols.
- New keys in **both** `web/locales/en.json` and `de.json`
  (`scripts/check_i18n.py` gates this): `settings.title`, `settings.language`,
  `settings.appearance`, `settings.password` and the section descriptions.
- Register the route in the suite: `tests/ui/helpers/scenarios.js`'s
  `buildRoutes` (line ~152) so the overflow / heading / accessible-name /
  tap-target sweeps cover it in both viewports and both colour schemes from
  this milestone onward.

**Verify:** `make ci`; `npx playwright test tests/ui/routes.spec.js
tests/ui/headings.spec.js tests/ui/a11y-names.spec.js` green with the new route
in the matrix — one `h1`, no level skips, no overflow at 324×756.

**Done.** `web/js/pages/settings-page.js` renders the three `editor-card`
sections (Language / Appearance / Password), each with an `h2`, a
`.editor-card__hint` line and an empty slot for the control that lands in
Milestones 3–5. Route `{ pattern: "/settings" }` sits right after `/trips` in
`app.js`, and the user menu gained a Settings item above Log out, navigating
via `navigate("/settings")`.

Two deviations from the plan. The `settings` icon **was already in the sprite**
(the trip tab bar uses it), so no `gen_icon_sprite.py` run was needed. And the
page is titled **"Account settings"** / "Kontoeinstellungen" rather than
"Settings": a trip already has a Settings tab, and a menu item reading
"Settings" two clicks from it would name the wrong thing. Seven keys landed in
both locales (`settings.title`, and `.language` / `.appearance` / `.password`
each with a `…Hint`); `check_i18n.py` reports 141 keys in sync.

Verified: `make ci` green; `make test-ui` green (18 tests). `/settings` is
registered in `scenarios.js`'s `buildRoutes`, so the sweeps now cover 20 routes
(a11y-names checked 348 controls) across desktop/mobile × light/dark with no
overflow, a valid heading outline and no unnamed controls. `menu.spec.js` also
clicks the new item now — the only clickable one in that menu, since Log out
would end the shared session — asserting the URL becomes `/settings` and the
`h1` reads the localized title.

One real bug the German run caught: the outside-click step clicked the
heading's *centre*, and at 324px the wider German label ("Kontoeinstellungen")
makes the right-aligned dropdown overlap it, so the click landed on the menu
instead of outside it. Now clicked at the heading's left edge.

---

## 3. Appearance: light / dark / auto

Pure frontend, no persistence question left open.

- Teach `base.css` a `data-theme` hook without duplicating the palette: keep
  the light values on bare `:root` (1–32), change the dark block at 35 to
  `@media (prefers-color-scheme: dark) { :root:not([data-theme="light"]) }`,
  and add a `:root[data-theme="dark"]` block redefining the same tokens so an
  explicit choice beats the OS in both directions. The `.btn:hover` /
  `.btn:active` brightness inversion at 113 needs the identical treatment — it
  is the one rule that reads the scheme outside `:root`, and missing it makes
  buttons the only element that ignores the toggle.
- `web/js/theme.js`: `getTheme()` / `setTheme(value)` over
  `localStorage["caravel.theme"]` (`"light" | "dark" | "auto"`, default
  `"auto"`) writing/removing `document.documentElement.dataset.theme`, mirroring
  `i18n.js`'s storage-key + detect shape.
- Apply it **before first paint** — a tiny inline `<script>` in
  `web/index.html`'s `<head>` reading the same key, so a dark-forced app does
  not flash light while `app.js` loads. Keep it a single statement with a
  `try {}` so a blocked `localStorage` cannot break boot.
- The Appearance card is a three-radio group (not a toggle): "auto" has to be a
  real, selectable choice rather than the absence of one. Keys
  `settings.theme.light` / `.dark` / `.auto` in both locales.

**Verify:** `make ci`; a new `tests/ui/settings.spec.js` asserting computed
styles rather than screenshots — with `colorScheme: "light"` emulated, choose
Dark and assert `documentElement.dataset.theme === "dark"` *and* that
`getComputedStyle(document.body).backgroundColor` actually changed to the dark
token; the mirror case under `colorScheme: "dark"` choosing Light; and that
Auto removes the attribute and restores the emulated scheme's background. Plus
a reload asserting the choice survived and that no light flash occurs
(`data-theme` present on the first `document.documentElement` read).

**Done, with one deliberate change of approach.** The plan kept
`prefers-color-scheme` in CSS and added `data-theme` beside it, which means two
copies of the dark palette — an OS copy and an override copy — that can drift.
Instead **`data-theme` is now the only thing base.css keys on**, and
`web/js/theme.js` resolves the preference (light / dark / **auto**) to one of
light/dark and stamps it on `<html>`. So `[data-theme]` is never the word
"auto": auto is resolved, not represented. That is only safe because Caravel is
entirely client-rendered — with scripting off there is no app to theme, so a
CSS-only dark fallback would protect nobody. The result is one dark palette in
the file (plus a two-line `color-scheme` pair so scrollbars and native widgets
follow the app rather than the OS), and the `.btn:hover` / `:active` brightness
inversion became two ordinary `:root[data-theme="dark"]` rules.

`theme.js` exports `getTheme` (the stored *preference*), `resolveTheme`,
`setTheme`, `applyTheme` and `initTheme`; every `localStorage` access is
guarded, and "auto" is stored as the *absence* of a key so "never chose" and
"chose auto" are one state. `initTheme()` (called from `app.js` before
`initI18n`) adds the `matchMedia` listener that keeps Auto following the OS
while the tab is open — the one thing CSS used to do for free. The pre-paint
copy is an inline `<script>` in `web/index.html`; `sw.js`'s `CACHE_VERSION` went
to `v2` since both shell files changed. The control itself is
`components/theme-field.js`: three radios, each wrapped in a `.setting-choice`
label so the pill (not the ~14px input) is the tap target, with
`:has(input:checked)` for the accent — no JS-toggled class to keep in sync.

Verified: `make ci` green (144 i18n keys in sync); `make test-ui` green, 22
tests. `tests/ui/settings.spec.js` covers dark-on-a-light-device (attribute,
computed `body` background, stored value), the mirror case plus the theme
holding on a different route, Auto following `emulateMedia` live *and* an
explicit choice not being undone by the OS, and — with `**/js/**` aborted so no
app module can run — that `index.html` alone applies a stored dark theme, which
is the no-flash claim. Both new mechanisms were checked negatively: deleting the
inline script fails exactly the no-flash test, and removing the `matchMedia`
listener fails exactly the Auto-follows-live test, with the other three still
passing.

---

## 4. Language selector — the first caller of `setLocale()`

- **Three modes, like Appearance: Auto / English / Deutsch** — "auto" being
  today's behaviour (follow the browser), so it has to be a selectable choice
  rather than the absence of one. More languages are expected, so the control is
  *generated*, never hand-listed: a `renderMenu` in its selection mode (the
  category filter at `pages/locations-tab.js:49` is the pattern) over
  `["auto", ...SUPPORTED_LOCALES]`. Adding a language then stays two edits —
  `SUPPORTED_LOCALES` in `i18n.js:3` plus a `web/locales/xx.json` — with no
  settings-screen change at all. Appearance keeps radios because its three
  states are genuinely fixed; this list is not.
- Each language is labelled **in its own tongue** and therefore not translated:
  a `LOCALE_NAMES = { en: "English", de: "Deutsch" }` map exported from
  `i18n.js`, next to `SUPPORTED_LOCALES` so a new locale is obviously missing a
  name. Only the Auto row is translated (`settings.language.auto`, in both
  locale files), and it should name what it resolved to — "Automatic (English)"
  — otherwise the row gives no feedback about what the browser actually asked
  for.
- **"Auto" needs a clear, which `i18n.js` has no API for.** `detectLocale():10`
  reads `localStorage` first, and `setLocale()` only ever *writes* it, so
  returning to Auto is unreachable today. Add the missing pair alongside them:
  `getLocalePreference()` returning `"auto"` or the stored code (what the
  control binds to, distinct from `getLocale():44`, which returns the *resolved*
  locale), and support for `setLocale("auto")` / `clearLocale()` that
  `removeItem`s the key, re-detects from `navigator.languages`, and then follows
  the same path below.
- `setLocale()` (`i18n.js:34`) already writes `caravel.locale`, sets
  `documentElement.lang`, re-runs `translatePage(document.body)` and dispatches
  `locale-changed` on the shared `eventBus` (`web/js/eventbus.js:4`) — which has
  **zero listeners today**. `translatePage` only rewrites declarative
  `data-i18n*` attributes, so every string built through `t()` in JS (menu item
  labels, category filters, itinerary day headings) would keep the old locale.
  So this milestone's real work is the missing re-render: listen for
  `locale-changed` in `app.js:48`'s `renderAuthenticated` and re-run
  `renderUserMenu` + `router.render()`, preserving the current path.
- Wire the listener once per authenticated mount and remove it on re-mount, so
  a logout/login cycle does not stack duplicates.

**Verify:** `make ci`; extend `tests/ui/settings.spec.js` — switch to Deutsch,
assert `document.documentElement.lang === "de"`, that the `h1` reads the German
`settings.title`, **and** that a `t()`-built string elsewhere followed (navigate
to a trip's Locations tab and check the category filter trigger), which is the
assertion that would have caught the missing re-render. Then reload and assert
German persisted. Then the Auto path, which is the one with no code behind it
today: with the browser locale forced to German
(`test.use({ locale: "de-DE" })`), pick English, assert English, pick Auto, and
assert the app returns to German *and* that `localStorage["caravel.locale"]` is
gone — a stale key would make Auto silently sticky.

---

## 5. Set my password

Local accounts only (`auth_identities.provider = 'local'`); an OIDC identity has
no password here to change.

- **sqlc** (`internal/db/sqlc/queries/`): `UpdateAuthIdentityPassword` (by
  `provider` + `provider_user_id`, or by identity id) in
  `auth_identities.sql`, and `DeleteSessionsByUserID` in `sessions.sql`. Run
  `sqlc generate` by hand from `internal/db/sqlc/` and check **both** dialect
  packages regenerated; add both methods to the `db.Store` interface
  (`internal/db/store.go:170-181`) and to each hand-written adapter. **No
  migration** — nothing in the schema changes.
- **Service:** `(*Service).ChangePassword(ctx, userID, current, next)` in
  `internal/auth/auth.go`, reusing `argon2id.ComparePasswordAndHash` /
  `CreateHash` exactly as `Authenticate` (line 105) and `Register` (line 54) do.
  Requires the current password; returns `ErrInvalidCredentials` when it is
  wrong. On success, invalidate **all** the user's sessions
  (`DeleteSessionsByUserID`) — the whole point of the change is that a leaked
  password stops working everywhere.
- **Handler:** `POST /api/auth/password` in `internal/httpapi/auth.go`, behind
  `auth.RequireAuth` and `s.rateLimitLogin` (`router.go:76-81` is the pattern),
  enforcing the same ≥ 8-char floor as `handleRegister:29`. Because it just
  killed the caller's own session, it must then `startSessionAndRespond`
  (`auth.go:83`) so the current browser stays logged in with a fresh cookie
  while every other device is logged out.
- **Gate the card honestly:** add `has_password` to `userResponse`
  (`internal/httpapi/auth.go:13`, filled by looking up the local identity) and
  hide the Password card when false, so the screen is already correct when OIDC
  arrives instead of showing a control that cannot work.
- Frontend: current / new / confirm inputs, inline error via the existing form
  error pattern, success message on save. Keys in both locales, including a
  distinct message for "wrong current password" versus "too short".

**Verify:** `make ci` with new Go tests in `internal/auth` and
`internal/httpapi` — wrong current password rejected, short new password
rejected, correct change succeeds and the *old* password then fails, another
device's session token stops validating while the caller's new cookie still
does. Then one manual pass: change the password in a browser, confirm you are
still logged in, reload, and log in fresh with the new password.

---

## 6. Sweep-up

- German pass over the whole screen at 324×756 — German is the longer language
  and the radio-group labels are the most likely thing to overflow.
- `python3 scripts/i18n.py unused` should report clean; check no keys were
  orphaned by the `user-menu.js` rewrite.
- `docs/plans/stage-12.md`: a **Done.** paragraph per milestone.
- `docs/plans/todo.md`, both directions: delete the *user-menu refactor*, the
  *in-app language switcher*, the *manual light/dark theme toggle* and the
  *set my password* bullet of the settings entry; rewrite the settings entry
  down to what is genuinely left (profile picture + the per-account vs.
  per-browser question for both preferences); add anything this stage surfaced.

---

## Build order

1 → 2 → 3 → 4 → 5 → 6. One commit per milestone; a same-day fix on a milestone
gets its own "… follow-up: …" commit.

## Workflow

Per `CLAUDE.md`: implement → verify (`make ci` green **plus** a Playwright/manual
pass proving behaviour changed, assertions preferred over screenshots) → update
`docs/plans/stage-12.md` and `docs/plans/todo.md` → commit → leave `make dev`
running → **stop and wait** for the go-ahead before the next milestone.

## Verification (stage level)

- `make ci` green: build, vet, JS syntax, i18n key parity, `go test`.
- `npx playwright test` green, including `/settings` in the route sweeps
  (desktop + 324×756 mobile, light + dark) and the new `settings.spec.js`.
- Manual: user menu shows two items; Deutsch flips the whole app including
  `t()`-built strings and survives a reload; Dark overrides a light OS and Auto
  gives it back; password change logs out a second browser but not the one that
  made it.

## Out of scope

Profile picture (needs the `media_assets.trip_id` decision), per-account
persistence of language/theme (needs `users` columns and an endpoint), OIDC,
and the login/register page specs — all stay in `todo.md`.
