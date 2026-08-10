# Stage 03 — Bug fixes, CI foundation, checklists

> **Status: in progress.** Building one milestone at a time per the Workflow
> section below, each with its own commit and a manual-testing checkpoint.

## Context

Stages 01–02 delivered a working Caravel app and then a UI/UX review pass.
`docs/plans/todo.md` collects everything deferred or discovered since:
confirmed bugs found by re-reading the current code, smaller Stage 01/02
deferrals, testing/CI gaps, and new feature ideas from `notes.md`. Stage 03
picks the highest-value subset of that backlog: the four confirmed bugs (one
of which is an actual XSS hole, not just a cosmetic gap), a first CI/testing
safety net (there is currently zero automated testing and no CI at all), and
one new feature (trip checklists) that fits the existing schema/handler/
frontend pattern cleanly.

Explicitly **out of scope** for this stage, left in `todo.md` for later:
sharing/collaboration/permissions, public share links, expenses, S3 storage,
Prometheus metrics, OIDC, federation, journal entries, manual light/dark
toggle, itinerary reordering, "add to every day" convenience action, in-app
language switcher, migrations squash, drag-and-drop document upload, user
menu additions beyond logout, and the Leaflet-vs-Google-Maps
re-evaluation. The location editor's create-mode field gap
(`location-editor-page.js`) was investigated and found to already work as
designed — Basic Info and Cover Photo are available immediately, and the
create→edit redirect (mirroring the trip editor) gets the user to
Location/Links/Dates/Documents within one save, matching Stage 02's stated
scope boundary. No action needed there.

Grounding for this plan came from three Explore passes over the current
codebase (confirmed-bug code paths in `documents.go`/`media.go`/notes
rendering; tab routing/breadcrumbs/location-editor gap; CI/testing state and
the `documents` feature as a template for `checklists`) — file:line
references below reflect the actual current state.

## 1. CI / testing foundation

Currently: `Makefile` only has `run`/`dev`/`build`/`test` (the last just
`go test ./...`, with zero `*_test.go` files in the repo to run), no
`.github/workflows/`, no seed-data script. `web/locales/en.json`/`de.json`
are flat dotted-key JSON (100 keys each currently), which makes a key-parity
diff trivial.

- **i18n key-parity script**: small Go or Python script (e.g.
  `scripts/check_i18n.py`, following the precedent of the existing
  `scripts/gen_icons.py`/`gen_icon_sprite.py`) that **globs every
  `web/locales/*.json` file** rather than hardcoding `en`/`de` — computes
  the union of all keys across all discovered locale files and fails with a
  non-zero exit + printed diff (per file: which keys it's missing) if any
  file doesn't have the full set. This way adding a third language later
  needs no script change — it's picked up automatically and checked the
  same way.
- **JS syntax check**: no bundler/Node toolchain exists by design — use
  `node --check <file>` (ships with any Node install, no deps) looped over
  every file in `web/js/` as a Makefile target / CI step.
- **Go checks**: `go build ./...` and `go vet ./...` (already scriptable via
  existing `Makefile` targets — just needs CI wiring).
- **Dev seed-data script**: a small script or a `caravel` CLI subcommand
  (whichever fits the existing `cmd/caravel` structure better once read)
  that creates a demo user + trip + a few items/documents/itinerary entries
  against a fresh SQLite DB, so manual testing doesn't start from empty
  every time. Wire a `make dev-seed` or similar Makefile target.
- **GitHub Actions workflow** (`.github/workflows/ci.yml`): runs on
  push/PR — `go build ./...`, `go vet ./...`, the JS syntax check, and the
  i18n parity check. No Playwright in this stage (kept out per user
  decision — full UI test suite deferred to a later stage). Use current
  major versions of the standard actions rather than pinning to whatever an
  old tutorial shows — as of this writing that's `actions/checkout@v7` and
  `actions/setup-go@v7`, but **re-check for newer major releases at
  implementation time** since these move fast.

This lands first so every later milestone in this stage is verified by an
actual CI run, not just ad hoc local checks.

## 2. Markdown rendering for notes (+ fixes a real XSS bug)

Confirmed: **trip notes are inserted as raw unescaped HTML** —
`web/js/pages/trip-detail-page.js:76`:
`` `<dd class="trip-overview__notes">${trip.notes ?? "—"}</dd>` `` — this is
an XSS hole today (any trip note containing `<script>` or an event handler
attribute executes), not just a missing-markdown cosmetic issue. Item/
location notes (`web/js/pages/location-view-page.js:106`) use `textContent`
today — safe, but plain text, no markdown rendering.

**Decision: render server-side in Go, not client-side JS.** Rather than
vendoring a JS markdown library + a JS sanitizer (the Leaflet/Lucide
vendoring pattern), use a Go markdown pipeline and store the rendered,
sanitized HTML alongside the raw markdown. This avoids adding any new
frontend vendor dependency, keeps the sanitization boundary in one trusted
place (server) instead of relying on client-side JS to sanitize before every
render, and means the frontend does zero markdown work — it just inserts
already-safe HTML. The standard, well-maintained combination for this in Go
is [`goldmark`](https://github.com/yuin/goldmark) (CommonMark-compliant
parser/renderer; does not render raw embedded HTML by default) piped
through [`bluemonday`](https://github.com/microcosm-cc/bluemonday)'s
`UGCPolicy()` (allowlist-based HTML sanitizer, purpose-built for exactly
this "untrusted markdown → safe HTML" use case) — sanitize *after*
rendering, not before, so nothing unsafe can be reintroduced downstream.

- Add `go get github.com/yuin/goldmark github.com/microcosm-cc/bluemonday`.
- Add a small nullable `notes_html TEXT` column via migration to `trips`
  and `items` (whichever tables hold the `notes` field today — confirm
  during implementation). Compute it server-side (`goldmark.Convert` →
  `bluemonday.UGCPolicy().SanitizeBytes()`) whenever `notes` is created or
  updated, store both the raw markdown (unchanged, still editable) and the
  rendered/sanitized HTML.
- Include `notes_html` in the trip/item response DTOs.
- Frontend: replace the raw interpolation at `trip-detail-page.js:76` and
  the `textContent` assignment at `location-view-page.js:106` with
  `innerHTML = notesHtml` — safe now because sanitization already happened
  server-side before the value ever reached the API response.
- Leave document notes (`document-list.js:50`, already HTML-escaped, short
  strings) and itinerary day notes (`itinerary-tab.js` — editable textarea
  only, no read view) as plain text; not worth pulling into scope here.

## 3. Documents: serve inline instead of forcing download

`internal/httpapi/documents.go`'s `handleDownloadDocument` unconditionally
sets `Content-Disposition: attachment; filename="..."` regardless of MIME
type. `ContentType` today is only the client-declared multipart header at
upload time — never server-verified.

- Add a small inline-safe-list (`application/pdf`, `image/png`,
  `image/jpeg`, `image/gif`, `image/webp`, `text/plain` — **excluding**
  `image/svg+xml`, which browsers execute as script if rendered inline, a
  real inline-disposition XSS vector).
- Set `Content-Disposition: inline; filename="..."` when `doc.ContentType`
  is in the safe-list, else keep `attachment` as today.
- Since the stored `ContentType` is currently just the trusted-by-default
  client-supplied header, add server-side verification at upload time in
  `uploadDocument` via `http.DetectContentType` (sniff first 512 bytes) so
  a mislabeled file can't get inline treatment it shouldn't have.

## 4. Linked images: fetch and cache locally instead of hotlinking

`internal/httpapi/media.go`'s `handleCreateMediaURL` validates the pasted
URL is http(s) with a host, then stores it verbatim in
`media_assets.external_url` — never fetched. The frontend then hotlinks that
URL forever.

- On create, `http.Get` the URL server-side (with a timeout and a max-size
  cap — neither exists for outbound fetches anywhere in the codebase today,
  so this is new logic), pipe the body through the existing
  `internal/imaging.DecodeAndResize` (already used by `handleUploadMedia`
  for uploaded images), then `Blob.Put` it via `internal/storagefs` under a
  trip-scoped key exactly like an upload does.
- Populate `storage_path`/`content_type`/`width`/`height` on the
  `media_assets` row for `kind = 'url'` rows too (keep `kind = 'url'` as
  provenance metadata — "originally linked from X" — but stop using it to
  decide *how* the asset is served).
- Update `mediaAssetToResponse` and `handleServeMedia`'s gate (currently
  `if asset.Kind != "upload" || asset.StoragePath == nil`) to serve any
  asset with a non-nil `storage_path` through `/api/media/{id}/file`,
  regardless of `kind` — so linked images behave identically to uploads
  from this point on.
- If the fetch fails (dead link, non-image content, too large), return a
  clear error to the frontend rather than silently falling back to
  hotlinking.

## 5. Tab state in the URL + breadcrumb navigation

Confirmed: `trip-detail-page.js`'s Overview/Locations/Map/Itinerary/
Documents tabs are pure local JS state (`let activeTab = "locations"`,
`trip-detail-page.js:23`) with no URL/history involvement at all — reload,
back/forward, and deep links all lose the active tab. There is also no
shared breadcrumb component anywhere; every page hand-rolls a single-hop
"← Back" link (`trip-detail-page.js:28`, `location-view-page.js:34`,
`trip-editor-page.js:38`, `location-editor-page.js:40`).

- Add tab sub-routes to `app.js`'s `routes` array:
  `/trips/:tripId/locations`, `/trips/:tripId/map`,
  `/trips/:tripId/itinerary`, `/trips/:tripId/documents`, and
  `/trips/:tripId` (or `/overview`) for Overview — all rendered by
  `renderTripDetailPage`, router already captures arbitrary `:param`
  segments so no router changes needed. These are 3-segment patterns with
  distinct literals, so no ordering conflict with the existing
  `/trips/:tripId/edit` — but note the ordering convention for any future
  route added at this same depth (literal patterns must precede `:param`
  patterns at the same segment count, per the existing comment in
  `app.js:16-19`).
- In `trip-detail-page.js`: read the tab from the route param instead of
  the closure variable (default to `"locations"` when absent, preserving
  Stage 02's "land on Locations" decision); replace the tab-click handler
  (`trip-detail-page.js:46-51`) with a call to the router's `navigate()`
  helper so tab switches produce real history entries and back/forward work
  through the router's existing single `popstate` listener — no new
  listener needed.
- Add a small shared breadcrumb render function (e.g.
  `renderBreadcrumb(container, { tripName, tab, itemName })` in a new
  `web/js/components/breadcrumb.js`), replacing the single-hop back-links in
  `trip-detail-page.js`, `location-view-page.js`, `trip-editor-page.js`, and
  `location-editor-page.js` with a real trail: `Trips ▸ TripName ▸ Tab [▸
  ItemName]`, each segment a working link (the trip-tab segment now has a
  real URL to link to, thanks to the routing change above).

## 6. New feature: trip checklists

Follows the existing `documents` feature end to end as a template
(migration → sqlc queries → domain struct → dual store impl → HTTP handlers
→ router → frontend component → i18n keys) — no new architectural pattern
needed, just two nested resource types (checklist + items) instead of one.

- **Migration** (`internal/db/migrations/{sqlite,postgres}/000X_add_
  checklists.*.sql`): `checklists` (id, trip_id, title, position,
  created_at) and `checklist_items` (id, checklist_id, text, checked,
  position, created_at), FKs with `ON DELETE CASCADE`, indexed on
  `trip_id`/`checklist_id` respectively.
- **sqlc queries** (`internal/db/sqlc/queries/checklists.sql`): Create/List/
  Delete for checklists; Create/Toggle/Delete for items (reorder can follow
  the same `position` pattern already used elsewhere, or be deferred — decide
  during implementation based on how simple a minimal version is).
- **Domain + store**: `Checklist`/`ChecklistItem` structs in `domain.go`,
  interface methods in `store.go`, dual impl in `sqlite_store.go`/
  `postgres_store.go` mirroring the `documents` pattern
  (`sqlite_store.go:580-643` as the template).
- **HTTP handlers** (`internal/httpapi/checklists.go`): trip-scoped list/
  create/delete for checklists, item-scoped create/toggle/delete for
  checklist items; routes added in `router.go` alongside the existing
  document routes.
- **Frontend**: new `web/js/components/checklist-list.js` (title, add-item
  input, checkable item rows, delete), wired into `trip-detail-page.js`'s
  `TABS` array and tab-render switch the same way `document-list.js` is
  today (`trip-detail-page.js:8,10,62-63`) — note this milestone lands
  after Milestone 5, so the new tab slots into the now-route-backed tab
  system directly instead of the old closure-state one.
- **i18n**: new flat `checklists.*` keys in both `en.json`/`de.json`.

## Build order

1. **CI / testing foundation** (Section 1) — build/vet/JS-syntax/i18n-parity
   checks wired into GitHub Actions, plus a dev seed-data script. Lands
   first so everything after it is checked automatically.
2. **Markdown rendering for notes** (Section 2) — vendor renderer +
   sanitizer, apply to trip and item notes, closing the trip-notes XSS hole.
3. **Documents inline display** (Section 3) — safe-list + `Content-
   Disposition: inline`, server-side MIME sniffing at upload.
4. **Linked images cached locally** (Section 4) — fetch-on-paste, reuse
   `imaging`/`storagefs`, serve via the existing media route regardless of
   `kind`.
5. **Tab state in URL + breadcrumbs** (Section 5) — tab sub-routes, drop
   closure-state tab tracking, shared breadcrumb component.
6. **Trip checklists** (Section 6) — new feature, built on the
   `documents`-pattern template, wired into the now-route-backed tab system
   from Milestone 5.

## Workflow: one milestone at a time, with a manual-testing checkpoint

Same loop as Stage 02, applied to all 6 milestones above:

1. Implement that milestone's changes.
2. Verify it myself — `go build ./... && go vet ./...`, the new CI checks
   once Milestone 1 exists, curl smoke tests for backend changes, a
   Playwright click-through for UI changes.
3. Remove the `todo.md` bullet(s) that milestone resolves (they cite their
   source, so it's a direct lookup — e.g. Milestone 3 removes the
   "Documents always force a download" bullet from the "Confirmed bugs /
   gaps" section) so `todo.md` stays an accurate list of what's still
   outstanding, not a historical log.
4. Commit just that milestone's changes, including the `todo.md` edit (one
   commit per milestone).
5. Start the dev server (`make dev`) and hand back control — **stop and
   wait** for manual testing rather than continuing automatically.
6. Resume only once told to — "continue" moves to the next milestone;
   feedback/bugs get addressed, re-verified, and committed as a follow-up
   first.

## Verification

- Backend: `go build ./... && go vet ./...` plus the new CI script checks;
  curl smoke tests for the documents `Content-Disposition` change (confirm
  a PDF/image request now gets `inline`, a `.zip` still gets `attachment`)
  and for the linked-image fetch (confirm a pasted image URL round-trips
  through `storage_path` and is served via `/api/media/{id}/file`, not the
  raw external URL).
- Migrations: run the new checklists migration against a non-empty SQLite
  DB and re-run the Postgres parity smoke test used in prior stages.
- Frontend: browser click-through per milestone — confirm markdown renders
  (and that a note containing `<script>` no longer executes), confirm tab
  switches produce real URL changes and back/forward works, confirm
  breadcrumbs show the correct trail on every page, confirm checklist items
  can be added/checked/deleted.
- CI: push a branch and confirm the new GitHub Actions workflow actually
  runs and passes/fails correctly (e.g. temporarily break an i18n key to
  confirm the parity check catches it, then revert).
