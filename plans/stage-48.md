# Stage 48: Pages get a lifecycle

## Context

The backlog item "Pages have no lifecycle: late saves redirect, and nothing is
torn down" (`plans/todo.md`, **soon**) covers two problems. The router already
gives every render a fresh `<main>`, so a late *render* is harmless. What it
does not cover is anything a page does outside its own container:

- **A late save still redirects.** About ten handlers await a request (often
  behind a `confirmDialog`) and then call `navigate`, `leaveEditor`,
  `history.*` or `window.location`. If the user has moved on in the meantime,
  they get pulled off the page they chose. Only
  `location-editor-page.js:65` guards against this today, with
  `container.isConnected`.
- **Nothing is torn down.**
  - Logging out and back in creates a second router (`app.js:92`). The first
    router's `popstate` and click listeners (`router.js:111/115`) stay alive,
    so every navigation also renders into a dead `<main>`. That means double
    fetches, and stray `replaceState` calls such as
    `trip-detail-page.js:37`.
  - `mention-picker` adds `window` resize and orientationchange listeners. They
    leak whenever you leave the notes editor by any route other than its own
    re-render.
  - suggest-page's assist stream is never aborted when you leave the page.
    assist-panel's stream is aborted only on the editor's own re-render. In
    both cases a paid model call can run with nobody reading it.
  - A confirm dialog that is open survives Back. The awaiting handler then
    resumes on the new page.

The approach was agreed in the backlog review: the router hands each render
one `AbortSignal` and aborts it when the next render starts. Late saves check
`signal.aborted`, global listeners register with `{ signal }`, and streams
pass it to `fetch`. This has to land before offline mode, because offline
mode makes slow and failed requests the normal case.

**Deliberately not aborted: mutating requests.** Aborting a save in flight
does not undo it on the server, and the client would no longer know the
outcome. A save always finishes; only what happens *after* it (the redirect)
is skipped. GET requests are not aborted either, because late renders are
already harmless.

Out of scope, and going into `todo.md`:
- Aborting stale GETs. This saves bandwidth only, but may matter for offline
  mode.
- `popup.js` document listeners. They self-heal on the next click.
- The un-cleared gesture-hint `setTimeout` in map-view. It is harmless.

## 0. Land the plan

Commit this as `plans/stage-48.md`. Update the `todo.md` entry so it points
at Stage 48.

## 1. The router owns a signal per render, and can be torn down

- `router.js` `createRouter`:
  - Keep a `current` AbortController. In `render()`, run
    `current?.abort(); current = new AbortController();` and call
    `route.render(container, params, current.signal)`. The signal is the third
    argument, so the existing `(container, params)` signatures keep working.
  - Add a router-lifetime `AbortController`. Register the `popstate` and
    `click` listeners with `{ signal }`.
  - Return `destroy()`, which aborts both controllers.
  - Rewrite the comment at :84-91. It currently describes the leaked-router
    workaround; the fresh `<main>` stays for late renders.
- `app.js` `renderAuthenticated`: keep the router in a module-level `let`,
  the same way `onLocaleChanged` is kept. Call `router?.destroy()` before
  creating the new one. Make `onLogout` call `router.destroy()` before
  `boot()`, so the page that was open is aborted as well.
- `trip-detail-page.js`: tab switches are local re-renders that don't go
  through the router, so each tab needs its own scope.
  - The local `render()` aborts the previous `tabController` and creates
    `tabSignal = AbortSignal.any([signal, tabController.signal])`.
  - Pass `{ signal: tabSignal }` (alongside the existing options) to every tab
    renderer: locations, itinerary, notes, files, checklists, expenses,
    members and settings.
  - Also pass the route `signal` (as `pageSignal`) to settings-tab and
    members-tab, for the reason given in milestone 2.

Verify:
- New `tests/ui/page-lifecycle.spec.js`:
  - Log in, log out and log back in.
  - Open a trip and count `GET /api/trips/:id` requests with
    `page.on("request")`. Expect exactly 1. Today it is 2.
  - Click one `data-link` and expect one route fetch.

**Done.** `router.js`'s `render()` aborts the previous render's controller and
passes a fresh signal as the third argument to `route.render`. The `popstate`
and `[data-link]` click listeners register with a router-lifetime signal, and
the new `destroy()` aborts both. `app.js` keeps the router at module scope,
destroys the old one before creating a new one, and destroys it in `onLogout`
before `boot()`. The comment that described the leaked-router workaround now
explains the signal instead.

Deviation: the trip page's per-tab signal moved to milestone 2. Nothing
consumes it before then, so creating it here would only have added unused
plumbing.

Verified:
- `make ci` is green. The full `make test-ui` run gave 343 passed and 1
  failed: map.spec.js "a fix that disagrees is ridden out" (the course
  marker). It passed on its own re-run, and it does not touch routing.
- New `tests/ui/page-lifecycle.spec.js` ("logging out and back in leaves one
  router") fakes logout and login at the network, then counts one
  `GET /api/trips/:id` for a card click and one `GET /api/trips` for Back.
  Against the old `router.js`/`app.js` it fails with 2 trip fetches.
- By hand on `make dev` at 324×756: a real logout and login, then a card click
  and Back, issue one trip fetch and one list fetch.

## 2. Late saves don't redirect

The pattern at every site: after the last `await`,
`if (signal.aborted) return;` before the navigation. This replaces the
`isConnected` check in `location-editor-page.js:65`.

Sites, threading `signal` through closures that already capture `container`:
- **location-editor-page:** the guard at :65, delete (:311) and
  `commitSave` (:428).
- **trip-editor-page:** `onSaved` → `leaveEditor` (:58). trip-form needs no
  change, because the check sits in the page's `onSaved`.
- **suggest-page:** `addSelected` → `navigate` (:418). Also stop the
  `attachCovers` loop between items once aborted. The items are created
  already, and only the cover attachment is cosmetic.
- **admin-page:**
  - `toggleAdmin` → `navigate` (:176).
  - `deleteUser` → `window.location.href` (:217). Keep this one even when
    aborted: you deleted yourself, so the session is gone either way. Add a
    comment explaining why.
- **settings-tab and members-tab:** trip delete (:88) and leave trip (:211)
  check the **route** signal (`pageSignal`), not the tab signal. Switching to
  another tab of the same trip still leaves you on a trip that no longer
  exists or that you no longer belong to, so the redirect stays right.
  Leaving the trip entirely skips it.
- **locations-tab:** distance filter `getCurrentPosition` (:451). Check the
  tab signal before `applyFilters()`, which writes to history.
- **`dialog.js`:** `confirmDialog({ ..., signal })` closes the dialog and
  resolves as cancelled on abort. Every confirm call site above passes it.
  This covers "Back while the confirm is open".

Verify, in `page-lifecycle.spec.js`, using `holdRoute` from
`tests/ui/helpers/gate.js`:
- Hold `PATCH /api/items/*`, press Save in the location editor, click the
  brand link to `/trips`, then release. Expect the URL to stay `/trips` and
  the PATCH to have completed (200).
- Hold the trip `DELETE`, confirm, and switch to another trip. Expect no
  redirect. In a second case, stay on the trip (switch tab only) and expect
  the redirect to `/trips`.
- Open a confirm dialog and run `page.goBack()`. Expect the dialog to be gone
  and no DELETE to have been sent.

## 3. Teardown: listeners and streams

- **mention-picker:** `bindMentionPicker(..., { signal })` registers its
  `window` listeners with `{ signal }` and calls `suggest.destroy()` on
  abort. notes-tab passes the tab signal. The existing per-re-render
  `destroy()` stays.
- **suggest-page:** pass `AbortSignal.any([signal, controller.signal])` to
  `postStream`. This keeps Cancel as it is. The existing `AbortError` swallow
  already covers the abort.
- **assist-panel:** `renderAssistPanel` accepts `signal`, and the location
  editor passes the route signal. Combine it the same way. `destroy()` stays
  for the editor's own re-render.
- Grep `pages/` and `components/` once more for `window.`/`document.`
  `addEventListener` without a matching remove. The census found no other
  leaks, but re-check after milestones 1-2.

Verify:
- Hold the suggest stream endpoint open, start a run and navigate away. Expect
  the request to fail with `requestfailed`, and the server log to show the
  context cancelled. The same with the assist panel in the location editor.
- Mention picker: an init script wraps `window.addEventListener` and
  `removeEventListener` to count live `resize` listeners. Open the notes
  editor, switch to another tab, and expect the count to be back at baseline.

## Build order

0 → 1 → 2 → 3. Milestones 2 and 3 consume the signal that milestone 1
introduces.

## Workflow

For each milestone:
1. Implement.
2. `make ci` green, plus `make test-ui` (the new spec and the existing
   locations "Back while loading" and double-submit specs). Do a manual pass
   against `make dev` at 324×756.
3. Add a **Done.** paragraph here and update `plans/todo.md`. After milestone
   3, remove the lifecycle entry and add the out-of-scope items above.
4. Commit, one per milestone.
5. Make sure `make dev` is running, then stop and wait.

## Verification

- `make ci` and `make test-ui` green, including `page-lifecycle.spec.js`.
- Manual check in a real browser with DevTools throttling set to "Slow 3G":
  - Save a location, then press Back immediately. You stay where Back took
    you, and the edit is saved.
  - Delete a trip, then tap another trip right away. You stay on the other
    trip.
  - Log out and back in, then navigate. The Network panel shows one fetch per
    navigation.
  - Start a suggestion and leave the page. The stream request shows as
    cancelled.
