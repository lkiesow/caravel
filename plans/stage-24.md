# Stage 24 — Who the client is, and what a create leaves behind

## Context

The 2026-08-29 backlog review cut `plans/todo.md` roughly in half and tagged
thirteen entries **(soon)**. This stage takes the server-correctness cluster and
the frontend-polish cluster. The feature work (trip-level AI suggestions, the
Google Maps place-ID investigation, assistant tool batching) and vector tiles
are deliberately left for later stages.

The review also surfaced the largest item here, which was not in the backlog at
all: **Caravel never reads `X-Forwarded-For`.** `clientIP`
(`internal/httpapi/auth.go:248`) parses the host out of `r.RemoteAddr` and
nothing else, and all four rate limiters key on it (`security.go:98-138`).
Behind a reverse proxy — the shape `docs/running/reverse-proxy.md` documents and
recommends — every request carries the proxy's address, so "10 logins/minute per
client" becomes 10/minute for the whole instance and one person mistyping a
password twice locks everybody out. That page already describes the consequence
honestly as a limitation; nothing tracked fixing it.

Alongside it, two entries turn out to be the same shape as each other: a create
that can half-succeed. Stage 23 Milestones 3-4 made *location* creation one
atomic multipart request. Trip creation was left as it was.

### What exploring the code changed about the plan

- **The `with_server.sh` leak has a different cause than the backlog guessed.**
  The entry proposed "a trap that covers the signals it does not currently
  catch". That would fix nothing: the script ends with `exec "$@"`
  (`with_server.sh:172`), which *replaces* the shell, so once Playwright starts
  the `trap cleanup EXIT` no longer exists at all. The fix is structural.
- **Widening the contrast gate is not "a line in the Makefile".**
  `contrast.js:266-280` navigates literal `--route` strings and asserts the
  landed path matches the requested one. Every route the entry names — trip
  tabs, the location editor — contains a trip id the script cannot resolve, and
  there is no interaction machinery for dialogs at all.
- **The session IP is write-only, so that half of the proxy fix is free.** I
  claimed in the backlog entry that the sessions list in settings shows the
  proxy address. That is wrong and Milestone 0 corrects it: there is no sessions
  list, no `/api/sessions` route, and no List-by-user store method. `Session.IP`
  is written at login and never read back. Fixing `clientIP` fixes what gets
  stored; nothing else is needed.
- **Trip create silently drops image provenance today.**
  `trip-editor-page.js:66` posts only `{ url }` to `/media/url`, while
  `image-field.js` stages `provenance` and the location editor forwards
  `source_url`/`credit`/`license`. Folding the cover into the create fixes this
  as a side effect.
- **`DecodeAndResize` is already byte-oriented inside.** `imaging.go:47` opens
  with `io.ReadAll` and everything below it takes `[]byte`. The double buffer
  needs a `[]byte` entry point, not a restructure.
- **There is no handler-level test for `POST /api/trips`.** `trips_test.go` only
  table-tests `tripRequest.validate`. The multipart milestone brings the first
  ones.
- **No config option today parses a list.** `CARAVEL_TRUSTED_PROXIES` has to
  establish that pattern; everything else copies `CARAVEL_ASSIST_RATE_LIMIT`
  end to end (`config.go:112` → `:193` → `Options` → `NewServer` → docs →
  `.env.sample` → `config_test.go`'s bad-value table).

---

## 0. Land the plan, and reconcile the backlog

Commit this file as `plans/stage-24.md`. In the same commit, fix four entries in
`plans/todo.md`:

- **Correct the trusted-proxy entry**, in two places. Delete the claim about the
  sessions list in settings — there is no such screen; the stored `Session.IP`
  is write-only, so fixing `clientIP` is the whole job. And replace "empty by
  default … the default must stay trust nothing" with the private-range default
  decided here, since the entry currently argues for the opposite and would
  mislead whoever reads it next if the stage stalls.
- **Rewrite the `with_server.sh` entry** around the `exec` finding.
- **Rewrite the contrast-gate entry** to say it needs trip-id resolution, not a
  Makefile line.
- **Add a new entry:** `CARAVEL_ASSIST_RATE_LIMIT` and
  `CARAVEL_ASSIST_MAX_CONCURRENT` are parsed, validated, documented
  (`docs/configuration/assistant.md:78`), sampled (`.env.sample:150`) and logged
  at startup (`main.go:138-152`) — but `main.go:155-171` never puts them in
  `httpapi.Options`, so `NewServer` always takes `DefaultAssistRateLimit` (6)
  and `DefaultAssistMaxConcurrent` (4). The startup log reports a number the
  server is not using. Two lines to fix, plus a test that a configured limit
  actually bites; deliberately not in this stage.

**Verify:** `make ci`. No behaviour change.

---

## 1. Trusted proxies, and a client IP that means something

`internal/config/config.go`, `internal/httpapi/{auth,security,router}.go`,
`cmd/caravel/main.go`, docs.

- **`CARAVEL_TRUSTED_PROXIES`** — comma-separated CIDRs and/or bare addresses,
  parsed into `[]netip.Prefix` on `Config`; a bare address becomes a /32 or
  /128. This is the first list-valued option, so it also establishes the idiom:
  a `getEnvPrefixList(key)` helper beside `getEnvInt` (`config.go:305`),
  appending to the same `errs` accumulator so a bad value names its variable the
  way `config_test.go:216`'s table expects.
- **The default is the private ranges**, following Tomcat's `RemoteIpValve`
  `internalProxies` and Rails' `config.action_dispatch.trusted_proxies`:
  `127.0.0.0/8`, `::1/128`, `10.0.0.0/8`, `172.16.0.0/12`, `192.168.0.0/16`,
  `169.254.0.0/16`, `fe80::/10`, `fc00::/7`. Setting the variable **replaces**
  the set rather than adding to it, and the literal `none` empties it — there
  has to be a way to say "trust nothing", and an empty string now means "use the
  default".
- **Why this is safe, and where it is not.** The header is honoured only when
  the *direct peer* is already inside one of those ranges, so an instance
  exposed straight to the internet ignores it: a public attacker's own peer
  address is untrusted, and no header they send is read. The cost is real but
  local — somebody who can already connect from a private address (the same LAN,
  the same host, a sibling container) can forge `X-Forwarded-For` and choose the
  address the limiters key on. That is the same trade-off Tomcat and Rails make,
  and it is the right one for a self-hosted app whose proxy is nearly always on
  the same host or the same network. An operator who wants it closed sets the
  variable to just their proxy, or to `none`.
- **`100.64.0.0/10` (CGNAT) is deliberately excluded**, though Rails includes
  it: it is Tailscale's range, and on a Tailscale-reachable instance those
  addresses are usually *end users* rather than proxies. Trusting them by
  default would hand every tailnet peer the forgery above. Say so in a comment.
- **`clientIP` becomes a method on `*Server`** taking the configured prefixes.
  All five call sites (`security.go:98,111,123,138` and `auth.go:157`) are
  already methods on `*Server`, so this is mechanical. The resolver:
  1. `net.SplitHostPort(r.RemoteAddr)` — replacing the naive
     `strings.LastIndex(":")`, which truncates a bare IPv6 address.
  2. If the direct peer is **not** in `TrustedProxies`, return the peer and
     **ignore the headers entirely**.
  3. Otherwise walk `X-Forwarded-For` right to left and return the rightmost
     entry that is not itself a trusted proxy; fall back to the peer if the
     header is absent, empty or unparseable.
  It returns **two** values — the resolved address and whether forwarding was
  actually applied. Milestone 2 needs the second one, and a caller that ignores
  it behaves exactly as before.
- **There is no "trust everything" setting**, and none should be added. A
  wildcard would let any caller on the public internet forge a client address
  and defeat every limiter — worse than the bug being fixed. The peer check is
  what makes the private-range default defensible, so it must not be bypassable.
- `X-Real-IP` is deliberately **not** read: it carries no chain, so there is no
  way to tell how many hops it crossed. Note this in the docs so it is a
  decision rather than an omission.
- `isRequestSecure` (`security.go:17`) stays exactly as it is. Its comment
  explains why trusting `X-Forwarded-Proto` unconditionally is safe — it can
  only ever *add* `Secure` to a cookie. That reasoning does not carry over to
  this header, and the two should **not** be made consistent with each other.
  Say so in a comment, or someone will "tidy" it later.
- **Docs:** rewrite `docs/running/reverse-proxy.md`'s "Rate limits stop being
  per-client" section (lines ~52-72) from a description of the bug into a short
  statement that the common shapes now work unconfigured, plus the two cases
  that need the variable: a proxy outside the private ranges, and an instance
  where private-range clients should not be trusted (`none`). Add the variable
  to `docs/configuration/server.md`'s table with its default set spelled out,
  and to `.env.sample`. That doc's limits table is also missing the image-search
  limiter — add the row while there.

**Verify:** a table test for the resolver — a public peer plus a header present
(header ignored, peer returned, which is the case that keeps the default safe),
a trusted peer with a chain (real client returned), a chain of several trusted
proxies (leftmost untrusted returned), `none` configured with a loopback peer
sending a header (header ignored), IPv6 with and without a port, malformed and
empty entries. Plus a config test that the default set is what is documented and
that setting the variable replaces rather than extends it. Plus a handler test
that two different `X-Forwarded-For` clients behind a trusted proxy get
**separate** login budgets, which is the actual bug. This needs a way to set
`RemoteAddr` and headers on a test request — `testing_test.go`'s `do` (`:126`)
has none, so add one helper beside it. Then `make ci`.

---

## 2. Loopback is exempt from the login limiter

`internal/httpapi/security.go`.

Skip `s.LoginLimiter` when the request is a **genuinely direct loopback
connection**: the peer address from `RemoteAddr` is loopback *and* Milestone 1's
resolver reports that no forwarding was applied. That exempts the UI suite and
the developer's own browser against `make dev`, and nothing else.

- **Key on the peer, not on the resolved address.** This matters precisely
  because loopback is now trusted by default: were the exemption keyed on the
  resolved IP, anyone able to reach the server locally could send
  `X-Forwarded-For: 127.0.0.1` and switch the login limiter off for themselves.
  Keying on "peer is loopback and the header was not used" closes that.
- **Login limiter only.** Geocode, assist and image-search keep their budgets;
  the suite does not exhaust those and they protect somebody else's service.
- A reverse proxy on the same host is *not* affected, because the private-range
  default already trusts loopback: its forwarded client resolves to the real
  address, forwarding was applied, and the exemption does not fire. This is the
  hazard the earlier draft of this plan had to warn about; the default chosen in
  Milestone 1 removes it.

**Verify:** three tests — fifteen logins from a bare `127.0.0.1` peer all get
past the limiter; a proxied client (loopback peer + `X-Forwarded-For`) is still
limited at ten, proving the exemption does not leak through a proxy; and a
loopback peer sending a forged `X-Forwarded-For: 127.0.0.1` **is** limited,
proving the bypass above is closed. Then the
real proof: run the **full** UI suite three times and see `register.spec.js`
pass each time. It failed on roughly half of full runs before.

---

## 3. `with_server.sh` stops leaking its server

`scripts/with_server.sh`.

- **Drop the `exec`** at line 172. Run `"$@"` as a child, capture its status,
  let `cleanup` run, exit with the captured status. This is the actual fix: the
  trap currently ceases to exist at the moment the tests start.
- **Trap `INT TERM HUP QUIT` as well as `EXIT`**, with `cleanup` idempotent so
  the EXIT trap firing after a signal trap is harmless.
- **Make the no-free-port message name the real cause** (`with_server.sh:149`).
  It currently says only "no free port in 8090-8120 — set PORT=… to pick another
  range". It should add that the range may be held by abandoned servers from
  earlier runs, and give the exact sweep: the PIDs whose `/proc/PID/exe`
  resolves under `/tmp/tmp.*/caravel` — matching on the exe path, not the
  process name, because the developer's own `make dev` server shares the name.

Nothing defends against `kill -9` of the wrapper; that is what the improved
message is for.

**Verify:** start `scripts/with_server.sh sleep 60` in the background, `kill`
the wrapper (TERM), and confirm with `ss -ltn` and a `/proc/*/exe` sweep that no
server and no `/tmp/tmp.*` directory survive — the case that leaks today. Then
`PORT=8090 PORT_LAST`-equivalent exhaustion (or a temporary narrowed range) to
read the new message. Then `make test-ui` green.

---

## 4. Sweep-up on the server: a directory is a 404, and an image is buffered once

Two unrelated small fixes, one commit, in the manner of Stage 23 Milestone 8.

**The directory listing** — `internal/httpapi/staticassets.go:118-126`. The
branch decides on `Open` alone, and a directory opens successfully, so `/js/`
reaches `http.FileServer` and gets an index of every module. Add a `Stat` in the
`else` arm: if it is a directory, treat it as if it had not opened —
`isAssetRequest(path)` → real 404, otherwise the SPA fallback. **Mind the root:**
`/` is itself a directory and must keep working; it is not an asset request, so
it takes the fallback to `/` and `http.FileServer` serves `index.html` as it
does today. Keep `TestStaticShellHasETagAtRoot` passing.

**The double buffer** — `internal/imaging/imaging.go:47` and
`internal/httpapi/media.go:314`. Add `DecodeAndResizeBytes(data []byte)` holding
the current body, and reduce `DecodeAndResize(r io.Reader)` to `io.ReadAll` plus
a call to it. `fetchImage` then passes its slice directly instead of
`bytes.NewReader(data)`. The other three callers (`media.go:133`,
`items_create.go:161`, `cmd/seed/main.go:421`) and all five imaging tests keep
working untouched.

**Verify:** a Go test asserting `GET /js/` is 404 and not an HTML listing,
sitting beside `TestStaticMissingAssetIsNotFound` (`:98`) — note
`staticassets_test.go`'s `staticFS()` has no directory-shaped case, so it needs
one. Existing static tests still green (root, SPA fallback, ETag/304, dev
no-store). For the imaging half, the existing `media_fetch_test.go` and
`imaging_test.go` suites cover behaviour; add nothing beyond keeping them green.
Then `make ci`.

---

## 5. Creating a trip becomes one request — the server

New `internal/httpapi/trips_create.go`, modelled directly on
`items_create.go`.

- `handleCreateTrip` (`trips.go:199`) branches on
  `strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data")`
  exactly as `handleCreateItem` does (`items.go:277`). The JSON path stays.
- `createTripMultipart`: `MaxBytesReader` on a new `maxTripCreateBytes`
  (an image and no files, so 60MB rather than the location's 100MB) →
  `ParseMultipartForm` → `trip` JSON part decoded with `DisallowUnknownFields`
  → `validate()` → mint `tripID := uuid.NewString()` → **reuse `stageImage`**
  (`items_create.go:148`, already handles the file/`image_url` either-or, the
  size cap, provenance and `fetchImage`) → one `WithTx` doing `CreateTrip`,
  `CreateMediaAsset` and the preview-image assignment together.
- Because everything is fetched before the transaction, an unfetchable URL fails
  with **no trip created** — which is the entire point.
- Forward `source_url`, `credit` and `license`, which the current flow drops.
- Reuse `storeImage` (`items_create.go:198`) for the blob write. The accepted
  impurity is the same one the location path already documents: a blob written
  before a rolled-back transaction is orphaned and unreferenced.

**Verify:** the first handler-level tests for `POST /api/trips`, mirroring
`items_create_test.go`'s shape and ending on a trip-count assertion: commits
trip + asset + preview together; rolls back on an unfetchable `image_url` (count
unchanged); rejects both `image` and `image_url`; rejects a bad `trip` part; the
JSON path still works. Plus `make test-postgres`, since this adds a transaction
over `trips` and `media_assets`.

---

## 6. Creating a trip becomes one request — the form

`web/js/pages/trip-editor-page.js`, `web/js/components/trip-form.js`.

- Replace the `onSaved` sequence (`trip-editor-page.js:54-86`) — create, then
  upload or post the URL, then `PUT /preview-image`, then navigate with an
  `imageFailed` branch — with a single `api.postForm("/trips", form)`, building
  the body the way `buildCreateForm` does in the location editor
  (`location-editor-page.js:386-408`): a `trip` JSON part, then `image` or
  `image_url` plus its provenance fields.
- `trip-form.js:115` currently owns the `api.post("/trips", body)` call, with
  `onSaved` awaited outside the try so the busy guard covers the upload. With
  one request that split is no longer needed; keep the guard covering the whole
  thing.
- On failure nothing was created, so the page stays in create mode and shows the
  error inline — the same shape Stage 23 gave the location editor. Delete the
  `alertDialog({ messageKey: "image.fetchFailed" })` + navigate-to-settings
  path. **Check whether `image.fetchFailed` is still referenced elsewhere**
  before removing the key; if not, remove it from both locale files
  (`scripts/check_i18n.py` enforces parity).

**Verify:** `tests/ui/trip-editor.spec.js` — the existing create-with-cover test
(line 42) must still pass unchanged, which proves the happy path. Add one
asserting that creating with an unreachable cover URL leaves the user on
`/trips/new` with an inline error and creates **no** trip (`GET /api/trips` count
before and after). Plus `make ci`.

---

## 7. Text inputs stop zooming iOS Safari

`web/css/base.css`.

The shared input rule (`base.css:514-531`) says `font: inherit`, and the labels
those inputs sit inside are `0.875rem` (`base.css:500-511`) — so every form
control in the app renders at 14px, and mobile Safari zooms the page on focus
below 16px. Stage 23 Milestone 8 removed the one exception when it consolidated
three rules into one; the comment at `:485-499` records that `.auth-form` had
said `font-size: 1rem`.

Add `font-size: 1rem` to the shared rule. Then walk the other input rules that
say `font: inherit` and sit under one of those labels — `.location-form`,
`.link-form`, `.date-form`, `.dialog__input`, `.dialog__select`
(`:1661-1679`), `.image-field__url-form` (`:1752`), `.checklist-*-form`
(`:2439`), `.itinerary-add-day` (`:2113`) — and confirm each now computes to
16px rather than inheriting 14px from a different ancestor. `.assist__context
input` (`:3811`) deliberately overrides the block-level sizing; leave it.

This changes the proportions of every form in the app, so it wants looking at,
not applying blind. The tap-target floor (`--tap-min: 2.75rem`, `:93`, applied
at `:2452-2489`) already lifts controls to 44px at phone width, so this is about
the zoom, not the target size.

**Verify:** a UI assertion that the computed `font-size` of the login input is
`>= 16px` — an assertion, not a screenshot. Existing tap-target sweeps in
`routes.spec.js` stay green. Then a manual pass at 324×756 against `make dev`
across the auth form, the trip form, the location editor and a dialog, because
the point of the milestone is that the new proportions look right.

---

## 8. The contrast gate learns to reach the whole app

`tests/ui/contrast.js`, `Makefile:137-139`.

Today `CONTRAST_ROUTES` is three id-free paths and the script navigates literal
strings, asserting the landed path matches (`contrast.js:266-280`). Teach it
route *templates*: after the in-page login (`:253-265`), fetch `/api/trips`,
take the seeded trip's id, and substitute `{trip}` into any route containing it.
Then widen `CONTRAST_ROUTES` to the trip tabs (overview, locations, map,
itinerary, expenses, checklists, files, members) and the location editor.

- The landed-path assertion has to compare against the *substituted* route, or
  every templated route fails.
- Map tiles are already blocked by `page.route` (`:246-250`), so the map tab
  measures its chrome and legend without waiting on the network.
- Dialogs stay **out of scope** — they need interaction machinery the script
  does not have. Say so in the file's header comment so the next reader knows it
  was a decision.

Expect the widening to surface real failures; that is the point. Fix what it
finds, or add a justified `EXEMPT` entry (`:56-66`) with a `why` string in the
existing style.

**Verify:** `make check-contrast` green over the widened list, in both schemes,
with `--strict`. The script's own too-few-elements guard (`:437-440`) confirms it
actually measured the new routes rather than silently skipping them; note the
before/after element count in the Done paragraph.

---

## 9. `mobile-map.png` catches up

`docs/assets/screenshots/mobile-map.png` (and whatever else the run changes).

Stage 23 Milestone 7 raised the mobile trip map from 320px to 85vh; the
committed capture predates it. A previous attempt was deliberately not
committed because two or three tiles per run failed with
`NS_ERROR_UNKNOWN_HOST`, leaving grey rectangles in `map.png`,
`location-detail.png`, `assistant.png` and `mobile-map.png`.

Since Stage 23 Milestone 8, `shoot()` waits for every tile to be loaded or
definitively failed, retries the failed ones once, and prints
`WARNING: <name> has N tile(s) that would not load`
(`gen_screenshots.mjs:170-173`). So the run is now self-reporting.

Run `make screenshots` with `PHOTO_DIR` pointing at `images/`. **If any WARNING
appears, commit nothing** and leave the backlog entry standing with a note about
the attempt — a stale but clean screenshot beats a current but broken one. If
the run is clean, commit the changed captures.

**Verify:** the run's own output — zero WARNING lines — plus
`make check-screenshots` (already in `make ci`) and a visual check that
`mobile-map.png` shows the tall map.

---

## Build order

0 → 1 → 2 → 3 → 4 → 5 → 6 → 7 → 8 → 9.

**1 must precede 2**: the loopback exemption is only safe once a proxied request
resolves to its real client rather than to the proxy. **5 must precede 6**
(endpoint before caller). Everything else is independent; 3, 4, 7, 8 and 9 can
move if something turns out to be in the way.

## Files this touches

- `internal/config/config.go` (+ `config_test.go`), `cmd/caravel/main.go`.
- `internal/httpapi/`: `auth.go`, `security.go`, `router.go`,
  `staticassets.go`, `trips.go`, new `trips_create.go`, and their tests plus a
  new request helper in `testing_test.go`.
- `internal/imaging/imaging.go`, `internal/httpapi/media.go`.
- `web/js/pages/trip-editor-page.js`, `web/js/components/trip-form.js`,
  `web/css/base.css`, possibly `web/locales/{en,de}.json`.
- `scripts/with_server.sh`, `tests/ui/contrast.js`, `Makefile`.
- `tests/ui/trip-editor.spec.js`, plus whichever spec carries the font-size
  assertion.
- `docs/running/reverse-proxy.md`, `docs/configuration/server.md`,
  `.env.sample`.
- `plans/stage-24.md`, `plans/todo.md`, `docs/assets/screenshots/`.

## Out of scope, deliberately

Trip-level AI suggestions; the Google Maps place-ID investigation; assistant
tool batching and prompt caching; vector tiles; the `item` → `location`
identifier sweep; `Intl` locale formatting; the `X-Real-IP` header; contrast
measurement inside dialogs; and the `CARAVEL_ASSIST_RATE_LIMIT` plumbing bug,
which Milestone 0 records in the backlog instead.

## Verification

Every milestone: `make ci` green, plus its own named proof above — an assertion
rather than a screenshot wherever one is possible. `make test-postgres` for
Milestone 5. A manual pass at 324×756 against `make dev` for Milestones 6 and 7.
Milestone 2's real proof is three consecutive full `make test-ui` runs, because
the symptom was a flake at roughly one run in two and no unit test reproduces
that.

## Workflow

One milestone at a time, in the order above. For each: implement, verify
(`make ci` plus the milestone's own proof), add a **Done.** paragraph to that
milestone's section in `plans/stage-24.md` describing what actually landed —
including any deviation from this plan — and how it was verified, reconcile
`plans/todo.md` in both directions, commit (one commit per milestone; a
same-day follow-up fix gets its own "... follow-up: ..." commit), make sure
`make dev` is running, then stop and hand back control. Do not start the next
milestone until told to continue; feedback given at a checkpoint is fixed and
re-verified before moving on.
