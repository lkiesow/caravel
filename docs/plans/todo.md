# Caravel — TODO / Backlog

Everything below is **not yet built**. Compiled from three sources: Stage 01's
plan (`stage-01.md`) and Stage 02's plan (`stage-02.md`) — both marked
"complete" for their own scope, but each explicitly deferred things — plus
`notes.md` (hands-on notes jotted down separately). Nothing here is
prioritized or scheduled; this is raw input for planning the next stage.

Each item cites where it came from, so the reasoning behind it isn't lost.

## Confirmed bugs / gaps (verified by reading the current code)

These aren't just suspicions from `notes.md` — each was checked against the
actual source before going in this list.

- **Markdown notes are never rendered as markdown.** Every notes field
  (`trip.form.notesHint`, item notes) is labeled "Markdown supported," but
  trip notes are inserted as plain interpolated text
  (`web/js/pages/trip-detail-page.js`) and item notes via `.textContent`
  (`web/js/pages/location-view-page.js`) — no markdown parser is vendored
  anywhere in `web/js/`. The label is actively misleading right now. We should support Mardown here, but also in a few other places where entering longer texts is allowed.
  (`notes.md`, verified)
- **Documents always force a download, never display inline.**
  `handleDownloadDocument` (`internal/httpapi/documents.go`) unconditionally
  sets `Content-Disposition: attachment`, so a PDF or image document always
  downloads instead of opening in the browser like the note expected.
  (`notes.md`, verified)
- **Linked (pasted-URL) images are hotlinked forever, never cached
  locally.** `handleCreateMediaURL` (`internal/httpapi/media.go`) stores the
  pasted URL verbatim and the frontend serves it directly from that URL for
  as long as the trip exists — if the source disappears or changes, the
  image silently breaks or changes underneath the trip. Uploaded images
  don't have this problem; only pasted ones do. (`notes.md`, verified)
- **Tab state isn't reflected in the URL, so browser back/forward skips
  over it.** The router itself is clean (single `popstate` listener in
  `router.js`, no page bypasses it with a direct `pushState`) — but the
  trip detail page's Overview/Locations/Map/Itinerary/Documents tabs switch
  via internal JS state, not real routes. So clicking through tabs then
  hitting Back doesn't step back through tabs — it jumps straight past the
  trip to whatever page was open before it. This is what
  prompted the "back/forward seems to not always work" note, even though
  the pushState/popstate plumbing underneath it is correct (`notes.md`, code-verified as the cause)

## Deferred from Stage 01 (explicit "future phases," Section 7)

Stage 01's schema/architecture were deliberately designed so none of these
require a redesign later — they're additive, not blocked, but none are built:

- **Sharing/collaboration/permissions** — owner/participant/viewer roles on
  a trip. Needs a `trip_collaborators` join table and a change from
  "owner_id == current user" to "role >= X" checks.
- **Public shareable links** — unauthenticated read-only trip view via a
  token. Needs a `share_links` table (token, trip_id, scope, expires_at)
  plus an unauthenticated route variant. IDs are already non-guessable
  UUIDs, so this is low-friction whenever it's picked up.
- **Expenses / cost-splitting** — a new `expenses` table referencing
  `trip_id`/optionally `item_id`, no changes to existing tables.
- **Federation between self-hosted instances** — real sync-protocol design
  still needed; v1 only avoided the integer-PK/local-only-ID mistakes that
  would have made this harder later.
- **Trip journal with photos** — a `journal_entries` table (trip_id, date,
  body markdown) reusing the existing `media_assets` pipeline for photos.
- **S3-compatible object storage** — swap the `internal/storagefs` `Blob`
  implementation from local filesystem to S3-compatible (MinIO, Backblaze,
  etc.); the interface already isolates callers from the backend.
- **Prometheus/OpenMetrics metrics** — a `GET /metrics` endpoint via
  `promhttp.Handler()`. The routing reservation (`/metrics` outside `/api`
  and outside session-auth middleware) is already in place; the actual
  instrumentation (HTTP request count/duration/status, DB query duration,
  upload counts/sizes, session counts) is not.
- **OpenID Connect / external auth providers** — `auth_identities` already
  supports a `provider` column beyond `'local'` for exactly this; no
  provider integration exists yet.

## Deferred from Stage 01 (smaller items, mentioned in the plan body)

- **No manual light/dark theme toggle.** Theming is purely
  `prefers-color-scheme`-driven; Stage 01's plan explicitly structured this
  "so a manual `data-theme` override can be added later," but no such
  control exists — no way to override the OS setting from inside the app.
- **No itinerary entry reordering.** `itinerary_entries.sort_order` exists
  in the schema and Stage 01 floated "native drag-and-drop or a minimal
  pointer-events reorder," but `itinerary-tab.js` has no reordering UI at
  all — entries render in whatever order the API returns, with no drag
  handle, no up/down buttons.
- **No "add to every day of this stay" convenience action.** Stage 01
  floated this for multi-day items (e.g. a 3-night hotel stay) driven by
  the item's date range, but adding an item to a day is always manual,
  one-day-at-a-time via each day's own "Add item" dropdown.
- **No in-app language switcher.** `i18n.js` has a working `setLocale()`
  function and a `localStorage` cache for it, but nothing in the UI calls
  it — locale is autodetected from the browser/OS only, with no way to
  override it from inside the app.
- **No frontend build step / bundler** — deliberate for v1 ("revisit only
  if it becomes a real pain point"), not a bug, just worth remembering this
  was a conscious deferral, not an oversight, if load-time ever becomes a
  complaint.

## Deferred / scope-limited from Stage 02

- **Location (item) editor's create mode doesn't offer location, links,
  dates, or documents** — only the staged cover photo got the same
  create-mode treatment trips got. A brand-new item can't have a pin,
  links, dates, or attachments until after its first save, when the editor
  switches to edit mode. (Matches `notes.md`'s "new location editor should
  allow setting all the things the edit location allows" almost exactly.)
- **No drag-and-drop file selection in the documents "Add document"
  dialog** — explicitly scoped as "nice-to-have, not required" when built;
  file selection is click-to-browse only.
- **User menu dropdown only has "Log out."** Built "structured so more
  items can be added later" (per the Stage 02 plan) — admin/settings items
  were explicitly deferred, not forgotten.

## Testing / CI / dev-workflow gaps (`notes.md`)

- **No real Playwright UI test suite.** Stage 03 added GitHub Actions CI
  (`.github/workflows/ci.yml`) running a build check, `go vet`, a JS syntax
  check across `web/js/`, and an i18n-key-parity check
  (`scripts/check_i18n.py`, generalized to any number of locale files) —
  but everything UI-facing is still verified manually or via one-off
  Playwright runs during development, not a checked-in, repeatable suite
  (using Firefox specifically, per the original note). Still wanted.
- **Migrations should be collapsed/squashed before the first real
  release.** There are three migration files per dialect now
  (0001/0002/0003); since nobody has actually deployed this yet, squashing
  them into a single `0001_init` is safe and worth doing before that
  changes.

## New feature ideas (not previously in either plan, from `notes.md`)

- **Re-evaluate Leaflet+OSM vs. Google Maps.** Stage 01 already weighed this
  and chose Leaflet+OSM (no API key/billing, low-regret since the tile URL
  is a one-line swap later) — this note re-opens that question rather than
  reporting a gap. Worth a deliberate "still the right call, or not"
  decision rather than silently re-litigating it.
- **LLM-assisted metadata fetching for locations** (e.g. a small local model
  + web search to auto-fill an item's details from its title). Noted as
  "not that important for the MVP" — low priority by the user's own note.
- **Trip checklists** — a new feature: a checklist (title + checkable list
  items) attached to a trip, for pre-trip prep. Would need a new
  `checklists`/`checklist_items` table pair; no existing schema covers this.

## Also worth a breadcrumb-navigation pass

`notes.md` calls out wanting breadcrumb navigation. Right now every page
below the top level has exactly one "← Back" link to its immediate parent
(trip → trips list, item → trip) — there's no multi-level trail (e.g.
Trips ▸ Iceland Ring Road ▸ Locations ▸ Kirkjufell) shown anywhere.
