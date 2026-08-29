# Caravel — TODO / Backlog

Everything below is **not yet built**. This is the single backlog: raw input for
planning the next stage.

Sections group by *kind of work*, not by which stage deferred something.

**Conventions.** Add new notes straight into the section they belong to. When a
milestone implements an entry, delete it; when a milestone changes what's left
of one, rewrite it. A stale "still outstanding" item that was quietly built is
worse than a missing one, so both directions matter.

Entries tagged **(soon)** are the ones marked as wanted in one of the next few
stages, in the Stage 15 backlog review. Everything else is untagged and
unordered — worth keeping, not worth scheduling. The review also deleted about a
third of the file: entries whose remaining sliver wasn't worth the words, and
decisions that were already made and only being re-litigated by being written
down.

---

## Bugs and rough edges

- **The image fetch by URL does not go through `internal/safefetch`.**
  (Surfaced by the image size-limit fix of 2026-08-28.) `fetchImage` in
  `internal/httpapi/media.go` validates only the scheme and then hands the URL
  to `http.DefaultClient`, so "set this location image from a URL" will happily
  read `http://169.254.169.254/` or an admin panel on localhost and store the
  result. It is the third feature that fetches a caller-supplied URL, and the
  only one still unguarded -- `internal/assist/fetch.go` and
  `internal/geocode/maplink.go` both use `safefetch.PublicOnly()`. Note that
  `internal/httpapi/media_fetch_test.go` serves from `httptest` on loopback, so
  whoever does this needs `safefetch.AllowPrivateForTests()` there.

- **An image is buffered twice on its way in.** (Surfaced by the image
  size-limit fix of 2026-08-28.) `fetchImage` does `io.ReadAll` into a byte
  slice and then `imaging.DecodeAndResize` does `io.ReadAll` on a reader over
  that same slice. With the limit now at 50MB that is 100MB of transient buffer
  per concurrent upload, before the decoded bitmap. `DecodeAndResize` needs the
  bytes twice (once to decode, once to read the EXIF APP1), so the fix is to
  pass the slice rather than a reader, not to stream.

- **Upload limits are still compile-time constants.** (`plans/stage-01.md:152`
  wanted them configurable; the size-limit fix of 2026-08-28 left them so.)
  `maxImageUploadBytes` and `maxFileUploadBytes` are now both 50MB, hardcoded in
  `internal/httpapi/`, and duplicated client-side in
  `web/js/components/file-list.js` and `image-field.js`. An instance that wants
  a different figure has to rebuild. Worth doing together with an
  `/auth/me`-style capability so the client reads the number rather than
  repeating it.

- **Only JPEG orientation is honoured on upload.** (Stage 20 follow-up, the
  EXIF orientation fix.) `internal/imaging` now reads the EXIF Orientation out
  of a JPEG and bakes the rotation into the pixels, which is what phone cameras
  need. Not read: PNG's `eXIf` chunk and extended-WebP's `EXIF` chunk, both of
  which can carry the same tag. Neither is a realistic camera output, so this
  is a completeness gap rather than a live bug. HEIC, which modern iPhones do
  produce, is a separate matter -- `image.Decode` cannot read it at all, so
  such an upload is rejected outright rather than mis-rotated.

- **Images uploaded before the orientation fix stay wrong.** (Stage 20
  follow-up.) Their EXIF was discarded at ingest, so the orientation is not
  recoverable from what is on disk -- there is nothing to migrate, and the
  only fix is re-uploading the picture. Noted so it is not mistaken later for
  the fix having failed.

- **The assistant is slow, and regularly gives up.** (Stage 21; largely
  answered there, and this is what is left.) The complaint was real and the
  cause was mostly the **model**: Stage 21 Milestone 4 measured eight of them
  across two providers at 14.9s to 59.1s on the same run, and switching the
  instance to `nvidia/nemotron-3.5-lightning` took a Tokyo Tower run from 59.1s
  to 16.4s -- more than any code change on the table was worth. Milestones 2
  and 3 also made a slow run **diagnosable** rather than mysterious: the debug
  log and the in-editor trace now carry per-request timings, the tool calls and
  `answered_by`, so "why was that run slow" is answerable from outside a
  debugger for the first time.

  What is genuinely still open: nobody has caught an `assist_timeout` *with the
  trace in hand* and confirmed where it comes from. The gathering deadline does
  not produce it -- hitting that ends the research and the run still answers
  (`internal/assist/agent.go:245-252`) -- so it is the caller's context or a
  provider call's own deadline, and `AnswerTimeout` has already been raised
  once (Stage 16 Milestone 8, 60s to 2m). Next time one happens, read the trace
  before changing anything. The remaining code-level levers are recorded
  separately below, under "Three assistant speed levers" and "Assistant round
  trips" -- both measured, both below this system's run-to-run noise floor.

- **A cover photo set by URL on the *new trip* form is only validated at Create
  time.** (Stage 07; half-fixed in Stage 09 Milestone 4.) The URL is staged
  locally, the trip is created, and only then does the server fetch it — so a
  failure dialog arrives after a create that partly succeeded. A real fix means
  validating at "Set image" time, which needs a trip-independent validation
  endpoint. Softened a lot by Stage 07 Milestone 9's preview-error handler: a URL
  the browser itself can't load is already flagged in the card before Create is
  pressed.
- **A create failure shows the server's raw Go error to the user.** (Noticed
  during Stage 23 Milestone 4's manual pass; pre-existing, and not caused by
  the multipart create.) A cover the server cannot fetch puts `could not fetch
  image from url: Get "http://...": dial tcp ...: connect: connection refused`
  into the Basic info card's error line -- a Go error string, untranslated, in
  an app whose every other string comes from `web/locales/`. It comes from
  `writeError(..., "could not fetch image from url: "+err.Error())`, and
  `handleCreateMediaURL` has said the same thing since Stage 03. It is
  *useful* -- it names the real cause, which a generic message would not -- so
  the fix is probably an error code the client can translate plus the detail
  kept for the console, not simply hiding it. Worth deciding rather than
  drifting, since this is the failure a person is most likely to meet.

- **The zoom hint names Ctrl on every platform.** (Stage 23 Milestone 6.) The
  gate accepts Ctrl *or* Meta, so Cmd + wheel zooms on a Mac, but the string
  `map.ctrlZoomHint` says "Ctrl" everywhere -- Google Maps shows the Mac key
  instead. Fixing it means either platform detection in the component or two
  strings chosen at render time, and neither is worth it until somebody runs
  this on a Mac and says so. Note also that macOS binds Ctrl + wheel to its own
  screen zoom, so a Mac user pressing the key the hint names may get the
  operating system rather than the map -- which is the other half of the reason
  the string should probably follow the platform.

---

## Planned features

- **Expenses: a per-location total, considered and dropped.** (Stage 17; the
  link itself built in Stage 22 Milestone 3.) An expense can now name the
  location it was for, and the row links to it -- which is what the Stage 01
  sketch was after. The *other* half of that sketch, a per-location cost shown
  on the location view, was planned for Stage 22 and cut before it was built:
  the question the link answers is "what was this expense", not "what did this
  place cost", and a second aggregate beside the trip total is a second
  implementation of the same arithmetic. `GET /api/items/{itemId}/expenses` and
  a spending card are what it would take. Not outstanding work -- a decision,
  recorded so it is not re-litigated as an oversight.

- **The move dialog can only offer days the itinerary already has.** (Stage 22
  Milestone 2.) `moveToDay` builds its `<select>` from the days the tab is
  showing -- the trip range plus anything added outside it -- so moving an entry
  to a date that has no day yet means adding the day first, with the control at
  the bottom of the tab, and then moving. The API is not the limitation: it
  takes a date and creates the day itself, which is exactly what makes an
  arbitrary date possible. What it needs is a UI decision -- a date input beside
  the select, or an "another date..." option that swaps the select for one --
  and neither is obviously worth the second control until somebody wants it.

- **Assistant round trips: batching and parallel tool dispatch.** (Stage 21
  Milestone 4b, dropped after 4a was measured.) `agent.go` dispatches a turn's
  tool calls in a plain sequential loop, which reads oddly beside `checkLinks`
  in the same file -- that already fans out with a `WaitGroup`. Two halves:
  prompt the model to request several page reads in one turn rather than one at
  a time, and run a turn's calls concurrently. Results must be appended in call
  order, because a `tool` message has to follow its `tool_calls` and most
  servers reject a mismatch, so they go into a slice indexed by call; and the
  tool-call ceiling has to be decided before the fan-out rather than inside it.

  **Do not take this on as a speed fix.** All tool calls together are ~12% of a
  run at ~1.1s each, and only a turn issuing two or more benefits at all. More
  to the point, 4a measured: with a standard deviation of ~2.9s on an ~8.9s
  mean, detecting a 10% change needs roughly 180 runs per arm, and this targets
  the same order of effect. It is a tidiness item with a possible second of
  dividend. What made speed work worthwhile in the first place was switching
  the model, which took the same run from 59s to 8s.

- **Three assistant speed levers, measured and set aside.** (Stage 21
  Milestone 4.) All three were on the table and none survived the measurements,
  so they are recorded with the reasoning rather than silently dropped. The
  context: 85% of a run is the model, spread over roughly 4.4 sequential
  requests, and switching the instance to `nvidia/nemotron-3.5-lightning` took
  a Tokyo Tower run from 59.1s to 16.4s -- more than any of these would.
    - **A reasoning-effort knob** (`CARAVEL_LLM_REASONING_EFFORT`,
      `CARAVEL_LLM_MAX_TOKENS` on `wireRequest`). No measured benefit on any of
      the eight models tried: completion tokens ran 70-450 a turn, so none of
      them are spending time thinking. Worth building for whoever points
      Caravel at a genuine reasoning model and finds it slow -- but an unknown
      parameter must degrade the way `json_schema` already does
      (`provider.go:200-205`), or a server that 400s on it takes the assistant
      down entirely.
    - **Prompt caching.** Every turn resends the whole conversation, and
      OpenRouter and others can cache the repeated prefix. Never measured, so
      the size of the prize is unknown; it is the one lever that would help
      every request in a run rather than one of them.
    - **Compacting the conversation before composing.** Mechanical truncation
      only -- keep the most recent tool results whole, cut older ones to a lead
      fragment. It must **never** mean asking a model to summarise: that adds a
      round trip to a run that is already 85% model time, making it slower in
      order to make it cheaper. Estimated at roughly 2s (~7%) from the observed
      0.4-0.5s per thousand prompt tokens, in exchange for possibly dropping a
      detail the model had read. Strictly worse than caching, which keeps the
      information; only worth revisiting if caching turns out unavailable.

- **SearXNG as a search backend.** (Stage 16 Milestone 8.) Planned for that
  milestone and dropped: nobody had an instance to test against, and a backend
  verified only against a fake is a backend nobody should trust. Everything
  needed already exists -- the `Searcher` interface in `internal/assist` takes
  a ~60-line implementation, `CARAVEL_SEARCH_URL` already carries a
  self-hosted address (ddgs uses it), and `config.SearchProviders` is one
  string longer. What it needs is somebody with a running SearXNG. Two things
  to know when picking it up: the JSON output format is disabled by default and
  has to be added to `search.formats` in `settings.yml`, and it overlaps
  heavily with ddgs, which shipped -- both are self-hosted keyless metasearch,
  so this is for people who already run one rather than a gap in coverage.
- **Web search may want to leave `internal/assist`.** (Stage 21 Milestone 7.)
  `Searcher` and its four backends live in that package because the assistant
  was their only consumer. It no longer is: the image picker uses the same
  backend, `cmd/caravel` builds it and `internal/httpapi` type-asserts
  `assist.ImageSearcher` off it, so a package named for the assistant is now
  imported for something with no LLM in it. An `internal/websearch` in the shape
  of `internal/geocode` would be the honest arrangement. Mechanical but wide --
  every test in `internal/assist` names one of these types.
- **A SearXNG backend should implement `ImageSearcher` too.** (Stage 21
  Milestone 7.) See the SearXNG entry above: image search is now an optional
  capability a `Searcher` may also implement, and SearXNG has an images
  category, so a backend added later would want both halves rather than only
  the text one.
- **The image picker offers no way to page past the first results.** (Stage 21
  Milestone 7.) Twelve Wikipedia candidates and twelve from the web, and if none
  of them is the picture you want, the only move is a different search term.
  Both backends support paging (`gsroffset`, and ddgs takes a `page`), so a
  "more" control is possible; it was left out because a grid you have to scroll
  twice at 324px is already long, and nobody has yet wanted a thirteenth
  candidate.
- **AI trip-level suggestions.** (Stage 15 backlog review.) "Suggest things to
  do in Reykjavik" returning several candidate locations to add at once, rather
  than enriching one location at a time. **No longer blocked**: Stage 16 built
  the single-location version, so the provider, the search backends, the tools,
  the agent loop, the guard rails, the SSE transport and the stub are all in
  place and this reuses every one of them. What is genuinely new is a
  multi-result review UI, a way to add N locations in one transaction, and
  dedup against what the trip already has.
- **Account settings: what Stage 12 left out.** The screen, appearance and
  language controls and password changing all landed in Stage 12
  (Milestones 2-5). Two things remain:
    - a **profile picture**, which has a schema wrinkle: `media_assets.trip_id`
      is `NOT NULL` and cascades from `trips`, so a user-scoped image has no
      valid home today and deleting a trip would take an avatar with it. Needs a
      migration — nullable `trip_id`, a `user_id` column, or a separate table —
      before the existing upload pipeline can be reused.
    - **per-account preferences.** Appearance and language are both stored per
      browser (`localStorage["caravel.theme"]` / `["caravel.locale"]`), the
      deliberate Stage 12 decision. Making either follow the account means a
      column on `users` and an endpoint to write it — worth answering once for
      both, not twice.
- **Starting a location from a pasted link, promoted.** (Stage 22 Milestone 6,
  second follow-up.) The Maps-link paste lives in the Location card's search
  field, which is where you look for a place. It now fills the title as well as
  the coordinates, which is most of the value a first-class "start this location
  from a link" control at the top of the editor would add -- and that control
  was considered and deliberately not built: it needs its own card and i18n and
  specs, and it competes with the assist panel for the same slot. Worth
  revisiting only if pasting links turns out to be how people actually create
  locations.

- **A resolved Maps link names the place in whatever language the link was made
  in.** (Stage 22 Milestone 6, third follow-up.) The locale is now forwarded on
  all three outbound calls, and Nominatim honours it -- an address comes back as
  Deutschland or Germany as asked. Google does not: the name comes from the
  `/maps/place/<name>/` segment, which is baked into a short link when it is
  created, and neither `Accept-Language` nor `hl=` changes it (measured against
  the live service). So a title suggested from a link may be in someone else's
  language. Nothing to fix from this side; the only lever would be dropping the
  name and asking Nominatim for one, which would be a worse name more often
  than not.

- **Google Maps interoperability: the outbound half.** (Stage 13; the inbound
  half built in Stage 22 Milestone 6.) Pasting a Maps link into the address
  search now resolves it to coordinates. What is left is the other direction:
  the popup's and the location view's "View on Google Maps" links are a
  `?q=lat,lng` **search**, so they land on a dropped pin rather than on the
  hotel's own Google entry with its hours and reviews. Linking the actual place
  needs a place ID, which Caravel cannot get from OSM — so this stays blocked on
  either storing a user-pasted Google URL per location or accepting the search
  link as good enough.

- **A trip journal with photos.** (Stage 01.) A `journal_entries` table
  (trip_id, date, body markdown) reusing the existing `media_assets` pipeline
  for photos.
- **Federation between self-hosted instances.** (Stage 01.) Real sync-protocol
  design still needed; v1 only avoided the integer-PK and local-only-ID mistakes
  that would have made it harder later.

---

## Multi-user and sharing

- **Trip ownership transfer.** (Stage 14 Milestones 3 and 10.) `trips.owner_id`
  is the only record of who owns a trip and the owner has no `trip_members` row,
  which makes "exactly one owner" true by construction — and also means the only
  ways out of owning a trip are deleting it or leaving it in someone else's hands
  informally. The API already points at the gap: removing the owner answers 409
  `owner_cannot_leave`. A transfer is one UPDATE plus a members row for the old
  owner, so the schema is ready; what is undecided is what happens to the
  previous owner (demoted to editor, or removed outright) and to their personal
  files and checklists on a trip that is no longer theirs. The member-removal
  dialog answers that same question today — copy its decision rather than invent
  a second one.
- **Public shareable links.** An unauthenticated read-only trip view via a token.
  Needs a `share_links` table (token, trip_id, scope, expires_at) plus an
  unauthenticated route variant. Cheaper than when this was written: Stage 14
  gave every trip-scoped handler a minimum role through one seam
  (`internal/httpapi/authz.go`), so a link-holder is a synthetic `viewer`
  resolved from a token instead of a session, and read-only mode already exists
  in the frontend. The genuinely new part is the token lifecycle. Note that
  `scope` has to answer the visibility question too: a public link must never
  expose a `personal` file or checklist, whoever created the link.
- **Invite links / joining by token.** Adding a member needs their exact username
  today (Stage 14 Milestone 3), which is fine on a self-hosted instance where you
  know who you are inviting. This only becomes genuinely interesting **after
  federation**, where the person you are inviting is not a user of your instance
  at all — so treat it as a federation follow-on rather than a share-link
  follow-on.

---

## Consistency and cleanup

- **Three near-identical input rules in `base.css`, with drift.** **(soon)**
  (Stage 14 Milestone 3 follow-up.) `.auth-form`, `.trip-form`/`.password-form`
  and `.item-form` each declare their own label-and-input styling, and they do
  not quite agree: `.auth-form` uses `border-radius: var(--radius)` and
  `font-size: 1rem` where the other two use `0.375rem` and `font: inherit`. The
  Members form was joined onto the `.trip-form` group rather than becoming a
  fourth copy, which is right for one form and not a fix for the pattern — the
  next form added has three plausible rules to pick from and no reason to prefer
  one. Consolidating means picking the canonical values, which changes how the
  auth pages look, slightly. Precedent for how: the same file's error-callout
  rule already collapsed five copies into one.
- **Two busy states that are not the shared guard.** (Stage 20.) Everything
  mutating now goes through `web/js/busy.js`, with two deliberate exceptions.
  `assist-panel.js` keeps its own `running` flag, because it also owns stream
  cancellation and a Cancel button, and `file-list.js`'s drop zone keeps its own
  `aria-busy` plus `.file-drop--busy { pointer-events: none }` around a
  sequential batch upload. Both work; neither is the shared idiom, so a reader
  finds three ways of saying "in flight". Folding either in means teaching the
  guard about a set of controls rather than one, and about cancellation.
- **`confirmDialog` hardcodes a trash icon for every danger confirmation.**
  **(soon)** (Stage 14 Milestone 3.) `components/dialog.js` picks the icon from
  `danger` alone (`trash-2` when set, `check` when not), which is right for the
  delete confirmations it was written for and wrong for "Leave trip" — an action
  that removes no data and shows a bin anyway. The *label* half was fixed in that
  milestone (both member confirmations pass their own `confirmKey`), so what is
  left is one optional `iconName` on `confirmDialog`, plus a decision about
  whether `danger` should keep implying an icon at all.
- **Identifier sweep: "item" → "location".** Stage 05 fixed the user-visible
  copy, so what's left is entirely below the surface: the whole
  `item.detail.*`/`item.category.*`/`item.deleteConfirm` i18n namespace is still
  item-flavoured despite `location.form.*`/`location.editor.*` having migrated,
  and on the JS side `location-form.js` exports `renderItemForm`,
  `locations-tab.js` exports `renderItemsTab` and uses `data-action="new-item"`,
  and the list renders `<item-card>`. The API and schema say `items` too
  (`/api/items/{id}`, the `items` table), so this has a choose-your-depth
  question. Precedent for going all the way down: Stage 11 Milestone 1's
  "documents" → "files" rename included a `0006` table rename and dropped the
  `/trips/:id/documents` URL outright rather than redirecting it.
- **Number and date formatting follows the *browser* locale, not the app's.**
  (Stage 17 Milestone 3; narrowed by the Milestone 6 follow-up, which fixed the
  one case that was not merely cosmetic -- `Intl.ListFormat` now takes
  `getLocale()`, because it rendered "Nur für Other User *and* dich", an English
  conjunction inside German copy. What is left is the money and date formatters,
  where the browser locale is a defensible choice rather than a bug.) `format.js` calls `Intl` with an undefined locale
  throughout -- `formatDateRange`, the itinerary day headings, and now
  `formatMoney` -- which is a deliberate, pre-existing decision, documented in
  that file. Money makes the consequence louder than dates did: with the app
  switched to German, a total still renders as EUR 97.55 rather than 97,55 EUR,
  and a day heading still reads Thu 20 Aug. Nothing is *wrong* -- the numbers
  and dates are right and unambiguous -- but the app claims to be in German
  while formatting as though it were not. The fix is to pass `getLocale()` (or
  the resolved locale behind "auto") to every `Intl` constructor, which is a
  handful of call sites in one file. What it needs first is a decision: the
  browser locale is arguably the *better* source for number formats, because it
  is what the rest of that person's computer does, and someone reading a German
  UI may still want their own separators.

- **The social card is committed twice.** (Stage 18 Milestone 1.) The same
  1200x630 PNG lives at `web/brand/og-card.png`, because the app's `og:image`
  has to be served by the app, and at `docs/assets/brand/og-card.png`, because
  the documentation site and the README consume it as an image. ~100 KiB of
  duplicate bytes is not the problem; drift is -- regenerate one from
  `docs/assets/brand/src/og-card.svg` and the other silently stays old. Options:
  serve the docs copy from the site and let the app point at it (no, an instance
  must not depend on an external host), have the build copy one to the other (no
  build step for `web/` today), or accept it and document the pairing where the
  regeneration recipe lives.

- **A rendered note has no shared styling between the two places it appears.**
  (Stage 15 Milestone 3.) The editor's preview and the view page produce
  identical HTML — both from `renderNotesHTML` — but sit in different containers:
  `.notes-field__preview` has a dashed border, padding and a tinted background so
  it reads as a rendering rather than a field, while `.location-view__notes`
  carries no CSS rules at all. So the *content* cannot drift and the
  *presentation* already does. Resolving it means one rule owning "a rendered
  note" with both callers pointing at it — cheap, but it changes how the view
  page looks, which is why it was not done inside a preview milestone.

---

## Testing, CI and dev tooling

- **The UI suite reaches the real Nominatim.** (Stage 21 Milestone 1.)
  `scripts/with_server.sh` sets `CARAVEL_LLM_URL=stub` and
  `CARAVEL_SEARCH_PROVIDER=stub` but leaves `CARAVEL_GEOCODER_URL` at its
  default, which is `nominatim.openstreetmap.org`. So the coordinates
  suggestion in `assist.spec.js` -- and the address search in
  `locations.spec.js` -- depend on a live call to a third party, on a service
  with its own rate limits and its own opinion about automated traffic. The
  assist spec's own header says CI has no network budget, which is true of
  everything except this. Nobody has been bitten yet, and it is a real
  dependency all the same: the fix is a stub geocoder behind the same sentinel
  the LLM and search providers already use, so the suite stops asking a
  volunteer-run service for the same three coordinates on every run.

  Stage 22 Milestone 5 worked *around* this rather than fixing it: the reverse
  geocoding specs intercept Caravel's own `/api/geocode/reverse` and answer it
  from the spec, so the new tests add nothing to the outbound traffic. That is
  the right move for a client-side assertion and no move at all for the two
  older specs, which still call out.

- **The sweeps run in German at one combination only.** (Stage 19 Milestone 4.)
  `routes.spec.js` sweeps overflow and tap targets in German at mobile/light,
  and `a11y-names.spec.js` runs both languages; `headings.spec.js` deliberately
  does not, because heading levels are structural and identical in every
  language. Two limits worth knowing before trusting the German pass further:
  it covers one viewport and one colour scheme, on the argument that neither
  assertion depends on colour and 324px is where width bites; and the name
  sweep only catches controls whose *only* name is the translated string, so an
  empty label on anything with visible text is still invisible to it. A third
  language would multiply this decision rather than inherit it.
- **`with_server.sh` leaks its server when a run dies badly.** (Found while
  verifying Stage 23 Milestone 1.) The script picks a free port in 8090-8120 and
  builds a throwaway binary into a `/tmp/tmp.XXXX` directory, and normally
  cleans both up. It does not always: 31 abandoned servers were found holding
  the *entire* range, the oldest two days old, at which point `make test-ui`
  fails with "no free port in 8090-8120 -- set PORT=... to pick another range".
  The message is accurate and says nothing about the real cause, so the fix
  divides in two: make the cleanup survive whatever is killing it (a trap that
  covers the signals it does not currently catch, or a pidfile the next run
  reaps), and have the no-free-port message say that the range is held by
  processes matching `/tmp/tmp.*/caravel` and how to clear them. Until then, the
  manual sweep is to kill the PIDs whose `/proc/PID/exe` resolves under
  `/tmp/tmp.*/caravel` -- matching on the exe path rather than the process name,
  which the developer's own `make dev` server also has.

- **A directory URL under the asset tree still gets a listing.** (Noted in Stage
  23 Milestone 1.) `serveStatic` sends a *missing* asset path to a real 404, but
  `s.WebFS.Open("/js/")` succeeds for a directory, so the request never takes
  that branch and `http.FileServer` renders its index of every module. Harmless
  on a self-hosted app whose source is public, and it predates the milestone
  that noticed it; the fix is one `Stat` for `IsDir` in the same place.

- **Two concurrent UI runs still share Playwright's `test-results/`.** (Stage 19
  Milestone 1.) Everything else about a run is now private to it -- port,
  database, uploads, saved sessions -- but the output directory is still the
  repository's, and Playwright empties it at startup. So a second run starting
  while the first is going deletes the first run's traces and screenshots. It
  cannot make a passing run fail, because nothing is written there unless a test
  fails, which is why it was left alone: fixing it means either a per-run output
  directory (and then CI's artifact upload has to find it) or accepting that
  failure artifacts from concurrent runs are best-effort.
- **A fast `GREP` can spend the login budget by itself -- and the full suite
  now trips it too.** (Stage 19 Milestone 1; the full-run half observed in
  Stage 23 Milestones 6-7, where `register.spec.js` failed on roughly half of
  full runs and passed every time in isolation. The entry used to say the full
  suite was spread out enough not to hit it; that is no longer true.) Login is limited to 10/min/IP, and the limiter now lives in the run's own
  server rather than being shared with everything else on the machine -- an
  improvement. But `auth.setup.js` spends two, and a `GREP` that selects a
  login-heavy subset (the settings specs, say) finishes in twenty seconds and
  spends the rest, so the run fails on a 429 whose message reads like a broken
  seed -- it names the seed, not the limiter. The full suite is spread out
  enough not to hit it. Options: raise the limit when the assistant stubs are on
  (configuration weakening a security control, rejected once already in Stage
  16), have the specs share one login the way `auth.setup.js` does, or teach
  the message to name the 429 as its own cause.
- **Only two gestures are driven with real touch.** (Stage 19 Milestone 5.)
  `map.gesture.spec.js` covers one-finger scroll and two-finger pan in the
  `chromium-gestures` project; every other touch interaction in the app -- the
  marker drag in the coordinate picker, the itinerary reorder buttons, swiping
  anywhere -- is still only exercised with a mouse, or through Firefox with the
  `(pointer: coarse)` stub. Adding to the project is cheap now that it exists
  (a `*.gesture.spec.js` file is all it takes), so this is a note about reach
  rather than a gap that needs a plan. Mind the trap that produced a false pass
  the first time: CDP silently delivers nothing outside the viewport, so a
  target below the fold must be scrolled into view or the test passes having
  touched nothing.
- **The contrast gate covers three routes and one exemption.** (Stage 19
  Milestone 6.) `make check-contrast` sweeps `/trips`, `/settings` and
  `/trips/new` in both palettes, holding each element to its own WCAG threshold,
  with `.app-brand` exempt as a logotype. What it does not reach: every trip
  tab, the location editor, the map legend and the dialogs -- all of which the
  script *can* measure, since it pierces shadow roots, but none of which is in
  the route list. Widening it is a line in the Makefile plus whatever the
  numbers then say; the reason it starts narrow is that each route costs two
  browser page loads and nobody has read the numbers for the rest yet.
- **The suite waits on injected plumbing, not on the app's own state.** Stage 09
  Milestone 5 gave every route a `common.loading` line, which fixes the
  user-facing half of the old "empty shell" problem but not the suite's: a
  loading line carries no `<h1>`, so `gotoRoute` must wait for fetches to settle
  before asserting heading outlines, and that wait is a `window.fetch` wrapper
  injected by `tests/ui/helpers/scenarios.js`. A `data-loading` attribute on
  `#app`, or a "ready" event, would let the suite wait on a contract the app
  publishes. One component already does exactly that: `leaflet-map.js` sets
  `data-ready` once the map has laid out (Stage 13 Milestone 3), and `gotoRoute`
  waits for it — Leaflet is lazily imported *after* a route's fetches settle, so
  "fetches quiet plus two frames" did not mean the map was up. That is the shape
  the app-wide version wants.
- **`scripts/i18n.py`'s dynamic-prefix rule only fires inside a `t()` call.**
  (Stage 13 Milestone 8.) `DYNAMIC_CALL_RE` matches a template literal as the
  argument of `t(`, so a key composed anywhere else is invisible: Milestone 6's
  `locateErrorKey()` returned `` `map.locate.${reason}` `` from a helper, and all
  five real reason keys were reported as unused. They were not deleted — the
  report's own warning did its job — but the next person might, and the fix that
  milestone applied (spell the keys out in a lookup object, which the bare-string
  pass *does* see) is a workaround at the call site rather than in the tool.
  Teaching the scan to follow a function that returns a composed key is hard;
  recognising a template literal assigned to a `const` whose name ends in `Key`,
  or an explicit allowlist comment, would cover the realistic cases.

---

## Deployment and operations

Nothing here is needed to keep developing; all of it is needed before anyone
else runs this.

- **A service worker update can leave one tab mixing two builds.** (Stage 23
  Milestone 2; the rest of the stale-asset entry was implemented there and is
  gone.) The worker calls `skipWaiting()` and `clients.claim()`, so a new build
  takes over a page that is already running. Navigations and code are
  network-first now, so a reload is always coherent -- but a module imported
  lazily *after* the swap (there is one, the Leaflet vendor copy in
  `leaflet-map.js:438`) is fetched under the new cache while the modules around
  it came from the old one. Low risk, since that import is a vendored library
  that rarely changes, and the alternative -- dropping `skipWaiting()` so the
  new worker waits for every tab to close -- delays the fix this milestone was
  for. The real answer if it ever bites is to notice `controllerchange` and
  offer a reload rather than to stop claiming.

- **Going offline during the first load after a deploy loses the code cache.**
  (Stage 23 Milestone 2.) The new worker's `activate` purges the previous
  cache, and the modules that load *during* that first post-deploy navigation
  went into the outgoing one -- so a client that goes offline in that window has
  only the six precached shell URLs and cannot boot. The next online load
  repopulates all 54 entries. This is ordinary purge-on-activate behaviour and
  it converges after one load; fixing it would mean precaching the module graph
  on install, which is the brittle enumeration `web/sw.js` was written to avoid.

- **The RPM has no repository, and no real-host verification of its unit.**
  (Stage 18, RPM follow-up.) Packages are attached to each GitHub release, so
  installing is `dnf install ./caravel-....rpm` and upgrading means fetching the
  next one -- there is no `dnf update` path without a yum repository (COPR, or
  a repo published to GitHub Pages beside the docs, which is where it would
  naturally live). Separately, the unit's hardening directives
  (`ProtectSystem=strict` and friends) have only been exercised in a
  *privileged* container: rootless refuses at step USER for want of
  CAP_SYS_ADMIN, which is the container's limitation rather than the unit's, but
  a real host is the only place that settles it.

- **Generated release notes will be nearly empty until changes arrive as pull
  requests.** **(soon)** (Stage 18, releases-on-tag follow-up.) GitHub builds the
  notes `.github/workflows/release.yml` asks for from **merged pull requests**
  since the previous tag. This repository has no merge commits at all -- every
  change is pushed straight to main -- so a release will show the compare link
  and not much else. Three ways out, and it is worth picking one before the first
  real release rather than after: start routing changes through labelled PRs (the
  grouping config in `.github/release.yml` is already there for it), generate the
  notes from `git log` between tags instead and pass them as the release body, or
  accept thin notes and treat the compare link as the changelog. The stage plans
  are arguably the real changelog either way.

- **The screenshot set does not show an expense linked to a location.** (Stage
  22 Milestone 7.) `make screenshots` was re-run for this stage and the tour now
  shows the itinerary row's menu and the expense form's Location field, but no
  row in `expenses.png` actually *names* a location, so the link itself is
  invisible. Dressing it in `gen_screenshots.mjs` was considered and dropped: the
  seeded expenses and locations share no titles, so any pairing would come from
  a heuristic, and a heuristic that links "Fuel, Route 1" to a hotel is the
  waterfall-offered-a-hostel failure that file already warns about. The honest
  fix is a seeded expense that names a seeded location, in `cmd/seed` where the
  pairing can be deliberate.

- **The documentation screenshots go stale silently.** (Stage 18 Milestone 11.)
  `make screenshots` regenerates them and `scripts/check_screenshots.py` keeps
  the set and the pages in agreement, but nothing notices when the UI moves and
  the committed captures start showing an older layout. There is no cheap
  automatic answer -- comparing images would fail on every unrelated pixel -- so
  the realistic options are a reminder in the stage workflow (regenerate
  whenever a milestone changes a screen that appears in the tour) or a periodic
  refresh. Worth deciding rather than drifting.

- **The screenshots depend on photographs that are not in the repository.**
  (Stage 18 Milestone 11.) The set is dressed from `images/`, which is
  deliberately untracked -- the author's own photos, used temporarily and not
  published as files. So anybody else regenerating gets the seeder's 343x200
  test-sheet fixtures instead, and the output will not match what is committed.
  The script says so loudly rather than pretending otherwise. If the project ever
  wants reproducible-by-anyone screenshots, it needs a small set of
  known-licence photographs committed for the purpose.

- **The Zensical pin needs periodic review, in two files.** (Stage 18 Milestone
  9.) `zensical==0.0.57` is pinned in `.github/workflows/docs.yml` and in
  `ci.yml`'s `docs` job, deliberately, because a 0.0.x generator can change its
  output between patch releases. That means the site silently stops receiving
  fixes until somebody bumps it -- including the two 0.0.57 bugs `home.html`
  currently works around (the `page.is_homepage` flag being falsy for
  `docs/index.md`, and the skip link pointing at a markdown-derived anchor that
  an emptied content block does not render). When bumping, drop the workarounds
  and re-check the landing page title and skip link.

- **The site ships a font face it never loads.** (Stage 18 Milestone 9.)
  `scripts/gen_brand_fonts.py` writes both weights to `docs/assets/fonts/`, but
  nothing on the site uses Montserrat 500 -- `document.fonts` confirms it stays
  unloaded, so it costs a committed 17 KiB and no request. Harmless, and the
  cost of the alternative is a generator whose two destinations differ. Revisit
  if a docs page ever wants the 500, or trim it if none ever does.

- **Nothing tests the documentation site's rendering.** (Stage 18 Milestone 9.)
  `zensical build --strict` catches dead links and unresolved references, which
  is what CI gates on, but the landing page's layout, contrast and both-palette
  behaviour were verified by hand with the Playwright MCP tools -- there is no
  committed assertion, unlike `tests/ui/brand.spec.js` for the app. A spec
  would need the built site served, which the Playwright config does not do
  today. Worth it if the landing page grows; overkill for one page.

- **Vector tiles, for map labels in the user's own language.** (Surfaced by
  the tile-source change of 2026-08-25.) `CARAVEL_TILE_URL` now lets an
  operator pick a provider whose labels are latin script, or one language
  chosen for the whole instance -- but not one that follows each user's own
  preference, because raster tiles are drawn before anyone asks. The answer is
  vector tiles: MapLibre GL against OpenFreeMap (no key, no request limits,
  unmodified OpenMapTiles schema, which carries both `name:en` and `name:de` --
  exactly the two locales `web/js/i18n.js` supports), with the label
  `text-field` set to coalesce `name:<locale>`, `name:latin`, `name` and
  re-applied on the `locale-changed` event the i18n module already dispatches.
  The cost is what makes it a stage of its own rather than a follow-up:
  vendoring MapLibre GL beside the Leaflet copy, rewriting all 717 lines of
  `web/js/components/leaflet-map.js` (markers as symbol layers or DOM overlays,
  popups, `fitBounds`, pick mode, the locate control), and reworking
  `tests/ui/map.spec.js` and `map.gesture.spec.js`, including Stage 13's
  two-finger gesture handling, which is expressed entirely in Leaflet handler
  terms.

- **The committed screenshots still show the default tile provider.**
  (Surfaced by the tile-source change of 2026-08-25.) `make screenshots` runs
  its own throwaway server, so `docs/assets/screenshots/map.png` shows whatever
  that server is configured with -- OpenStreetMap, and correct for a stock
  install. If the documented default ever changes, the set needs regenerating
  or the map page will illustrate a provider nobody uses.

- **S3-compatible object storage.** Swap the `internal/storagefs` `Blob`
  implementation from local filesystem to S3-compatible (MinIO, Backblaze, and
  so on); the interface already isolates callers from the backend.
- **Prometheus/OpenMetrics metrics.** A `GET /metrics` endpoint via
  `promhttp.Handler()`, outside `/api` and outside the session-auth middleware.
  Note: Stage 01's plan described that routing reservation as already in place,
  but there is no `metrics` reference anywhere in `internal/` or `cmd/` today —
  the route needs adding along with the instrumentation (HTTP request
  count/duration/status, DB query duration, upload counts/sizes, session counts).
- **OpenID Connect / external auth providers.** `auth_identities` already
  supports a `provider` column beyond `'local'` for exactly this; no provider
  integration exists yet.
