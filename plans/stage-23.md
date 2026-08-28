# Stage 23 — Shipping updates, and the map you can actually use

## Context

Four things came out of using the app since Stage 22, recorded in
`plans/notes.md`. Exploring them found that three are smaller than they look
and one is bigger, and that the cause of the loudest one is not what it
appeared to be:

- **A failed cover image creates a second location.** The complaint was
  "creating a location is not atomic". It is worse than that:
  `location-editor-page.js:348-368` never assigns the created item to `item`,
  so after a cover failure the page is still in *create* mode — heading, button
  label, back link, staging mode, all of it — and pressing Save again runs
  `POST /trips/{id}/items` a second time. Cancel silently abandons the first.
  The backlog already carried the non-atomicity ("A new location's cover photo
  and files are still a post-create upload"); it did not know about the
  duplicate.
- **The coordinate picker does not go to the coordinates you type.**
  `leaflet-map.js:622-630` recentres on `initial`, and afterwards only when the
  point leaves the viewport. An editor opened with no coordinates sits at the
  world view, zoom 2 — and a marker dropped anywhere on Earth is inside *those*
  bounds, so typing a latitude and longitude moves a pin you cannot see.
- **The mouse wheel over a map zooms it instead of scrolling the page.**
  `scrollWheelZoom` appears nowhere in the codebase, so Leaflet's default
  (enabled) wins on desktop. The touch half of this was already answered in
  Stage 13 by `dragging: !isCoarsePointer()` (`leaflet-map.js:454`), with a
  static caption below the map.
- **A new version does not reach the browser without a force reload.** The
  guess was HTTP caching, and the proposed answer was a build step. Neither is
  right. `web/sw.js:54-73` is a **cache-first service worker with no
  revalidation**, runtime-populating every JS module into one cache keyed on a
  hand-edited `CACHE_VERSION` — a constant touched four times in the project's
  life while `web/js` changed constantly. Production sends no `Cache-Control`
  and no `ETag`, and `embed.FS` reports a zero modtime so there is no
  `Last-Modified` either: the browser's HTTP cache has nothing to work with and
  the service worker's decision is the only one in play. It serves the old
  files forever, which is exactly the reported symptom.

This stage fixes all four, and takes the three cheapest **(soon)** backlog
entries that the same code is already open for.

### What exploring the code changed about the plan

- **A build step is not needed and should not be added.** `package.json:6`
  says the absence of one is deliberate; there is no import map and 194 bare
  relative imports, so hashed filenames would touch every import site. The
  actual defect is a service worker that never revalidates, and a file server
  with no validator. Both are small, local fixes.
- **The SPA fallback caches a lie.** `router.go:427` rewrites any unknown path
  to `/`, so a stale client asking for a since-deleted `/js/foo.js` gets
  `index.html` with **200** — and the service worker caches that HTML under the
  JS URL permanently. This has to be fixed *with* the cache work or the cache
  work is undermined by it.
- **The build version already reaches the runtime.** `internal/buildinfo`,
  stamped through `Makefile`, `Dockerfile` and `release.yml`, already surfaces
  at `GET /api/health`. It is `dev` for a plain `go build` and `unknown`
  without a `.git`, so anything keyed on it needs a content-hash fallback.
- **A multipart create can generate the item ID before the transaction.**
  `CreateItem` already takes a caller-generated `uuid.NewString()`, and the
  file storage key needs the item ID (`files.go:134`). So the ID is minted
  first, blobs are written, and then one `WithTx` creates item, nested
  location/links/dates, media asset, image attachment and file rows together.
- **The height cap is shared by three different maps.** `#map { height:
  min(50vh, 20rem) }` sits in a blanket `@media (max-width: 640px)` block, but
  the component mounts three ways: the trip Map tab (`trip-detail-page.js:131`,
  plain `:host`), the location view (`location-view-page.js:92`,
  `:host([lat])`, 16rem) and the editor picker (`location-editor-page.js:164`,
  `:host([pick])`, 20rem). Raising the number blindly would put an 85vh map
  inside a form card. The media query has to become mode-specific.
- **The drag-safe strip is no longer load-bearing.** The backlog entry for the
  map height preserves the reasoning from *before* Stage 13's `dragging: false`
  landed. With Leaflet's drag handler off on coarse pointers, a one-finger drag
  over the map is never consumed by the map at any height. The legend above the
  map stays because that is where the filters live, not because the page needs
  somewhere to be dragged from.
- **`--radius` is `0.375rem`.** The backlog says the three input rules disagree
  on border radius and font. They disagree on the *font* only:
  `.auth-form input` sets `font-size: 1rem` (leaving the UA's input font
  family) where the other two set `font: inherit`. The consolidation is
  therefore one line of real visual change on the auth pages, not two.

**Decided with the user up front:**

1. **A true multipart create endpoint**, not the cheaper mode-switch fix — the
   location must not exist at all if any part of it fails.
2. **No bundler.** Fix the service worker and the file server's headers.
3. **The trip Map tab goes to 85vh on mobile**; the location view keeps 16rem
   and the editor picker keeps 20rem. Both of the latter are currently
   *inflated* to 320px by the blanket rule, so this corrects them too.

---

## 0. Land the plan, and fold the notes into the backlog

Commit this file as `plans/stage-23.md`. In the same commit, fold
`plans/notes.md` into `plans/todo.md` — the four entries go into the sections
they belong to, rewritten as backlog entries with the exploration findings
above, and `notes.md` is emptied. Also rewrite the two entries this stage's
exploration found understated: the create-mode entry (it describes a
recoverable retry; it is a duplicate-creating bug) and the map-height entry
(its stated blocker was answered by Stage 13's `dragging: false`).

**Verify:** `make ci`. No behaviour change.

---

## 1. Static assets get a validator, and the fallback stops lying

`internal/httpapi/router.go`, around the `NotFound` handler.

- Walk `s.WebFS` once at startup and build `map[string]string` of path → strong
  ETag (SHA-256 prefix of the content). Cheap: the tree is about a megabyte.
  Skip it entirely when `NoCache` is set — dev keeps `no-cache, no-store,
  must-revalidate` and its real `Last-Modified` from `os.DirFS`.
- Set `ETag` plus `Cache-Control: no-cache` before delegating.
  `http.ServeContent` handles `If-None-Match` and the 304 itself once the
  header is present. `no-cache` means "revalidate", not "do not store", so a
  repeat load is a handful of 304s rather than a refetch.
- **Do not** rewrite a missing path to `/` when it is under `/js/`, `/css/`,
  `/locales/`, `/icons/`, `/fonts/`, `/brand/` or `/vendor/`, or when it ends
  in a known asset extension. Those get a real 404. The SPA fallback stays for
  everything else, which is what deep links need.

**Verify:** a Go test in `internal/httpapi` asserting (a) `GET /js/app.js`
carries an `ETag` and a second request with `If-None-Match` gets 304, (b)
`GET /js/does-not-exist.js` is 404 and not HTML, (c) `GET /trips/abc` is still
200 `index.html`, (d) with `NoCache` set the ETag is absent and the no-store
header is present. Plus `make ci`.

**Done.** `internal/httpapi/staticassets.go` is new and holds all three parts:
`buildAssetETags` walks the tree once at startup and hashes each file to 16 hex
characters of SHA-256, `isAssetRequest` decides whether a miss is a missing
asset or a client-side route, and `serveStatic` is what the `NotFound` handler
now delegates to. `router.go` keeps only the wiring: an `assetETags` field on
`Server`, one call in `NewServer` guarded by `!opts.NoCache`, and a
three-line `NotFound`.

Two decisions worth recording. The tag is **content-derived, not
version-derived** — a version-keyed tag would change on every release whether
or not the file did, throwing away every cached asset each time, which is the
opposite of the point. And the map is built **eagerly**: 71 files and 1.3MB
hash in milliseconds, and doing it at startup keeps the serving path a map
lookup with no locking. An unreadable file is skipped rather than fatal, so it
serves as it did before this existed.

`/` gets `/index.html`'s tag explicitly — the shell is reached under that name
far more often, since it is what every deep link falls back to.

Verified three ways. Six Go tests in `staticassets_test.go` cover the ETag and
the 304, a stale validator still getting 200, the shell's tag matching under
both names, seven missing asset paths 404ing, three deep links still reaching
the shell, and dev mode growing no validator. As a negative control the fix was
disabled (`isAssetRequest` returning false, the ETag branch short-circuited) and
three of the six failed, then passed again on restore — so they are testing the
change and not the scaffolding. Then, against a **production-mode binary**
(embedded FS, no `CARAVEL_WEB_DIR`, port 8123): `/js/app.js` came back
`Etag: "ef62e8e0811d0cdd"` with `Cache-Control: no-cache`, the conditional GET
304ed, a bogus `If-None-Match` got 200, `/js/does-not-exist.js` 404ed as
`text/plain` where it previously answered 200 with `index.html`, and
`/trips/abc` still served the shell. All 70 servable files answered 200 with an
ETag (`/index.html` 301s to `/`, which is `http.FileServer`'s own behaviour and
predates this). Loading the app in a browser against that binary: 53 requests,
no 404s, zero console errors or warnings. `make ci` green and the full UI suite
green at 156 passed.

Two things this did **not** do, deliberately. The UI suite runs with
`CARAVEL_WEB_DIR=web` (`scripts/with_server.sh:69`), so it exercises the dev
path and cannot see the ETags at all — the Go tests and the manual
production-binary pass are the coverage for that half. And a directory URL such
as `/js/` still reaches `http.FileServer`'s listing rather than 404ing, since
`Open` succeeds for a directory; noted in the backlog rather than fixed here,
as it predates this milestone and the source is public anyway.

Unrelated but found while verifying: 31 abandoned `with_server.sh` servers, up
to two days old, were holding the whole 8090-8120 port range and made
`make test-ui` fail with "no free port". Killed by matching
`/tmp/tmp.*/caravel` on their exe paths, and added to the backlog.

---

## 2. The service worker learns the build version

- Serve `/sw.js` through a handler registered ahead of the `NotFound` route,
  reading the file from `s.WebFS` and substituting a placeholder with
  `buildinfo.Version` — falling back to the content hash from Milestone 1 when
  the version is `dev` or `unknown`, so a `go build` with no ldflags still gets
  a distinct value. The bytes then differ on every release, which is precisely
  the trigger the browser's own service-worker update check waits for:
  `install` runs, `activate` purges every cache that is not the new key.
  `CACHE_VERSION` stops being a manual step, and `CLAUDE.md:204` loses its
  warning.
- Change the fetch strategy in `web/sw.js`:
  - navigations (`request.mode === "navigate"`) → **network-first**, cache as
    fallback, so a running instance always gets the current shell;
  - same-origin GET assets → **stale-while-revalidate**: serve the cached copy,
    fetch in the background, update the cache. A missed version bump then costs
    one stale load rather than every load until someone notices.
  - Never cache a non-200, and never cache an `text/html` response under a
    non-navigation request — the second half of Milestone 1's fallback fix,
    belt and braces for any client that still has a poisoned entry.
- `scripts/check_js.sh` already parses `web/*.js` as a classic script, so the
  placeholder must be syntactically valid JavaScript in the file on disk.

**Verify:** a Go test that `GET /sw.js` contains the running version and not
the placeholder. Manual proof of the actual complaint, which is the point of
this milestone: load the app, `make build` with a changed string in a JS
module, restart, reload **without** a force reload, and see the change. Record
the before/after in the Done paragraph.

**Done.** `/sw.js` is now a route (`handleServiceWorker`) rather than a plain
file, substituting `__CARAVEL_BUILD__` in `web/sw.js` on the way out.

**Deviation from the plan, and the reason.** The plan said key the cache on
`buildinfo.Version` with a content hash as fallback. It is the other way round:
the key is `buildinfo.Version + "-" + assetTreeFingerprint(...)`, and the
fingerprint is what makes it correct. The version alone changes on every
commit, including the many that touch only Go, and each change would throw away
every asset every client has cached. The fingerprint - the ETags of every file,
in path order, hashed again - changes exactly when a served file does. The
version stays in the string because a cache called
`caravel-shell-a1b2c3d4e5f6` tells nobody in DevTools which deploy it belongs
to. In dev there is no fingerprint (the ETag map is not built) so the version
stands alone, and nothing is cached there anyway.

**A second deviation: code is network-first, not stale-while-revalidate.** The
plan said SWR for all assets. Writing it exposed the flaw: SWR answers the
first load after a deploy from the old cache and only *then* refreshes, so a
new build takes **two** reloads to appear - a smaller version of the bug this
milestone exists to fix. So navigations and code (`.js`, `.css`, `.json`) go
network-first, and only fonts, icons and images keep SWR, where an instant hit
is worth having and being one version behind for a single load costs nothing.
Network-first is cheap precisely because of Milestone 1: an unchanged module is
a conditional request and a 304, not a refetch.

**A latent break Milestone 1 introduced, fixed here.** `isCacheable` refused any
response whose `Cache-Control` contained `no-cache` - written when only dev sent
such a header. Milestone 1 made *every production asset* send `no-cache`, which
silently switched the service worker's cache off entirely. It now refuses only
`no-store`, which is the honest reading: `no-cache` means "keep it, revalidate
before use", and revalidating is exactly what the new strategy does. Also
rewritten: `isCacheable` takes `(response, allowHTML)` rather than a request,
because a `Request` built in script cannot have mode `"navigate"` - the
constructor refuses it - so deriving the HTML guard from `request.mode` made the
precache fail its own check.

**Verified against the actual complaint, with a negative control.** A git
worktree at `8699f14` (pre-Stage-23) and the working tree were each built with a
marker written into `web/js/app.js`, so the marker proves the *module graph*
came fresh rather than just `index.html`. Both were driven to the same steady
state: service worker controlling, 54 assets in its runtime cache.

- **Old code, port 8131.** Deployed MARKER-TWO; `curl` confirmed the server
  served it. The browser then read MARKER-ONE on reload, and on a second and
  third reload. `fetch("/js/app.js", {cache: "no-store"})` *also* returned
  MARKER-ONE, because the worker intercepts it - which is why a force reload was
  the only workaround. Cache key stayed `caravel-shell-v4` throughout.
- **New code, port 8132.** Same deploy. The served key changed on its own from
  `caravel-shell-dev-5f6bf275e5dc` to `caravel-shell-dev-b8d44aae88a3`, and
  **one plain reload** showed MARKER-TWO with the old cache gone.

Offline was re-checked, since it is the reason a worker exists at all: with the
server stopped, a steady-state client still rendered the full login page and ran
its module graph (the marker was readable), serving 54 entries from cache.

**One transient gap, accepted.** Going offline *during* the first load after a
deploy loses the code cache: those module responses went into the outgoing
cache, which the new worker's `activate` then purges, leaving only the six
precached shell URLs. The next online load repopulates all 54. Standard
purge-on-activate behaviour, converging after one load; noted rather than
worked around.

Also: four Go tests (substitution happens, the key tracks the asset tree and
holds still when nothing changes, dev mode still substitutes and keeps
`no-store`, and `web/sw.js` really does contain the placeholder the handler
looks for - a rename on either side would otherwise silently restore the bug).
`make ci` green, full UI suite green at 156 passed. `CLAUDE.md`'s instruction to
bump `CACHE_VERSION` by hand was removed, since it is no longer true.

---

## 3. Creating a location becomes one request — the server

`POST /api/trips/{tripId}/items` gains a multipart variant, dispatched on
`Content-Type`; the existing JSON path is untouched, because `readJSON`'s
unknown-field strictness, the item tests and the assistant all depend on it.

Parts:

- `item` — the existing `itemRequest` JSON, same `validate()`.
- `image` — an optional file, **or** `image_url` with the optional
  `source_url` / `credit` / `license` provenance fields
  (`media.go:186-198`).
- `file` — repeated, with `file_note` and `file_visibility` in matching order.

Order of operations, which is what makes it atomic:

1. Parse and validate everything, including decoding the image
   (`imaging.DecodeAndResize`) and fetching the URL one (`fetchImage`) — all
   failures answer 400 before anything is written.
2. Mint the item ID and the media asset ID, then `Blob.Put` the image and the
   files under their final keys.
3. One `s.Store.WithTx`: `CreateItem`, `writeItemNested`, `CreateMediaAsset`,
   `SetItemImage`, `CreateFile` per attachment.
4. Return the same 201 detail shape as today.

A blob written in step 2 and orphaned by a step-3 rollback is the one
impurity, and it is the impurity the code already has (`handleUploadMedia`
stores before `CreateMediaAsset`). Say so in a comment rather than pretending
otherwise; nothing user-visible is left behind.

**Verify:** Go tests for the happy path (item, cover, two files, links and
dates in one request), and for each failure — an undecodable image, an
unreachable `image_url`, an oversized file, an invalid `item` part — asserting
in every case that `GET /trips/{id}/items` still returns **nothing**. Plus
`make test-postgres`, since this exercises `WithTx` across four tables.

**Done.** New `internal/httpapi/items_create.go`. `handleCreateItem` now
dispatches on `Content-Type`: a `multipart/form-data` body goes to
`createItemMultipart`, everything else takes the JSON path unchanged.

The ordering is the whole design, and it is worth stating in one line: parse,
validate, decode and fetch **everything** first, so every rejection happens
before a single write; then the blobs; then one `WithTx` for the item, its
nested location/links/dates, the media asset, the image attachment and every
file row. The item ID is minted before the transaction opens, because a file's
storage key contains it (`files.go:134`) and the blobs go down first.

The blob orphaned by a rolled-back transaction is documented in the handler
rather than papered over. It is the impurity the code already had
(`handleUploadMedia` stores before `CreateMediaAsset`), it is invisible - no
location exists and nothing references it - and the alternative, writing blobs
inside the transaction, cannot be rolled back either, because the blob store is
a filesystem.

Two decisions the plan did not settle. `maxItemCreateBytes` is **100MB for the
whole request**, deliberately not the sum of the per-part limits: a 50MB image
plus four 50MB files would be a quarter of a gigabyte in one request, which is
not a location being created. Per-part limits still apply on top
(`maxImageUploadBytes`, `maxFileUploadBytes`), and a location wanting more can
be created first and have files added from its own page. And sending both an
`image` part and an `image_url` is a **400** rather than a silent
preference - guessing which was meant would put the wrong picture on the
location.

Reused rather than reimplemented: `itemRequest.validate`, `writeItemNested`,
`imaging.DecodeAndResize`, `fetchImage`, `sniffContentType`,
`extensionForContentType`, `truncateBytes`/`optional` for provenance, and
`db.FileVisibility.Valid()`'s "unrecognised means trip" default. The provenance
sanitising matches `handleCreateMediaURL` exactly, including dropping a
malformed `source_url` rather than refusing the image over it.

**Verified** by eleven new Go tests, all green on **both dialects** (`make ci`
and `make test-postgres`). The happy path asserts the item, the nested
location, links, dates, the cover (`image_id` and `image_url` both non-null)
and two files with their *positional* notes and visibilities landing on the
right file. Every failure test ends on the same assertion - `GET
/trips/{id}/items` returns zero - for an undecodable image, an unfetchable
`image_url`, five kinds of bad `item` part (no title, bad category, unknown
field, not JSON, a link with no URL), a missing `item` part, both image forms
at once, an oversize file, and a stranger with no editor role.

The strongest of them is `TestCreateItemMultipartRollsBackEveryWriteTogether`,
which extends the existing `failingStore` with a `failCreateFile` flag: it
fires on the *last* write in the transaction, after the item, the nested rows,
the media asset and the image attachment have all been inserted. Nothing
survives - no location, and no orphan file row at the trip level either.
`TestCreateItemJSONPathStillWorks` guards the path that was not supposed to
change, including its unknown-field strictness.

---

## 4. Creating a location becomes one request — the editor

`web/js/pages/location-editor-page.js`.

- `commitSave` in create mode builds a `FormData` instead of a JSON body: the
  `item` part from the existing `readValues()` + links + dates + location, the
  staged `draft.image` (`{kind:"file"}` or `{kind:"url"}` with provenance) and
  `draft.files` as parts. Edit mode is unchanged.
- **`flushUploads` is deleted.** Its whole reason for existing was that a photo
  cannot ride in a JSON body.
- One failure path, one error line, and the page stays in create mode with the
  draft intact — which is now correct, because nothing was created.
- The `saveGuard` stays: it guards concurrency, which is a different problem.

**Verify:** a Playwright spec in `tests/ui/locations.spec.js` that stages a
cover image whose URL the server will reject, presses Create, asserts the error
appears **and** that the trip's location list count is unchanged, then fixes
the image and presses Create once more and asserts exactly one location exists.
That assertion — the count — is the whole point; today it would be two. Mobile
pass at 324×756.

**Done.** `commitSave` now branches: edit sends the same JSON PATCH it always
did, create sends a `FormData` through a new `api.postForm`. `flushUploads` is
deleted - its only reason to exist was that a photo cannot ride in a JSON body.
The failure path is now a single `catch` that shows the error and returns, and
that is *correct* rather than a compromise: nothing was created, so the draft
sitting on the page is the whole truth and Save can simply be pressed again.

`api.postForm` was added rather than branching inside `post()`: the browser has
to set `Content-Type` itself so it can include the multipart boundary, so the
JSON header must not be sent and the body must be handed over unstringified.
Everything after that - the 204, the JSON error body, the `ApiError` - is
shared, instead of the raw `fetch` the editor used to hand-roll twice.

The staged notes and visibilities are appended **positionally**, one
`file_note` and one `file_visibility` per `file`, with an empty string where
there is none - skipping would shift every later file onto the wrong note.

**Verified with a negative control, which is what makes this convincing.** Two
specs in a new `creating a location is atomic` describe block, with its own
trip. The first stages a cover URL the server cannot fetch
(`http://127.0.0.1:1/`, refused at dial, nobody else's host involved), presses
Create, and asserts the trip still lists **zero** locations - then fixes the
picture, presses Create again, and asserts exactly **one**, with its cover. The
second asserts the create is a single POST by recording every `/api/` POST the
page makes and comparing the list to `["/api/trips/{id}/items"]`.

Reverting *only* `location-editor-page.js` to its committed state and re-running
both: the first fails on `a failed create must leave no location behind:
Expected 0, Received 1`, and the second fails with an extra
`/api/trips/{id}/media` in the POST list. So they catch the actual bug, not the
scaffolding. Restored, both green; full suite 158 passed, up from 156.

**Manual pass at 324x756**, and it nearly recorded a false pass worth
remembering: the first attempt showed the right *outcome* for the wrong
*reason*. `make dev` was still running the pre-Milestone-3 binary, so the
multipart body hit the old JSON handler and `readJSON` rejected it - a 400 and
no location, which looks identical to success. `scripts/dev_server.sh restart
"MARKER=send either an image file"` exists for exactly this and confirmed the
new binary before the pass was redone. Then: the error line read `could not
fetch image from url: ... connection refused` (the real one, from the multipart
handler), the trip still had 2 locations, the button still said "Create
location", there was no delete card, the title and the staged image were both
still on the page, and nothing overflowed 324px. Replacing the cover with a
real PNG and pressing Create once landed on the view page with **exactly one**
copy of the location and its cover attached.

---

## 5. The picker goes to the coordinates you type

`leaflet-map.js:622-630`. Recentre at `SINGLE_MARKER_ZOOM` not only when
`initial`, but whenever the marker is being **created** on a map that is still
at the world view — that is, the user has not zoomed or panned it themselves
yet. Track that with a flag set in the `map.on("zoomend"/"dragend")` handlers
rather than by inspecting the zoom level, so a deliberate zoom-out to 2 is not
mistaken for "never touched".

Typing a latitude before a longitude briefly makes a valid pair out of a
half-typed number, so the recentre must not fire per keystroke on an existing
marker — the existing "only when out of bounds" branch keeps handling that
case unchanged.

**Verify:** extend `tests/ui/map.spec.js` — open the editor for a location with
no coordinates, read `host._map.getZoom()` (2) and `getCenter()`, type a
latitude and longitude, assert the zoom is now `SINGLE_MARKER_ZOOM` and the
centre is within a small epsilon of the typed point. Then pan away, type a
nearby point, and assert the map did **not** jump.

**Done.** `syncPickMarker`'s rule is now `initial || (creating &&
!this._userMovedMap)`, where `creating` is "the pick marker did not exist a
moment ago". Once the marker exists the old out-of-sight branch takes over
unchanged, so typing still does not yank the map on every keystroke.

**Deviation from the plan, and it matters.** The plan said to feed the
"untouched" flag from the map's own `zoomend`/`dragend`. That cannot work here:
`setView` fires those too, so the flag would be set by *our own* recentring -
including the `setView([20, 0], 2)` this component performs when it opens with
no coordinates, which would disable the feature outright on the very screen it
is for. The flag is fed instead by four real input events on the map container
- `mousedown`, `wheel`, `touchstart`, `keydown` - none of which `setView` can
raise. Between them they cover dragging, the zoom buttons, double-click zoom,
wheel zoom, pinch, and the arrow and +/- keys.

**A case the plan did not consider, and which this handles for free.** Placing
a point by *clicking the map* also fills the coordinate fields, and zooming to
14 underneath that would throw away the view the person deliberately chose.
Because a click requires a `mousedown` on the map, the flag is already set by
the time the marker is created, so a clicked point keeps its zoom. The same is
true of a marker drag. Only coordinates that arrive from somewhere other than
the map - typing, an address-search result, a pasted Maps link, the assistant -
zoom in.

**Verified with a negative control.** Three new specs in `map.spec.js`'s
existing picker describe: typing zooms (world view, zoom 2, to zoom > 5 centred
on the point); a map the person has wheel-zoomed is left where they put it; and
a point placed by clicking keeps the click's zoom. Reverting the rule to the old
`if (initial)` and re-running reproduces the complaint exactly - `the map should
zoom to the typed point, not stay at world view: Expected > 5, Received 2` -
while the two guard tests still pass, which is right: they assert the map is
*not* moved, and the old rule never moved it either. They are guards against the
new behaviour over-firing, not against the bug.

Restored, all green: full UI suite 161 passed (up from 158), `make ci` green.
Mobile pass at 324x756 against `make dev`: zoom 2 and centre [20, 0] before,
zoom 14 and centre [64.1466, -21.9426] after typing, one marker, and the page
does not overflow.

**A known transient, not worth more machinery.** The fields update on `input`,
so typing a latitude and longitude by hand can briefly form a valid pair out of
half-typed numbers - "6" and "-2" before "63.8" and "-20.3" - and the marker is
created at that intermediate point, zooming there. Every keystroke after that
goes through the out-of-sight branch, which follows the marker, so it lands
correctly; the cost is a brief pan. Debouncing would fix the flicker and delay
the thing this milestone is for, and the paths that matter most (search, paste,
click) set both coordinates at once and never see it.

---

## 6. Zooming needs Ctrl, and the hint becomes an overlay

`leaflet-map.js`.

- Construct with `scrollWheelZoom: false`, and add a `wheel` listener that
  enables the zoom for the duration of a Ctrl-held (or Meta-held, for macOS)
  wheel gesture. A plain wheel then scrolls the page, as it does everywhere
  else on it.
- A semi-transparent overlay inside `.map-wrap`, hidden by default, shown for
  ~1.5s when a plain wheel arrives over the map, saying "Use Ctrl + scroll to
  zoom the map". Same overlay, different string, when a one-finger drag arrives
  on a coarse pointer: "Use two fingers to move the map" — the existing
  `map.twoFingerHint`, which stops being a permanent caption below the map and
  becomes something shown when it is relevant. Keep it `aria-live="polite"` and
  `pointer-events: none` so it never eats the gesture it is describing.
- New i18n key for the Ctrl string in `en.json` and `de.json`; the caption's
  CSS rule (`leaflet-map.js:237-241`) is replaced by the overlay's.

`tests/ui/map.spec.js:87` reads `.gesture-hint`'s text — it needs updating to
the overlay's new lifecycle rather than deleting.

**Verify:** Playwright — dispatch a plain `wheel` over the map, assert
`getZoom()` is unchanged and the overlay is visible; dispatch one with
`ctrlKey`, assert the zoom changed and no overlay. On the mobile project,
assert the overlay is absent on load (today's caption is always present) and
appears after a one-finger touch drag in the `chromium-gestures` project.
`tests/ui/map.gesture.spec.js` must stay green unchanged — the one-finger page
scroll and the two-finger pan are not being altered.

---

## 7. The mobile map grows

`leaflet-map.js`'s `@media (max-width: 640px)` block becomes mode-specific:

- plain `:host` (the trip Map tab) → `#map { height: 85vh }`;
- `:host([lat])` → 16rem, and `:host([pick])` → 20rem, i.e. what their desktop
  rules already say, instead of both being inflated to 320px by the blanket
  rule.

The legend keeps `order: -1`. `min-height: 16rem` goes, since each mode now
states its own height.

**Verify:** `tests/ui/map.gesture.spec.js` green — the one-finger scroll
assertion is the regression guard and it matters more at this height, so make
sure the target is scrolled into view first (CDP silently delivers nothing
outside the viewport). Add a measurement assertion for each of the three
modes' rendered heights at 324×756. Manual pass on the phone viewport for all
three screens.

---

## 8. Sweep-up: three input rules, and a bin on the wrong button

Both **(soon)** backlog entries, both small, both in code this stage has
already opened.

- `web/css/base.css`: collapse `.auth-form`, `.trip-form`/`.password-form` and
  `.item-form`'s label-and-input declarations into one shared rule. The label
  blocks are byte-identical; the input blocks differ only in `font-size: 1rem`
  versus `font: inherit`. Take `font: inherit` — the auth pages' inputs change
  from the UA font family to the app's, which is the visible half of this and
  the reason it was not done inside another milestone. Precedent for the
  mechanics: the same file's error-callout rule already collapsed five copies.
- `web/js/components/dialog.js:42`: `confirmDialog` gains an optional
  `iconName`, so "Leave trip" — which deletes nothing — stops showing a bin.
  `danger` keeps implying `trash-2` when nothing is passed, so every existing
  caller is unchanged; only the leave-trip call site passes one.

**Verify:** `make ci`, `make check-contrast` (the auth route is in its list and
this changes an auth-page font), and a Playwright assertion on the leave-trip
dialog's icon. Screenshot the auth page before and after by hand to confirm the
font change is the only difference.

---

## Build order

0 → 1 → 2 → 3 → 4 → 5 → 6 → 7 → 8.

1 must precede 2: the service worker's new strategy depends on the fallback no
longer answering 200 for a missing asset, and on responses carrying a
validator. 3 must precede 4 (endpoint before caller). 6 must precede 7 —
raising the map to 85vh with the wheel still hijacked would make the desktop
regression louder, and the overlay is what explains the gesture at the new
size. 5, and the pair 3–4, are independent of everything else.

## Files this touches

- `internal/httpapi/`: `router.go` (ETag map, asset 404s, the `/sw.js` route),
  `items.go` (the multipart create), `media.go` and `files.go` (helpers reused
  by it), and their tests.
- `web/sw.js`, `web/js/app.js` if registration needs it.
- `web/js/pages/location-editor-page.js`, `web/js/components/leaflet-map.js`,
  `web/js/components/dialog.js`, `web/css/base.css`,
  `web/locales/{en,de}.json`.
- `tests/ui/`: `locations.spec.js`, `map.spec.js`, `map.gesture.spec.js`,
  `sharing.spec.js` (the leave-trip dialog).
- `plans/stage-23.md`, `plans/todo.md`, `plans/notes.md` (emptied),
  `CLAUDE.md` (the `CACHE_VERSION` warning, which stops being true at
  Milestone 2).

## Out of scope, deliberately

A bundler and hashed filenames (judged on transfer size later, not on caching);
vector tiles; the trip journal; trip-level AI suggestions; share links; the
`item` → `location` identifier sweep.

## Verification

Every milestone: `make ci` green, plus its own named proof above — an assertion,
not a screenshot. `make test-postgres` for Milestone 3. A mobile pass at
324×756 against `make dev` for Milestones 4, 6 and 7. Milestone 2's proof is
the manual upgrade-without-force-reload, because that is the complaint and no
unit test covers it.

## Workflow

One milestone at a time, in the order above. For each: implement, verify
(`make ci` plus the milestone's own proof), add a **Done.** paragraph to that
milestone's section in `plans/stage-23.md` describing what actually landed and
how it was verified, reconcile `plans/todo.md` in both directions, commit (one
commit per milestone; a follow-up fix gets its own "... follow-up: ..."
commit), make sure `make dev` is running, then stop and hand back control. Do
not start the next milestone until told to continue; feedback at a checkpoint
is fixed and re-verified before moving on.
