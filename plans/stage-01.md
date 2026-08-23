# Stage 01 — Caravel v1 Implementation Plan

> **Status: complete.** All 10 milestones in Section 6 have been implemented,
> verified (including a full Postgres parity smoke test), and hardened per
> Section 6, milestone 10. Kept here as a record of the v1 design decisions
> and rationale for future reference.

## Context

Caravel is a new, greenfield web application for planning trips: creating a "Trip," filling it with locations to visit, places to stay, and transportation options, laying those out on a map, building a day-by-day itinerary, and attaching documents/images/notes along the way. The name was chosen after checking for naming conflicts with existing apps/projects (candidates considered and rejected for existing conflicts included Waypoint, Roamly, Tripline, Wanderlog, Wanderlust, Wayfinder, Sextant, and others — see naming discussion). The project directory is currently empty — this plan defines the v1 MVP from scratch.

The long-term vision (discussed with the user) includes multi-user sharing/permissions, federation between self-hosted instances, public shareable links, a trip journal, expense splitting, S3-compatible storage, and Prometheus/OpenMetrics-compatible metrics — but v1 deliberately scopes down to a single-user-per-account core: auth, trips, items (locations/stays/transport), map, itinerary, and documents. The schema and architecture below are chosen so none of the deferred features require a redesign later (see Section 7).

Key decisions locked in with the user before this plan was written:
- Container entity is called **"Trip"** (not Vacation/Adventure/Expedition).
- Backend: **Go**, with **SQLite as the zero-config default** and **PostgreSQL as an optional swap** for larger installs — via the same query layer, not two parallel codepaths.
- Frontend: **plain JavaScript, no framework**, Web Components for reusable pieces, mobile-first responsive design that also works well on desktop, installable as a **PWA**.
- Icons: **Lucide**. Theming: light/dark via `prefers-color-scheme`, defaulting to system. i18n from day one: **English (default) + German**.
- Auth: **local users only in v1** (OpenID Connect explicitly deferred, but the schema must not block adding it later).
- Manual "paste an image URL" and "upload a file" cover image needs for v1; no image search feature is planned.

---

## 0. Guiding principles

- Single Go binary serves both API and static frontend assets — simplest deploy story (one binary + one SQLite file).
- SQLite is the default; PostgreSQL is a supported alternative via the same query layer, not a bolt-on.
- No frontend build system for v1 — native ES modules are served directly, unbundled, in both dev and production (see Section 4.7 for when/if a bundler becomes worth adding).
- Every design decision leaves a door open for the explicitly deferred features (sharing, OIDC, expenses, federation, public links, journal) without forcing a schema rewrite.

---

## 1. Project structure / repo layout

Single Go module, monorepo:

```
caravel/
  go.mod
  cmd/caravel/            # main package, wires everything, starts HTTP server
  internal/
    config/                   # env/flag parsing (DB driver, DB path/DSN, port, upload dir, base URL)
    db/
      migrations/{sqlite,postgres}/   # SQL migration files, embed.FS, golang-migrate compatible
      sqlc/queries/                   # .sql query files (shared, dialect-specific where needed)
      sqlc/{sqlite,postgres}/gen/     # generated Go per dialect
      store.go                        # Store interface + dialect selection, transaction helper
    auth/                      # password hashing, session issuing/validation, middleware
    httpapi/                   # HTTP handlers by resource (trips.go, items.go, itinerary.go, documents.go, auth.go)
      router.go                # route table (chi)
    trip/ item/ itinerary/     # domain/service layers, business rules per resource
    storagefs/                 # file storage abstraction (local filesystem in v1)
    imaging/                   # image resize/thumbnail on upload
  web/                        # frontend source, embedded via go:embed in prod build
    index.html  manifest.webmanifest  sw.js
    css/base.css               # resets, light/dark CSS var tokens, layout grid, breakpoints
    js/
      app.js  router.js  api.js  i18n.js
      components/               # trip-card.js, item-form.js, leaflet-map.js, itinerary-day.js, ...
      pages/                     # login-page.js, trips-page.js, trip-detail-page.js
      vendor/                    # leaflet (vendored, no CDN), lucide icons (SVG sprite)
    icons/                     # app icons for manifest, favicon
    locales/en.json  locales/de.json
  uploads/                    # runtime data dir (gitignored) — trip/item images, documents
  data/                       # runtime data dir (gitignored) — sqlite file
  Makefile
```

Key decisions:
- **go:embed** the `web/` directory into the binary by default. A config flag/env var (`CARAVEL_WEB_DIR`) overrides this at startup to instead serve static files live from a local directory on disk with no-cache headers — this is the dev workflow (edit a file, refresh the browser, no rebuild/restart needed), while the embedded copy remains what ships in a built binary for deployment.
- **Repository/service split**: `internal/db` (sqlc-generated, dialect-specific) is the lowest layer; `internal/trip`, `internal/item`, `internal/itinerary` are the domain/service layer depending on a `Store` interface — handlers never touch SQL directly.
- No ORM (see Section 2).
- Frontend needs no build step at all for v1, in dev or production — native ESM `<script type="module">`, import map for `leaflet` (see Section 4.7).

---

## 2. Data model & SQLite/Postgres dual support

### 2.1 Dual-dialect strategy: **sqlc**, with per-dialect schema files

`sqlc` generates type-safe Go from SQL for both SQLite and Postgres, avoiding ORM magic/N+1 footguns while eliminating hand-written `Scan`/`Rows` boilerplate. Two `sqlc.yaml` configs point at a shared set of `queries/*.sql` files where syntax is identical, with a small number of dialect-specific overrides where it diverges (upsert syntax, `LIMIT/OFFSET` binding, boolean literals, date/time functions).

Concretely:
- Canonical DDL is written once **per dialect** (`migrations/sqlite/0001_init.sql`, `migrations/postgres/0001_init.sql`), kept in lockstep by convention — a shared abstraction layer isn't worth building for two dialects given column-type differences (TEXT vs VARCHAR, no native BOOLEAN in SQLite, etc.).
- **IDs are UUIDs stored as TEXT** (not autoincrement integers) — sidesteps the SQLite `AUTOINCREMENT` vs Postgres `SERIAL`/`IDENTITY` mismatch, and gives non-guessable, globally unique IDs that help future public-links/federation phases.
- Timestamps: `TEXT` ISO-8601 UTC in SQLite, `TIMESTAMPTZ` in Postgres; sqlc maps both to `time.Time`.
- Migrations via `github.com/golang-migrate/migrate/v4`, SQL embedded via `embed.FS`, run automatically on startup.
- **SQLite driver: `modernc.org/sqlite`** (pure Go, no CGO) — keeps cross-compilation/Docker builds simple, important for the "zero-config default" story. Enable `PRAGMA journal_mode=WAL` and `busy_timeout` on connect.
- **Postgres driver: `github.com/jackc/pgx/v5`** with `pgx/v5/stdlib` so the same `database/sql`-based sqlc code path works for both dialects.
- `internal/db/store.go` defines a `Store` interface the domain layer depends on; `sqliteStore`/`postgresStore` each wrap their sqlc `Queries`. Dialect is selected once at startup from config (`CARAVEL_DB_DRIVER=sqlite|postgres`). `Store.WithTx(ctx, func(Store) error)` wraps sqlc's per-dialect tx support.

### 2.2 Schema

**users** — id (UUID), username (unique), display_name, email (nullable), created_at, updated_at. **No password column here** — see `auth_identities` below.

**auth_identities** — id, user_id (FK), provider (TEXT, `'local'` for v1), provider_user_id, password_hash (nullable, only set for `provider='local'`), created_at. Rationale: keeps `users` provider-agnostic from day one so OIDC bolts on later as a new row type with zero migration to `users`, rather than retrofitting a nullable `password_hash` onto `users` now and moving it later.

**sessions** — id (opaque token, stored hashed), user_id (FK), created_at, expires_at, last_seen_at, user_agent, ip.

**trips** — id, **owner_id (FK → users, present from day one)**, title, start_date (nullable), end_date (nullable), preview_image_id (FK → media_assets, nullable), notes (markdown TEXT), created_at, updated_at.

**items** — id, trip_id (FK, cascade), **category** (TEXT enum: `location`|`stay`|`transport` — discriminator), **type** (free-text tag, e.g. "mountain", "hotel" — deliberately not a rigid enum; a curated suggestion list lives in frontend i18n data only), title, notes (markdown), image_id (FK → media_assets, nullable), show_on_map (bool, default true — the "excludable from map" flag), sort_order, created_at, updated_at.

**item_locations** (1:1 with items) — id, item_id (FK, unique, cascade), lat (nullable), lng (nullable), address (nullable free text) — lat/lng and address are independently nullable.

**item_links** (1:many) — id, item_id (FK, cascade), url, label (nullable), sort_order.

**item_dates** (1:many) — id, item_id (FK, cascade), start_date (nullable), end_date (nullable), label (nullable, e.g. "check-in"), all_day, start_time/end_time (nullable). A separate table (not two columns on `items`) because transport often needs multiple date/time segments and stays need a single range — one shape covers both.

**media_assets** — id, kind (`upload`|`url`), storage_path (nullable, set when uploaded), external_url (nullable, set when pasted), content_type, width/height (nullable), created_at. Single table backs both `trips.preview_image_id` and `items.image_id`.

**documents** — id, trip_id (FK, cascade, always set), item_id (FK, cascade, **nullable** — null = trip-level general document, set = per-item document), filename, storage_path, content_type, size_bytes, uploaded_at.

**itinerary_days** — id, trip_id (FK, cascade, unique on (trip_id, date)), date, notes (markdown, nullable).

**itinerary_entries** — id, itinerary_day_id (FK, cascade), item_id (FK → items, cascade), sort_order, note (nullable). Many-to-many join between days and items — an item spanning multiple days (e.g. a 3-night stay) gets one row per day, not a range reference, keeping itinerary reads a simple join with no interval-overlap logic.

Indexes: `trips(owner_id)`, `items(trip_id, category)`, `item_locations(item_id)` unique, `documents(trip_id)`, `documents(item_id)`, `itinerary_days(trip_id, date)` unique, `sessions(user_id)`, `sessions(expires_at)`.

---

## 3. Backend API design

### 3.1 Router & middleware
- **`github.com/go-chi/chi/v5`** — idiomatic `net/http`-compatible, no reflection magic, good subrouter/middleware ergonomics for organizing `httpapi/` by resource.
- Middleware: structured request logging, panic recovery, session-auth (loads user from cookie into request context), CSRF protection (double-submit cookie or `gorilla/csrf`) on state-changing requests, response compression.
- Keep `/api/*` reserved for the JSON API and route a future `/metrics` endpoint at the top level, outside that group and outside session-auth middleware (Prometheus scrapers don't send session cookies) — see Section 7 for the metrics feature itself; this is just the routing reservation so it slots in later without moving other routes.

### 3.2 Auth
- **Cookie sessions, not JWT.** This is a same-origin app (one binary serves API + frontend) with no third-party API consumers in v1 — cookie sessions give trivial server-side revocation (delete the row = logged out everywhere) and avoid token-refresh complexity. `HttpOnly` + `Secure` + `SameSite=Lax`.
- Password hashing: **argon2id** via `github.com/alexedwards/argon2id` (modern default, misuse-resistant API).
- Session token: random 32 bytes (`crypto/rand`), **stored hashed** (SHA-256) server-side, raw value in the cookie — a DB leak doesn't hand over usable tokens.
- Sliding expiration (extend `expires_at` on activity, e.g. 30-day idle timeout) plus a sweeper for expired rows.
- Endpoints: `POST /api/auth/login`, `POST /api/auth/logout`, `POST /api/auth/register` (self-registration open by default for a self-hosted app, behind a config flag), `GET /api/auth/me`.

### 3.3 Resource endpoints
All under `/api/`, JSON, auth required except login/register:
- `GET/POST /api/trips`, `GET/PATCH/DELETE /api/trips/{tripId}`
- `GET/POST /api/trips/{tripId}/items`, `GET/PATCH/DELETE /api/items/{itemId}` (`?category=` filter on list)
- `PUT /api/items/{itemId}/location`, `POST/DELETE /api/items/{itemId}/links/{linkId}`, `POST/DELETE /api/items/{itemId}/dates/{dateId}`
- `GET /api/trips/{tripId}/itinerary` (days + nested entries + resolved item summaries in one call), `PUT /api/trips/{tripId}/itinerary/days/{date}`, `POST/DELETE /api/itinerary/days/{dayId}/entries`
- `GET/POST /api/trips/{tripId}/documents` (trip-level), `GET/POST /api/items/{itemId}/documents`, `DELETE /api/documents/{docId}`, `GET /api/documents/{docId}/download`
- `POST /api/media` (multipart upload) and `POST /api/media/url` (paste-a-URL) — both return a `media_asset`, used by trip preview image and item image forms
- `GET /api/trips/{tripId}/map` — items with resolvable locations and `show_on_map=true`, pre-shaped for Leaflet (id, title, category, lat, lng, google_maps_url)

### 3.4 File storage
- **Local filesystem by default, not blob-in-DB** — keeps the SQLite file small/fast to back up, streams via `http.ServeContent` (Range support, needed for PDFs), no `database/sql` blob streaming.
- `internal/storagefs` defines a small `Blob` storage interface (`Put`, `Get`/`OpenReader`, `Delete`, given a storage key) rather than handlers calling `os.*` directly — mirrors the `Store` DB abstraction in Section 2.1. v1 ships one implementation (`localfs`) backed by the filesystem; adding an `s3` implementation (via `github.com/aws/aws-sdk-go-v2/service/s3`, S3-compatible so it also covers MinIO/Backblaze/etc.) later is additive — swap the configured implementation, no changes to callers or the `documents`/`media_assets` schema, since both already store an opaque storage key/path string rather than an OS-specific path.
- Storage key convention (works unchanged for either backend): `{trip_id}/items/{item_id}/{media_asset_id}-{filename}`, `{trip_id}/documents/{media_asset_id}-{filename}`, `{trip_id}/images/{media_asset_id}.{ext}`.
- Serving: authenticated download handlers that check trip ownership before streaming — not a raw static directory, even with UUID paths.
- Image handling: decode with stdlib `image` (+`image/jpeg`, `image/png`, `golang.org/x/image/webp` for reads), resize via `golang.org/x/image/draw` (no CGO/libvips dependency), generate a capped-size variant + thumbnail per upload.
- Upload limits via `http.MaxBytesReader` (e.g. 15MB images, 50MB documents, configurable).

---

## 4. Frontend architecture

### 4.1 Pages
Client-side routed via History API (Go serves `index.html` as fallback for non-`/api` paths):
- `/login` — login/register
- `/trips` — trip list (grid of `<trip-card>`, "+ New Trip")
- `/trips/:id` — trip detail shell with Overview/Items/Map/Itinerary/Documents sections — bottom/top tab bar on mobile, persistent left sidebar on desktop (same components, CSS breakpoint swap)
  - **Overview**: title/dates/preview image/notes, edit-in-place
  - **Items**: filterable by category, add/edit via modal or `/trips/:id/items/:itemId`
  - **Map**: full Leaflet map, per-category toggle, marker popup with "View on Google Maps" link
  - **Itinerary**: day-by-day agenda derived from trip dates, assign items to days
  - **Documents**: trip-level general documents; per-item documents live within each item's detail view

### 4.2 Web components
- Native Custom Elements + Shadow DOM for reusable pieces: `<trip-card>`, `<item-card>`, `<leaflet-map>`, `<itinerary-day>`, `<markdown-view>`, `<file-upload>`, `<modal-dialog>`, `<icon>` (Lucide SVG sprite lookup).
- Page controllers (`pages/*.js`) are plain modules (not custom elements) owning fetch calls and orchestrating components.
- No virtual-DOM library — components rebuild their own shadow subtree on data change via `render()`.
- Cross-component notifications via a single shared `EventTarget` event bus (e.g. "item saved" → map/itinerary/list refresh) instead of a state-management layer.

### 4.3 Leaflet integration
- Vendor Leaflet JS+CSS (npm-installed at build time, copied into `web/js/vendor/`) — no CDN dependency, works offline as part of the PWA app shell.
- `<leaflet-map>` lazy-loads Leaflet via dynamic `import()` only when the Map tab is activated.
- Tile layer: **OpenStreetMap standard tiles**, proper attribution, no API key/billing.
- Category-colored markers via Leaflet `divIcon` + Lucide icons; popup includes a "View on Google Maps" link (`https://www.google.com/maps/search/?api=1&query={lat},{lng}`, or by address if no coordinates) — this covers the outbound-link requirement with zero Google API integration.
- `map.fitBounds()` on load; category checkboxes filter already-fetched markers client-side.
- **Recommendation**: Leaflet + OSM as the default map, not the Google Maps JS SDK — no API key/billing risk, aligned with the self-hosted/no-framework approach. The Google Maps link covers what Google is genuinely better at (turn-by-turn, street view, reviews). If OSM's basemap fidelity becomes a real complaint later, swapping the tile URL to MapTiler/Stadia Maps/Thunderforest is a one-line config change, not a rewrite — low-regret decision.

### 4.4 i18n
- Flat JSON per locale (`web/locales/en.json`, `de.json`), dotted keys (`trip.form.title`). No ICU/plural-rule library needed at this copy volume — the i18n helper handles a simple `key`/`key_plural` pick via a `count` param.
- `i18n.js`: loads the active locale once at boot (from `navigator.language`, matched against supported locales, overridable and persisted in `localStorage`); exposes `t(key, params)` with `{name}`-style interpolation, and `translatePage(root)` walking `[data-i18n]` (+ `data-i18n-placeholder`/`data-i18n-aria-label`) attributes for static markup.
- Locale switch re-runs `translatePage` and emits `locale-changed` on the event bus so components re-render.
- Dates/numbers via native `Intl.DateTimeFormat`/`Intl.NumberFormat`.

### 4.5 Theming
- CSS custom properties at `:root` for light values, overridden in `@media (prefers-color-scheme: dark)` — system-driven by default in v1, structured so a manual `data-theme` override can be added later without restructuring.
- Design tokens (`css/base.css`): color roles (`--color-bg`, `--color-surface`, `--color-text`, `--color-accent`, per-category marker colors), spacing/radius scale.

### 4.6 PWA
- `manifest.webmanifest`: name/icons/`display: standalone`/theme colors/`start_url: /trips`.
- `sw.js`: cache-first for the app shell only (HTML, JS/CSS files, icons, vendored Leaflet) — `/api/*` requests always bypass the service worker (no offline data sync in v1, per scope). Cache name versioned to a value bumped on each release (e.g. an app version string) so deploys invalidate cleanly, since there's no build-hash to key off without a bundler.

### 4.7 Build tooling
- **No build step in v1, dev or prod** — native `<script type="module">` served as-is, import map aliases `leaflet` to the vendored path. This keeps the toolchain to just Go (no Node/npm dependency at all) and is the right amount of complexity for a no-framework app at this scale.
- Revisit adding a bundler (e.g. esbuild) later only if it becomes a real pain point — e.g. too many individual HTTP requests hurting load time on slow connections, or a desire for minification — rather than adding it preemptively. If added, the natural point is alongside the service worker (Section 4.6), since a build-content-hash is what makes SW cache invalidation clean.

---

## 5. Itinerary design

- `itinerary_days` rows are created lazily — when a trip has both dates, the itinerary view computes the full date range and only persists a day row once it has notes or an entry; days without content render as empty placeholders derived from the range.
- Trips without an `end_date` (or no dates at all) still work: only days with actual content show, plus a manual "add a day" affordance.
- `itinerary_entries` is the day↔item join; a multi-night stay gets one row per day it spans (UI can offer a "add to every day of this stay" convenience action driven by the item's `item_dates` range), keeping itinerary reads a simple join rather than interval-overlap logic — the right tradeoff since itinerary is read far more than written.
- Ordering within a day via `sort_order`, adjustable via native drag-and-drop or a minimal pointer-events reorder (no external DnD library needed for a single-axis list).

---

## 6. Build order / milestones

1. **Scaffold** — Go module, chi skeleton, config, sqlc for both dialects, golang-migrate wiring, `modernc.org/sqlite` + `pgx` drivers, health-check endpoint, static serving of a placeholder page. `make run` works against SQLite.
2. **Auth + users** — `users`/`auth_identities`/`sessions` migrations, argon2id, login/register/logout/me endpoints, session middleware, minimal login page with i18n wired end-to-end early (not bolted on later).
3. **Trips CRUD** — `trips` table, full REST CRUD, trip list + create/edit forms, dark/light CSS tokens established here (first real UI surface).
4. **Items CRUD (no map yet)** — `items`, `item_locations`, `item_links`, `item_dates`; per-category list/detail forms; validates the discriminator-column approach end to end.
5. **Media** — `media_assets`, upload + URL-paste endpoints, image resize pipeline, wired into trip preview image and item image fields.
6. **Map view** — `/api/trips/:id/map`, vendored Leaflet, `<leaflet-map>`, category toggle, Google Maps links, `show_on_map` exclusion UI.
7. **Itinerary** — `itinerary_days`/`itinerary_entries`, day-range generation, itinerary UI, item-to-day assignment.
8. **Documents** — `documents` table, per-item + trip-level upload/list/download endpoints and UI.
9. **PWA + i18n completion + polish** — manifest/service worker/icons, full German coverage across all screens, responsive breakpoint pass, accessibility pass.
10. **Hardening** — CSRF review, upload validation review, session sweeper, login rate limiting, Postgres path smoke-tested end to end (only revisited here to avoid dialect-debugging mid-feature), backup/restore docs for the SQLite file + uploads directory.

Each milestone should be demoable against SQLite before moving on; Postgres is deliberately only exercised at milestone 10.

---

## 7. Future phases (confirming v1 doesn't block them)

- **Sharing/collaboration/permissions** (owner/participant/viewer) — `trips.owner_id` already exists; add a `trip_collaborators` (trip_id, user_id, role) join table and change authorization checks from "owner_id == current user" to "role >= X" — additive.
- **Public shareable links** — IDs are already non-guessable UUIDs; add a `share_links` table (token, trip_id, scope, expires_at) plus an unauthenticated read-only route variant — additive.
- **Expenses/cost-splitting** — a new `expenses` table referencing `trip_id`/optionally `item_id` — no changes to existing tables.
- **Federation between servers** — UUID-based IDs and the `Store` interface abstraction reduce friction; the sync protocol itself still needs real design later, but v1 avoids the integer-PK/local-only-ID mistakes that would make it harder.
- **Trip journal with photos** — `media_assets` already generalizes "image, uploaded or linked"; add a `journal_entries` table (trip_id, date, body markdown) with a join to `media_assets` for multiple photos, reusing the existing image pipeline.
- **S3-compatible object storage** — the `internal/storagefs` `Blob` interface (Section 3.4) already isolates all callers from the storage backend; adding an `s3` implementation later is a config change plus one new small package, not a rework of documents/media handling.
- **Prometheus/OpenMetrics-compatible metrics** — not part of v1, but explicitly planned for a later phase. Expose a `GET /metrics` endpoint in the [OpenMetrics text format](https://openmetrics.io/) (which Prometheus scrapes natively), via `github.com/prometheus/client_golang/prometheus` + `promhttp.Handler()` — the de facto standard Go instrumentation library, so this is additive (a new dependency, a new route, and `prometheus.Counter`/`Histogram`/`Gauge` instances threaded through the chi middleware chain and the `Store`/`Blob` interfaces) rather than a redesign. Baseline metrics worth exposing when this is built: HTTP request count/duration/status by route (via a chi middleware, same pattern as the existing request-logging middleware), DB query duration and open-connection counts, upload counts/sizes, and session counts. The routing reservation in Section 3.1 (`/metrics` outside `/api` and outside session-auth middleware) is the only v1-relevant groundwork; everything else is deferred.

---

## Verification

- **Milestone-by-milestone manual testing** against SQLite: run `make run`, exercise each new endpoint via `curl`/browser and each new UI surface manually (login, create a trip, add items of each category, view the map, build an itinerary day, upload a document) before moving to the next milestone.
- **Mobile check**: use browser dev-tools device emulation (and a real phone if available) at each UI milestone to confirm the mobile-first layout and PWA installability (manifest + service worker registration, `Add to Home Screen` prompt).
- **Dialect parity check at milestone 10**: run the full migration + smoke-test flow against Postgres (`CARAVEL_DB_DRIVER=postgres`) to confirm no SQLite-only assumptions crept in.
- **i18n check**: toggle browser/OS language between English and German and confirm `translatePage` covers all visible strings with no missing-key fallbacks left showing raw keys.
