# Stage 20 — No write fires twice

## Context

On a slow network, double-clicking **Save** in the location editor creates the
location twice. Reported from real use, and reproducible by holding the POST.

The cause is structural rather than local: nothing in `web/js` stops a mutating
handler from being re-entered while its request is in flight. An audit of the
whole frontend found exactly **two** guarded controls —
`web/js/components/password-field.js` (`submit.disabled = true` … restored in
`finally`) and `web/js/components/assist-panel.js` (a `running` flag, which also
owns stream cancellation) — against roughly thirty unguarded ones: every
`<form>` submit, every page-level save/create/delete button, every ⋮ menu
action, and the checkbox and blur handlers that write on change.

The location editor is the worst case, and it shows why a per-button fix is not
enough. **Three** separate entry points call the same `save()`: the
`data-action="save"` click, the Basic-info form's submit-and-Enter pair reaching
it through `onSubmit`, and the Location card's own form. Disabling the Save
button alone would leave two doors open. They need one shared in-flight flag.

So this stage builds one small primitive and applies it everywhere, rather than
sprinkling `disabled = true` down thirty call sites and getting it subtly wrong
in the ones that re-render, the ones already disabled for another reason, and
the ones where a `<summary>` swallows the click.

**Decided scope.** All mutating controls. The busy state is `disabled` plus
`aria-busy="true"` — no spinner, no "Saving…" label, and therefore **no new
i18n keys**. Client-side only: no idempotency keys, no schema change, no
touching the create handlers. A second press is dropped by the browser, which
is where the double click happens.

---

## 1. The primitive, proven on trip creation

**New `web/js/busy.js`** — top level, beside `api.js` and `format.js`, because
it is app infrastructure and not a component.

```js
export function createGuard({ elements, preventDefault, stopPropagation })
//   -> { run(fn, ...args), wrap(handler), watch(elementsOrFn), get busy() }
export function guard(handler, options)          // sugar: createGuard(options).wrap(handler)
export function guardForm(form, handler, options) // attaches the submit listener
export function guardClick(el, handler, options)  // attaches the click listener
```

Every rule below answers a specific hazard the audit turned up.

- **`elements` may be an element, an array, or a function**, and a function is
  resolved *at invocation time*, so a control rebuilt by a later `render()` is
  found fresh. `guardForm` defaults it to
  `() => form.querySelectorAll('button[type="submit"], button:not([type])')`,
  so no call site has to name its own button — that removes the
  `form.querySelector('button[type="submit"]')` line `password-field.js`
  currently carries.
- **An element that is already `disabled` is skipped entirely** — not tracked,
  not given `aria-busy`, not restored. This is what the itinerary's
  move-up/move-down buttons need, since they are disabled at the ends of the
  list; the restore path can then never invent an enabled state.
- **Restore is per-element and skips detached nodes** (`el.isConnected`).
  Success paths call `render()`, `load()` or `navigate()`, so in practice only
  the failure path re-enables anything — the same shape `password-field.js`
  has today.
- **Focus restore, conditionally.** Disabling a focused button drops focus to
  `<body>`. Remember the focused node before disabling and refocus in `finally`
  only when `document.activeElement === document.body` and the node is still
  connected, so a `render()` that placed focus deliberately — the checklist
  add-item form does — always wins.
- **`preventDefault` and `stopPropagation` run before the busy check**, on the
  first argument when it is an event. Load-bearing for the itinerary's
  remove-day button, which sits inside a `<summary>`: the *dropped* second
  click still has to be swallowed or the `<details>` folds under the user.
  `guardForm` sets `preventDefault: true` and converted call sites delete their
  own line.
- **One flag per guard object, not per element.** A call while busy returns
  immediately — no throw, no queue. Handler errors propagate, so every existing
  `try/catch` around a write keeps working untouched.
- **No CSS needed.** `.btn:disabled` and `.icon-btn:disabled` already carry the
  look. One optional rule, `[aria-busy="true"] { cursor: progress; }`.

Then convert the two easiest and the one that proves the hard part:

- `password-field.js` — the existing idiom, so this conversion is a
  no-behaviour-change proof of the primitive.
- `login-page.js` — login and register.
- `trip-form.js` to `guardForm`, returning its guard alongside the `submit()` it
  already exposes; `trip-editor-page.js` then calls `form.guard.watch(createBtn)`
  so the page's own Create button shares the form's flag rather than owning a
  second one.

That last pair exercises the two hardest API decisions — the function-valued
resolver and one flag across two triggers — before anything else depends on
them.

Ships with `tests/ui/double-submit.spec.js` and the gate helper (see
Verification).

**Done.** `web/js/busy.js` landed as planned — `createGuard`, `guard`,
`guardForm`, `guardClick` — with the skip-already-disabled rule, the
`isConnected` check, the conditional focus restore and event handling before the
busy check all as described. `password-field.js`, `login-page.js` (login and
register) and `trip-form.js` are converted; `trip-editor-page.js` calls
`form.guard.watch(createBtn)`. One rule added to `base.css`
(`.btn[aria-busy="true"]:disabled { cursor: progress }` — specific enough to
beat the `.btn:disabled` / `.icon-btn:disabled` `not-allowed` that would
otherwise win it).

One deviation, and it is the part that mattered: `trip-form.js` did not
`await onSaved`. The create page uploads the staged cover photo and navigates
*inside* `onSaved`, so the guard would have released the moment `POST /trips`
answered and re-enabled Create halfway through the upload — the bug, one step
later. It is now `await`ed, and moved out of the `try` so a failed upload is not
reported as a failed save.

Verified: `make ci` green; `make test-ui` green in full (129 passed), which
covers the login, register, settings-password and trip-editor flows this
touched. The new `tests/ui/double-submit.spec.js` passes both cases, and — the
point of it — **fails against a defeated guard**: with the `busy` check
short-circuited, `POST /api/trips` reached the gate twice
(`Received length: 2`), which is the reported bug reproduced as an assertion.
The failure-path case is what proves the re-enable code, since every success
path throws the button away instead.

---

## 2. The location editor: one guard, three doors

The reported bug.

One page-level guard wrapping `save`, and the wrapped function is what gets
passed to `renderItemForm({ onSubmit })`, bound to `[data-action="save"]`, and
called by the Location card's submit-and-Enter handler — so all three entry
points share the flag while the Save button is the thing that visibly disables.

A second guard for `[data-action="delete"]`, wrapping the `confirmDialog` await
as well as the DELETE: the confirm dialog is itself an await, and a second
click while it is open should not stack a second dialog.

`flushUploads` needs nothing of its own — it only ever runs inside `save`.

---

## 3. The overflow menus, centrally

`web/js/components/menu.js` is the single choke point for **12** mutating ⋮
handlers, across `checklist-list.js`, `file-list.js`, `expenses-tab.js`,
`members-tab.js` and `admin-page.js`. One guard per `renderMenu` call — two
items in the same menu must not race, so Edit and Remove on one row are covered
by the same flag — with
`elements: () => [trigger, ...dropdown.querySelectorAll("[data-value]")]`.

Only the `onSelect` invocation is wrapped. `close()`, the `value === active`
short-circuit and the optimistic `syncLabel()` keep their current order, and
`setActive()` is untouched. **No call site changes.**

Sync callers are unaffected: `await fn()` on a non-promise resolves in one
microtask, and those callers navigate or re-render anyway, at which point
`isConnected` is false and restore is skipped.

The realistic double-fire here is *item, reopen, item again* — `close()` runs
first, which is precisely why disabling the trigger is the part that matters.

---

## 4. The remaining forms

Mechanical `guardForm` conversions, dropping each handler's own
`e.preventDefault()`:

- `admin-page.js` — create user
- `members-tab.js` — add member
- `expenses-tab.js` — add and edit expense
- `checklist-list.js` — add checklist, add item
- `itinerary-tab.js` — add day, add entry (the entry form can issue *two*
  sequential writes, `ensureDay` then the POST, so a re-entry here is worse
  than a duplicate row)
- `image-field.js` — set image by URL

---

## 5. The remaining buttons and toggles

- `settings-tab.js` — delete trip
- `image-field.js` — remove image, and the file-input upload above it
- `members-tab.js` — leave trip
- `itinerary-tab.js` — remove day, remove entry, move up, move down. Move
  up/down must keep their end-of-list `disabled` state, which the skip rule in
  Milestone 1 is there to guarantee.
- the checkbox `change` handlers in `checklist-list.js` (item checked) and
  `admin-page.js` (open signup)
- the day-notes `blur` PUT in `itinerary-tab.js`, guarded on the textarea
  itself: disabling the field the user has just left is invisible, and it makes
  a second `blur` structurally impossible rather than silently dropping typed
  text, which a bare drop-on-busy flag would do here.

**Out of scope, deliberately:** `assist-panel.js` keeps its own `running` flag,
because it also owns stream abort and a Cancel button; and the `search-place`
button in the location editor is a read, not a mutation, and already guards
itself.

---

## Build order

1. The primitive — first and alone; every later milestone applies it.
2. The location editor. The reported bug, and the hardest call site.
3. `menu.js`. Twelve call sites for one change.
4. The remaining forms.
5. The remaining buttons and toggles.

The reported bug is Milestone 2 rather than Milestone 1 because it is the one
call site that needs the shared-flag API to already be settled.

---

## Files this touches

- `web/js/busy.js` (new)
- `web/js/components/`: `menu.js`, `trip-form.js`, `password-field.js`,
  `checklist-list.js`, `file-list.js`, `image-field.js`, `location-form.js`
- `web/js/pages/`: `location-editor-page.js`, `trip-editor-page.js`,
  `login-page.js`, `admin-page.js`, `members-tab.js`, `expenses-tab.js`,
  `itinerary-tab.js`, `settings-tab.js`
- `tests/ui/double-submit.spec.js` and `tests/ui/helpers/gate.js` (both new)
- possibly one rule in `web/css/base.css`
- `plans/stage-20.md`, `plans/todo.md`

Reused rather than rebuilt: `web/js/api.js` unchanged, the existing
`.btn:disabled` styling, `password-field.js`'s disable-and-restore idiom as the
model for the primitive, and `tests/ui/locations.spec.js`'s own-trip
`beforeEach`/`afterEach` shape.

---

## Verification

`make ci` green at every milestone. No milestone adds a user-facing string, so
an i18n parity failure means something unintended happened.

New `tests/ui/double-submit.spec.js`, owning its own trip the way
`locations.spec.js` does, so the seeded scenarios other specs read are never
touched.

**Hold the request rather than sleeping.** New `tests/ui/helpers/gate.js`:
`page.route(pattern, …)` records the request, awaits a Node-side promise, then
`route.continue()`. The returned `{ seen, release }` lets a spec assert while
the request is held and again after releasing it — deterministic, where a fixed
delay is a race. Register it *after* `login(page)`: `scenarios.js`'s
`blockExternalRequests` installs a catch-all `page.route("**/*")` and Playwright
runs handlers in reverse registration order, so the later specific route wins,
and `route.fallback()` hands non-matching methods back down to it.

**Fire the double press without actionability waits.** `locator.click()`
auto-waits for the control to be enabled, so a second `click()` would simply
hang once the fix lands, and the spec would be asserting nothing. Use one
synchronous in-page double-fire —
`page.evaluate(() => { const b = …; b.click(); b.click(); })`, or
`form.requestSubmit(); form.requestSubmit();` for forms.

**Three assertions per case**, because each catches a different wrong fix:

1. exactly one request reached the gate;
2. while held, the control is `disabled` and carries `aria-busy="true"`;
3. after release, a server-side read (`page.request.get`, filtered by a unique
   title) returns exactly one row — this is the one that catches a guard which
   freezes the UI while the server still got two.

**Plus the failure path** (Milestone 2): hold the POST, `route.fulfill` a 500,
then assert the button is enabled again, has no `aria-busy`, and still has
focus. That path is the only one the restore code ever runs on, so it needs its
own case.

Run with `make test-ui GREP="double-submit"`. For each milestone, confirm the
new case actually **fails** with that milestone's guard reverted. A race test
that passes against the old code is measuring nothing, and this whole stage is
about races.

---

## Workflow

Per `CLAUDE.md`: one milestone at a time — implement → verify (`make ci` green
plus evidence the behaviour actually changed) → add a **Done.** paragraph to
this file → update `plans/todo.md` in both directions → one commit per
milestone (follow-ups get their own) → make sure `make dev` is running → stop
and hand back control. No starting the next milestone until told to.
