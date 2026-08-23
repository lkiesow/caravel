# Stage 11 — The Files view, redesigned

## Context

The Files view is the last part of the app still rendered as a bare flex row:
`filename-as-raw-link · size · — location · note · ✕`
([document-list.js:87-92](web/js/components/document-list.js#L87-L92)). Three
problems fall out of that shape:

- **The link is the row.** A blue-ish filename is the only tappable thing, and
  Stage 10 Milestone 7 already had to bolt `min-height: var(--tap-min)` onto
  `.documents li a` after the sweep caught it at 22px — the row was only passing
  by the accident of a filename that happened to wrap.
- **Mobile is a pile-up.** The same milestone had to add a `flex-wrap` +
  `order: 1` override at ≤640px to stop the location label truncating to
  "— Foss…". That's four competing pieces of text on one line, patched rather
  than designed.
- **A note is write-once.** It can only be set in the upload form; there is no
  PATCH endpoint, so a file uploaded without a note keeps none forever. And
  when the filename is a server-ish blob like
  `5d2ffd5f-b621-41d9-9b10-173d5a72f860.png`, the note is the only readable
  name the file has — but it renders as a small italic afterthought.

This stage replaces the row with a **tappable file card**: a type tile, a
readable name, one meta line, and a single ⋮ overflow so nothing competes with
the name. Upload becomes a **drop zone**. Decided with the user up front:

1. **Note wins the title.** With a note, the note is the card title and the
   filename drops into the meta line. Without one, the filename is the title.
2. **The ⋮ menu holds Edit note and Delete.** No "Open/Download" item — the card
   body *is* the download link, so opening a file stays one tap.
3. **Everywhere the list appears** — trip Files tab, a location's Files card,
   and the create-page staging list all get the same card.
4. **Its own stage**, one milestone per commit, per `CLAUDE.md`.
5. **The `documents` → `files` rename rides along**, all the way down to the
   schema, and the old `/trips/:id/documents` URL simply goes away rather than
   redirecting. It lands *first*, as a rename-only commit, so the rest of the
   stage isn't written in the vocabulary it's about to lose.

Also closed on the way: the duplicated `formatSize` (two copies today), the dead
`.documents .empty` CSS rule, and — the reason this stage is worth more than its
own screen — `menu.js` finally gets the **action-item mode** that `todo.md`
names as the blocker for both the checklist ⋮ menu and the settings entry in the
user menu.

---

## 1. `documents` → `files`, top to bottom

One mechanical, behaviour-preserving commit. It goes first because every file
the rest of the stage touches is a file this renames, and writing new code
against a name that's about to change is how a tree ends up half-migrated —
which is exactly what `todo.md` has been warning about since Stage 05.

- **Schema.** `0006_rename_documents_to_files.up/down.sql` in **both**
  `internal/db/migrations/sqlite/` and `.../postgres/`. Postgres:
  `ALTER TABLE documents RENAME TO files;` plus `ALTER INDEX
  idx_documents_trip_id RENAME TO idx_files_trip_id;` (and the `item_id` one).
  SQLite has `ALTER TABLE ... RENAME TO` but **no** `ALTER INDEX`, so the two
  indexes are dropped and recreated under the new names — the one real
  dialect divergence here. `down` reverses both.
- **Queries.** `queries/documents.sql` → `queries/files.sql`, every statement
  renamed (`CreateDocument` → `CreateFile`, `ListTripDocuments` →
  `ListTripFiles`, …), then `sqlc generate` by hand from `internal/db/sqlc/`
  for both dialects — which regenerates the row structs (`Document` → `File`,
  `ListTripDocumentsRow` → `ListTripFilesRow`) and both `querier.go`.
- **Go.** `domain.go`'s `Document`/`DocumentDetail` → `File`/`FileDetail`, the
  `store.go` interface methods, both hand-written adapters
  (`sqlite_store.go`, `postgres_store.go` — including
  `sqliteDocumentToDomain`/`postgresDocumentToDomain`), `cmd/seed/main.go`,
  and `internal/httpapi/documents.go` → `files.go` with its handlers,
  `documentResponse` → `fileResponse`, `loadOwnedDocument` → `loadOwnedFile`,
  `maxDocumentUploadBytes` → `maxFileUploadBytes`. Tests move with them
  (`documents_test.go` → `files_test.go`, and the route table in
  `ownership_test.go`).
- **API surface.** `/api/trips/{id}/documents`, `/api/items/{id}/documents` and
  `/api/documents/{id}[/download]` all become `/files`
  ([router.go:104-116,153-154](internal/httpapi/router.go#L104-L116)), and the
  `download_url` those responses hand out
  ([documents.go:86](internal/httpapi/documents.go#L86)) follows. Only this app
  calls these, so there is no compatibility surface to keep.
- **Storage prefix.** New uploads land at `{tripID}/files/{id}-{name}`
  ([documents.go:126](internal/httpapi/documents.go#L126)). **No data
  migration:** `storage_path` is recorded per row, so files uploaded before the
  rename keep working from their old key — only new keys change shape. The
  item-level key (`{tripID}/items/{itemID}/…`) never said "documents" at all.
- **Frontend.** Tab key `documents` → `files` in
  [trip-tabs.js:26](web/js/trip-tabs.js#L26), which by itself renames both the
  route (`/trips/:tripId/files`, built from the key in
  [app.js:22](web/js/app.js#L22)) and the i18n key it looks up. Then
  `document-list.js` → `file-list.js` (`renderDocumentList` →
  `renderFileList`), the `.documents` / `.document-*` CSS class family, and the
  call sites in `trip-detail-page.js`, `location-view-page.js` and
  `location-editor-page.js` (including `draft.documents` → `draft.files`).
- **i18n.** `documents.*` → `files.*`, `item.detail.documents` →
  `item.detail.files`, `trip.tabs.documents` → `trip.tabs.files`, in **both**
  `en.json` and `de.json` — `scripts/check_i18n.py` gates parity, and
  `scripts/i18n.py unused` should still report clean afterwards. The English
  and German *copy* is unchanged; only the keys move.
- **Tests.** `TRIP_TABS` in
  [tests/ui/helpers/scenarios.js:28](tests/ui/helpers/scenarios.js#L28) picks up
  the new segment, which is what makes the UI sweeps assert the new URL.
- **Out:** the `item` → `location` half of that same `todo.md` entry. Different
  rename, different blast radius; it stays in the backlog with the "documents"
  half struck out.

**Verify.** `make ci` and `make test-ui` green with **zero** intended behaviour
change — a rename that needs a test edited for anything other than a name is a
rename that changed something. Plus: the old `/trips/:id/documents` URL now
lands on the not-found page (deliberate — no redirect), a fresh database
migrates from empty, the existing `make dev` database migrates over 0006 with
its rows intact, `down` reverses cleanly, and a file uploaded *before* the
rename still downloads afterwards (the storage-path point above, which is the
one thing here that could silently break).

**Done.** Landed as planned, in one commit, with no behaviour change beyond the
URL and the vocabulary.

Both `0006_rename_documents_to_files` migrations are as the plan described, and
the dialect split is real: Postgres renames the table and both indexes in place,
SQLite renames the table and then drops and recreates the two indexes, because
it has no `ALTER INDEX`. `queries/documents.sql` became `queries/files.sql` with
every statement renamed, `sqlc generate` produced `File` / `ListTripFilesRow` in
both dialect packages, and the two stale `documents.sql.go` files were removed
rather than left behind. On the Go side `Document`/`DocumentDetail` are
`File`/`FileDetail`, `internal/httpapi/documents.go` is `files.go`, and the
routes are `/api/trips/{id}/files`, `/api/items/{id}/files` and
`/api/files/{fileId}[/download]`. One name collision needed a hand fix: the
mechanical `doc` → `file` pass inside `uploadFile` walked into the existing
`file, header, err := r.FormFile("file")`, so the created row is now `row`.

Frontend: the tab key change alone moved the route to `/trips/:id/files` and
the i18n lookup to `trip.tabs.files`, as designed;
`components/document-list.js` is `components/file-list.js` exporting
`renderFileList`; the `.documents`/`.document-*` CSS family is
`.files`/`.file-*`; `documents.*` became `files.*` in both locales (126 keys,
still in sync, and `scripts/i18n.py unused` still reports clean). Comments
naming the tab "Documents" — in `trip-tabs.js`, `app.js`, `menu.js`, `base.css`
and `check_js.sh` — were reworded, including the tab-width note whose "60px"
measurement was taken back when that label really did read "Documents"; it now
says so rather than pretending the measurement was of "Files".

**Verified.** `make ci` green and `make test-ui` **12/12**, with the only test
edit being `TRIP_TABS`' new segment — which is what makes the sweeps assert the
new URL. Migration behaviour was checked on a *copy* of the real dev database
first: up renames the table and both indexes with all 4 rows intact, down
reverses it exactly (table and index names back, 4 rows), and up again is
clean; a fresh empty database migrates straight to version 6 with a `files`
table. Then the live `make dev` database was migrated for real by the restart,
also with its rows intact. The storage-path risk was checked end to end rather
than reasoned about: a file uploaded *before* the rename still downloads
(`GET /api/files/{id}/download` → 200 and the right bytes, from its old
`{trip}/…` key), while a fresh upload lands under the new
`{trip}/files/{id}-{name}` prefix — probe file deleted afterwards, so the seed
is untouched. In the browser, `/trips/:id/files` renders both seeded files with
the location label unchanged, and the old `/trips/:id/documents` reaches the
"Not found" page, which is the decided behaviour: no redirect.

---

## 2. Backend: a note can be edited after upload

New `PATCH /api/files/{fileId}` (post-rename naming, per Milestone 1), body
`{"note": "..."}`. `""` or `null`
clears it back to SQL NULL, so the "No note" state is reachable, not a one-way
door.

- **Query.** `UpdateFileNote` in `queries/files.sql`, scoped
  `WHERE id = ? AND trip_id = ?` — the same (id, tripID) scoping `DeleteFile`
  already uses, so an owned-trip check is the whole authorization
  story. Returns the updated row (`:one`) so the handler can respond with the
  full `documentResponse` and the frontend can re-render from the answer rather
  than from its own optimistic guess.
- **Regenerate.** `sqlc generate` by hand from `internal/db/sqlc/` — **both**
  dialects, per `CLAUDE.md`, plus both `querier.go` files.
- **Store + adapters.** Add to the interface in
  [store.go](internal/db/store.go) beside `DeleteFile`, then both hand-written
  adapters (`sqlite_store.go`, `postgres_store.go`), mapping through the
  existing per-dialect row→domain helper — this returns a plain `File`, not the
  joined `FileDetail`, so no new mapper is needed.
- **Handler.** `handleUpdateFileNote` in `internal/httpapi/files.go`, reusing
  `loadOwnedFile` (the same helper the delete handler uses,
  [documents.go:230](internal/httpapi/documents.go#L230) pre-rename) and
  responding via the existing `fileToResponse`. `item_title` stays null here by
  design, consistent with every non-list endpoint.
- **Route.** `r.Patch("/", ...)` inside the existing `r.Route("/files/{fileId}")`
  group, [router.go:112-116](internal/httpapi/router.go#L112-L116).

**Verify.** `make ci`, plus Go tests: set a note, change it, clear it with `""`
(asserting the column is NULL, not the empty string), and 404 for another user's
document — the last by adding PATCH to the route table in
[ownership_test.go:174](internal/httpapi/ownership_test.go#L174), which already
sweeps list/download/delete/upload.

**Done.** Landed as planned. `UpdateFileNote` in `queries/files.sql` uses
`sqlc.narg(note)` so the parameter is nullable, scoped `WHERE id = ? AND
trip_id = ?` exactly like `DeleteFile`; both dialects regenerated. The store
method takes `note *string` and both adapters map it through the existing
`nullString` helper and the per-dialect row→domain function, so nothing new was
written for the mapping. `handleUpdateFileNote` reuses `loadOwnedFile`,
`readJSON` and `fileToResponse` and responds with the updated row.

The one judgement call not in the plan: the request's `note` is a pointer, but
since the body has exactly *one* field, an absent note and an explicit `null`
can only mean the same thing — clear it. Whitespace is trimmed the way the
upload path already trims it, so `"   "` clears rather than storing a note made
of spaces.

**Verified.** `make ci` green, `make test-ui` 12/12 (nothing frontend changed,
run as a regression check). `TestUpdateFileNote` is a table over all seven ways
a note can be written or cleared — set, change, trimmed, empty string,
whitespace, explicit null, omitted — each starting from a *set* note so the
clearing cases really clear something, and each asserted twice: on the PATCH
response and again on a re-read through the trip listing, because a handler
that merely echoed its input would pass the first check alone. It also asserts
the patch touches nothing else (filename, size, and `item_title` still null) and
that an unknown id is 404 while a malformed body is 400. `TestFileRoutesRejectAnotherUser`
gained the PATCH row plus a check that the *denied* PATCH left the owner's note
NULL — a 404 that still wrote would be the worst of both.

Proven non-vacuous: making the handler store `""` instead of nil fails
`cleared_by_empty_string` and `cleared_by_whitespace` with `note is "", want
null`, which is the bug verbatim. Live check against `make dev`: PATCH with
`"  Ferry ticket  "` stored `'Ferry ticket'`, PATCH with `""` left the column
`NULL` (checked in SQLite, not just in the response), and the seeded note was
restored afterwards so the dev fixtures are unchanged.

**Not verified: Postgres**, as ever — same generated SQL shape, no local
instance to run it against.

---

## 3. `menu.js` grows an action-item mode

The file ⋮ is not a selection — "Delete" isn't a state the menu is now in — so
`role="menuitemradio"` + `aria-checked` is wrong for it.
[menu.js:72](web/js/components/menu.js#L72) emits exactly that for every item.

- Per-item opt-out: an item carrying `onSelect` semantics stays a radio; an item
  marked as an action renders `role="menuitem"` with no `aria-checked` and no
  check-mark slot (its `iconName` takes the leading slot instead — the mechanism
  at line 73 already does this). Keep `activeValue`/`neutralValue` working
  untouched so the locations filter and the tab bar's More menu are unaffected.
- A `danger: true` item picks up the destructive color already defined for
  `.btn-danger`, so Delete reads as Delete.
- The "don't re-fire when re-selecting the current value" guard
  ([menu.js:138](web/js/components/menu.js#L138)) must **not** apply to action
  items — pressing Delete twice in a row has to fire twice.

Scope discipline: this milestone teaches the component the mode and uses it from
the file card only. Migrating `user-menu.js` and the checklist ⋮ onto it stays a
`todo.md` entry — but the blocker named there is gone after this.

**Verify.** `make ci` + `make test-ui`; extend
[tests/ui/menu.spec.js](tests/ui/menu.spec.js) (which already drives open/toggle/
outside-click/Escape in both locales) with the assertion that an action item
exposes `role="menuitem"` and no `aria-checked`, and that the radio menus are
unchanged.

**Done.** The component change is small and exactly as planned: an item marked
`action: true` renders `role="menuitem"` with no `aria-checked` and no
check-mark slot, `danger: true` tints it, `syncLabel` now stamps `aria-checked`
only on `[role="menuitemradio"]` rows so an action item can't have one invented
for it, and the click handler fires `onSelect` on every click for actions —
skipping the "same value, don't re-fire" guard, which is exactly wrong when
pressing Delete twice has to mean twice. Radio and action items can share one
menu; only the action ones opt out.

**Deviation, deliberate:** the plan had this milestone teach the mode and use it
"from the file card only", but the card doesn't exist until Milestone 4 — which
would have left this commit unverifiable behaviourally. So the consumer is the
*existing* file row: its bare ✕ became a ⋮ holding **Edit note** and
**Delete/Remove**, which also pulls `promptDialog` forward from Milestone 5.
Milestone 4 is now purely layout, and each commit stands on its own. Three new
i18n keys in both locales (`files.actions`, `files.editNote`,
`files.notePrompt`); `files.notePlaceholder` is reused as the dialog's
placeholder.

`promptDialog({ messageKey, value, placeholderKey })` threads one text input
through `dialog.js`'s existing private `open()`, as planned. It resolves to the
typed string or to **null** when dismissed, so "saved an empty value" (which is
how a note gets cleared) stays distinguishable from "changed their mind" — a
bare `""` could not carry that. Two details the plan didn't name: Enter in the
field closes with confirm (without it, Enter falls through to `<dialog>`'s
default button, which is Cancel), and the field takes focus over the first
button, since typing is the point of the box.

**Two bugs found while verifying, both mine, both invisible without measuring.**
First, `.menu__action--danger` silently lost to `.menu__dropdown button` on
specificity, so Delete rendered in the ordinary text color; it is now written
as `.menu__dropdown .menu__action--danger`, and the spec asserts the *computed*
color rather than the class so this can't come back. Second, adding a `<ul>` to
each row meant `.files li` began matching the dropdown's own `<li>`s, so the
row's flex and its ≤640px wrap rules were leaking into menu items — the file-row
rules are now `.files > li` (and `.files > li > a`).

**Verified.** `make ci` green, `make test-ui` **14/14** — the two new tests being
`menu.spec.js`'s file-row menu in both locales: `role="menuitem"` on both items,
zero `[role="menuitemradio"]` and zero `[aria-checked]` in the dropdown (the
mirror image of the tab bar's assertions), German copy, the destructive tint,
the 44×44 icon-only trigger and its accessible name, and Escape closing. It
stops short of clicking an action, since both mutate the shared seed. Proven
non-vacuous: dropping `action: true` from the two items fails it on the roles.
By hand at 324×756 against `make dev`: Edit note prefills the current note,
saves on Enter, the row re-renders from the PATCH response; an emptied field
clears the note; Cancel with text typed changes nothing; and the seeded note was
restored afterwards. The staging path was driven too, on a new location's Files
card — a staged pick's note is edited locally with no request, and Remove drops
it with no confirmation, leaving the empty state — and nothing was created,
since the location form was never submitted.

---

## 4. The file card

The visual core. `renderFileList` (the renamed
[document-list.js:29](web/js/components/document-list.js#L29)) keeps its
full-rebuild-per-render design (the comment at lines 34-51 explains why, and it
still holds) — only the row template and the CSS change.

```html
<li class="file-card">
  <a class="file-card__body" href="{download_url}" target="_blank" rel="noopener">
    <span class="file-card__tile">{icon}</span>
    <span class="file-card__text">
      <span class="file-card__name">…</span>
      <span class="file-card__meta">…</span>
    </span>
  </a>
  <span class="file-card__size">404.0 KB</span>   <!-- desktop only -->
  <div class="file-card__actions"><!-- renderMenu ⋮ --></div>
</li>
```

- **Name / meta rule** (decision 1): `name = note ?? filename`;
  `meta = [filename-if-note-was-used, note-absent-hint]` plus the size on mobile,
  where there is no room for a size column. Desktop shows size in its own
  right-aligned column and keeps the meta line for the filename / "No note".
  The location label (`item_title`, trip-level list only) joins the meta line
  rather than being a fifth element — it's the same information the ≤640px
  override was already forcing onto a second line, so this deletes that override
  instead of adding to it.
- **Middle truncation, CSS-only.** Split the filename into stem + extension and
  emit them as two spans; the stem gets `min-width: 0; overflow: hidden;
  text-overflow: ellipsis`, the extension `flex: none`. That renders
  `5d2ffd5f-b621-4… .png` exactly as in the mockup with no JS measurement and no
  resize listener.
- **Type tile.** Icon chosen from `content_type` (already server-sniffed and on
  every row): `image/*` → `image`, `text/*` and `application/pdf` → `file-text`,
  everything else → `file`. `file` is **not** in the sprite — add it to `ICONS`
  in `scripts/gen_icon_sprite.py` and regenerate per the `CLAUDE.md` recipe,
  diffing to confirm the existing symbols come out byte-identical.
- **Header summary.** `FILES · 3` / `459 KB total` from the mockup: the callers
  already own the heading (the tab, and the `.editor-card` on the location
  editor), so the component emits a `.file-list__summary` line with the count
  and the summed size. `t()` supports `{name}` interpolation and a `key_plural`
  form ([i18n.js:52](web/js/i18n.js#L52)) — use both, so "1 file" isn't "1
  files" in either locale.
- **Tap targets.** The card body is the link, so it clears 44px by construction
  and the bolted-on `min-height` for the old file-row link (base.css ~1449) goes
  away with the rest of the old rules.
- **Staging mode** renders the same card with no `href` (a `<span>` body), no
  location label, and its ⋮ offering Edit note (local) and Remove (no confirm) —
  the same asymmetry it has today, just inside the new shape.

**Verify.** `make ci` + `make test-ui` (the overflow, heading, a11y-name and
tap-target sweeps in `tests/ui/` already cover the Files route). Plus a mobile
pass at 324×756 via the Playwright MCP tools against `make dev`, asserting the
computed layout — card count, no horizontal overflow, the tile/name/meta
present — rather than eyeballing a screenshot.

**Done.** The card is as drawn: type tile, name, one meta line, size, ⋮. `file`
joined the sprite (`ICONS` + regenerate per `CLAUDE.md`; the diff is four added
lines and no existing symbol changed), and `tileIconName` buckets the
server-sniffed content type three ways — `image/*`, `text/*` + `application/pdf`,
everything else. The two render modes collapsed into one template via a small
view model, so a staged pick (a `File` object) and an uploaded row (an API row)
no longer have separate card markup — that also removes the duplication the old
two-branch template had. Middle truncation is CSS-only as planned: the stem
ellipsizes inside a wrapper, the extension never does, and a storage-blob name
reads `5d2ffd5f-b621-41….png`.

**Deviation, deliberate:** Milestone 6's "point `location-view-page.js` at the
shared card" is done here instead. Keeping the old row CSS alive for one more
commit *just* for that page would have meant either two file-row designs in the
tree or a throwaway `files--plain` class, both worse than moving one bullet
forward. It needed a `readOnly` mode (no add row, no ⋮ — everything editable on
that page lives behind its Edit button) plus a `rows` option so the caller can
hand over the list it already fetched to decide whether to render its card at
all; verified as exactly one request for that list, not two. Its duplicate
`formatSize` went with it. Milestone 6 keeps the `format.js` move and the
i18n/dead-CSS sweep.

**Punctuation was the whole difficulty**, and it took three passes. The meta
line's parts differ by breakpoint — the size is inline on a phone and a
right-hand column above 640px, "No note" is desktop-only, and the location wraps
to its own line on a phone — so a naive `join("·")` stranded a dot at the start
or end of a line in four different combinations. What works: separators are
nodes with their own variant class, each hidden together with the element it
belongs to, and the parts are ordered so every separator always has something
visible in front of it at its own breakpoint — `[size · filename] / [location]`
on mobile, `[filename or "No note" · location]` with the size in the column on
desktop. Two other fixes on the way: the meta line wraps rather than shrinking
(inline, the filename truncated to "hotel…" *and* the location to "Foss Ho…", so
neither said anything), and the stem/extension pair needed a wrapper of its own,
because as bare flex items they inherited the meta line's gap and an untruncated
name read as "hotel-booking .txt".

**One real bug found by measuring:** the card body came out **42px** tall — the
tap-target guideline missed by 2px, and missed in exactly the way Stage 10
Milestone 7 described, with the height coming from however tall the text
happened to be. `.file-card__body` now carries `min-height: var(--tap-min)`
itself instead of hoping its contents are tall enough.

**Verified.** `make ci` green, `make test-ui` **14/14** (the overflow and
tap-target sweeps cover the new card at both widths, in both themes). By hand
against `make dev` at 324×756 and 1280×800, on all three surfaces — trip Files
tab, location view, location editor: mobile shows `28 B · hotel-booking.txt`
with the location on its own line and no truncation; desktop shows the filename
and location on the meta line with the size right-aligned and no stray leading
dot; the summary reads "2 files · 56 B" and, in German, "2 Dateien · 56 B" and
"1 Datei · 28 B" — both plural forms exercised. Two throwaway uploads covered
what the seed cannot: a `.png` gave the image tile and a long name to truncate,
an `application/octet-stream` gave the generic `file` tile, and both showed the
"No note" hint. Both were deleted afterwards, so the seed is back to its four
files.

**Follow-up (same day, review feedback).** The card's overflow trigger used the
*horizontal* ellipsis, which is the sprite's only one and the icon the tab bar's
"More" already carries. A per-row overflow reads as the vertical form
everywhere else, so `ellipsis-vertical` joined the sprite (again four added
lines, no existing symbol touched) and the card menu uses it; the tab bar keeps
the horizontal one, so the two menus no longer look like the same control.
Verified: `make ci` green, `make test-ui` 14/14, and the rendered `<use href>`
checked on both — `#lucide-ellipsis-vertical` on the cards,
`#lucide-ellipsis` on the tab bar.

---

## 5. The drop zone, and editing a note

- **Upload becomes a drop zone** at the bottom of the list: a labelled region
  with `dragover` / `dragleave` / `drop` handlers plus the existing
  hidden-input-behind-a-label pattern for tap-to-browse (the same shape
  `image-field.js:27-29` uses, so nothing new is invented). Drag is a no-op on
  a phone, which is why the mobile copy says "Drop here or tap to browse" and
  the desktop copy leads with "Drop files here" — one component, two strings,
  chosen by CSS rather than by sniffing.
- **Multiple files.** `multiple` on the input and iterating `dataTransfer.files`,
  uploaded sequentially so one failure names its own file in the existing
  `role="alert"` error paragraph instead of failing the batch anonymously.
- **The note input leaves the form.** Per the mockup's "a note can be added after
  upload", the ⋮ → Edit note path from Milestone 2 replaces it. That needs a
  `promptDialog({ messageKey, value })` in
  [dialog.js](web/js/components/dialog.js) — its private `open()` already builds
  the dialog, buttons and promise, so this is one text input threaded through it,
  not a second dialog implementation. Staged (not-yet-uploaded) files use the
  same dialog and just write to the local `{ file, note }` object.
- 50 MB server limit (`maxFileUploadBytes`) — reject oversized picks
  client-side with a real message rather than letting the 413 surface as a
  generic error.

**Verify.** `make ci`; a Playwright interaction check driving a real upload,
then ⋮ → Edit note → the card title changing from filename to note, then ⋮ →
Delete. That's a **mutating** flow, which `todo.md` flags as needing an
isolation decision — do the cheap version it already suggests: the spec creates
its own trip and deletes it afterwards, leaving the shared `full` fixtures the
other specs depend on untouched.

**Done.** Upload is a drop zone: the whole box is the label for a hidden
`multiple` file input, so tapping it anywhere browses and dropping onto it
uploads, and picking is the trigger — there is no Upload button left, which also
retires the old "Choose a file first." case. Three controls became one. Copy
follows the mockup with two title/hint pairs, one shown at each width (drag does
not exist on a phone, and "tap to browse" is noise beside a Browse button); the
Browse control is a styled `span`, not a button, because the whole zone is
already the label and a real button inside it would be a second control for the
same action. Four i18n keys retired (`chooseFile`, `noFile`, `upload`, `stage`),
six added, both locales, and `scripts/i18n.py unused` still reports clean.

Drag needed two details that are easy to get wrong: `dragover` must
`preventDefault` on *every* event or the browser navigates to the dropped file
instead of handing it over, and `dragleave` fires when the pointer crosses onto
a *child* of the zone — so the highlight flickered off as soon as the pointer
reached the icon, until a `contains(e.relatedTarget)` guard went in.

Multi-file uploads run sequentially, and errors accumulate per file, so an
oversized member of a batch is named and the rest still land. The 50 MB check is
client-side against a `MAX_UPLOAD_BYTES` constant that points at
`maxFileUploadBytes`, because the server's own answer is a 413 whose body talks
about multipart parsing rather than about the file. Errors are held in a closure
variable and printed by the next `render()`, since `render()` replaces the
paragraph a handler would have written into.

**The spec found a real bug.** A freshly uploaded file was appended to the local
list while the API returns newest-first, so it appeared at the *bottom* and then
jumped to the top on the next load. `unshift`, both for uploads and for staged
picks.

**And the first version of the size assertion was vacuous** — worth recording,
because it looked fine. With the client-side guard deleted the server answers
413 and the catch reports `huge-map.pdf: file too large or invalid multipart
form`, which *also* contains the filename, so `toContainText("huge-map.pdf")`
passed either way. It now asserts the exact client-side copy, and with the guard
removed it fails with the two strings side by side.

**Verified.** `make ci` green, `make test-ui` **15/15**, the new one being
`tests/ui/files.spec.js` — the suite's first *writing* spec. It creates its own
trip, does everything there and deletes it in `afterEach` (which runs on failure
too), so the shared seed the other specs depend on is untouched: confirmed by
counting rows before and after, still 7 trips and 4 files. It drives the whole
lifecycle at 324px: empty state → two files in one gesture → the summary's
plural form → newest-first order → the filename as title with "No note" →
the card body being the download link and clearing 44px → the oversized batch →
⋮ → Edit note, with the field focused, the note becoming the title, the filename
moving to the meta line, and the change surviving a reload → ⋮ → Delete, Cancel
first (a destructive action that fires on Cancel being the bug worth catching)
then confirm.

By hand against `make dev`: the desktop zone reads "Drop files here / or browse"
with its Browse button, the mobile one is a centred "Add a file / Drop here or
tap to browse" with no button; a real two-file drag-drop uploaded both; an
oversized file was refused by name while its batch-mate landed. Staging was
driven too, and it is the case a spec cannot reach: two files dropped onto a new
location's card were staged with **no request at all**, rendered as spans rather
than links with a "Remove" action, and then flushed to the server by Create —
both arriving on the new location, which was deleted afterwards along with them.

---

## 6. Sweep-up

- **One `formatBytes`** in [format.js](web/js/format.js), replacing the copy in
  the file-list component (`document-list.js:157` pre-rename) and the second
  copy in
  [location-view-page.js:146](web/js/pages/location-view-page.js#L146).
- **`location-view-page.js` stops hand-rolling the list.** It renders its own
  read-only link+size markup at lines 119-123; point it at the shared card
  (read-only: no ⋮, no drop zone) so there is exactly one file-row template in
  the tree.
- **Delete the dead CSS**: the `.empty` rule that never matches (base.css:1081 —
  the JS uses the `-empty` class, not a descendant `.empty`), plus every ≤640px
  file-row override the new layout makes obsolete.
- **i18n**: new keys in `en.json` *and* `de.json` (`check_i18n.py` gates it),
  and delete `files.notePlaceholder` if the drop zone no longer has a note
  field. Expected new keys: the summary line + its `_plural`, "No note", the
  drop-zone copy, Edit-note dialog copy, and the ⋮ trigger's aria-label.
- **`todo.md`, both directions**: strike the `documents` → `files` half of the
  identifier-sweep entry (built in Milestone 1), leaving the `item` → `location`
  half standing on its own; rewrite the `menu.js` action-item blocker (built in
  Milestone 3); and note whatever this stage defers.

**Verify.** `make ci`, `make test-ui` green, and a final pass at 324×756 and
1280×800 on all three surfaces — trip Files tab, location view, location editor
(both create and edit, so staging is exercised).

**Done.** Most of this milestone had already been absorbed by 4 and 5, so what
was left is small and the account is short.

`formatBytes` now lives in [format.js](web/js/format.js) and is the only copy —
the location view page's went with its markup in Milestone 4, and the file
list's was the last one. The dead `.files .empty` selector is gone (the file
list has never rendered `<li class="empty">`; the link and date lists still do,
so they keep the rule). The i18n sweep happened in Milestone 5 where the keys
actually changed: four retired, six added, both locales, and `i18n.py unused`
clean.

A dead-class audit over `base.css` against the JS and HTML found nothing else
outstanding — every `.file*` rule has a user and every class the component emits
has a rule, with the two apparent exceptions being separator variants built from
a template variable. All the ≤640px overrides for the old row are long gone,
replaced by the card's own.

**The suite caught something better than dead CSS: the dev database had drifted
from the seed.** `menu.spec.js` failed with "expected 2, received 3" on the
seeded trip — a leftover file from Milestone 4's manual probing, which I had
believed I cleaned up. That is exactly the hazard `todo.md` already records
("leftover manual test data happened to be in the database that run"), so the
count assertion now carries a message naming the cause and pointing at
`make dev-reset FORCE=1`, rather than leaving the next person to work out why a
seeded trip has three files.

**Verified.** `make ci` green, `make test-ui` **15/15** after the stray row was
removed, and the rendered sizes and summary re-checked in the browser after the
helper swap (a shared formatter that silently formatted differently would be an
easy thing to miss, since nothing else asserts those strings outside the new
spec).

---

## Build order

1 → 2 → 3 → 4 → 5 → 6. The rename first, alone, so it stays a diff a reviewer
can read as "names only" and so nothing after it is written twice. Then the
endpoint, before the UI that calls it; `menu.js` before the card that consumes
it; sweep-up last, once the shape is settled and it's clear which CSS is
actually dead.

## Workflow

Per `CLAUDE.md`: one milestone at a time — implement, verify with `make ci` plus
a real behavioural pass (assertions over screenshots), add a "**Done.**"
paragraph to `docs/plans/stage-11.md`, update `docs/plans/todo.md` in both
directions, commit (one per milestone; follow-ups get their own "... follow-up:"
commit), leave `make dev` running, then stop and wait.

The approved plan lands as `docs/plans/stage-11.md` **before** any code.

## Verification (stage level)

- `make ci` green before every commit.
- `make test-ui` green from Milestone 1 onward.
- Mobile 324×756 pass via Playwright MCP on Milestones 4, 5 and 6.
- Postgres remains unexercised (no local instance, no CI job) — a standing
  `todo.md` entry, not something this stage fixes; both dialects get
  regenerated and diffed as usual. Milestone 1 makes this sharper than usual:
  the SQLite and Postgres rename migrations are genuinely *different* SQL
  (SQLite can't `ALTER INDEX`), so the Postgres one is verified only by
  reading it.

## Out of scope

- The `item` → `location` half of the identifier sweep — a separate rename with
  its own blast radius.
- Migrating `user-menu.js` and the checklist ⋮ onto `menu.js`, now unblocked.
- Per-visibility (private/shared) files — waits on multi-user roles.
- Squashing the migrations (`todo.md`) — 0006 makes six of them, which is one
  more argument for that entry, not this stage's job.
