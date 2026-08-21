# Stage 14 — Multi-user: roles, sharing, visibility, administration

## Context

Caravel is single-user in the only place that matters: a trip has one
`owner_id`, and every one of the 37 trip-scoped handlers asks "is this trip
mine?" through one of five `loadOwned*` helpers, four of which funnel into
`hasTripAccess` (`internal/httpapi/media.go:246`). There is no roles table, no
members UI, and no admin concept anywhere — `grep -i admin` over `internal/`,
`cmd/` and `web/` returns nothing but ARIA attributes. Registration is open by
default (`internal/config/config.go:29`) and never closes, so the only way to
get a second account onto a self-hosted instance is to leave the front door
open.

`todo.md`'s "Multi-user and sharing" section already groups this work and says
explicitly that roles, per-visibility checklists and per-visibility files want
designing *together* rather than bolting a column onto an existing table. The
administrative half (admin flag, closable registration, user management) came
from the user's notes and was folded into the same section as part of this
plan. This stage does all of it.

Two neighbours stay out on purpose:

- **Public shareable links** — an unauthenticated read-only view via a token.
  A different model (a `share_links` table, an unauthenticated route variant)
  that gets *easier* once roles exist, not harder.
- **Expenses / cost-splitting** — a new feature that merely *depends* on
  multi-user rather than being part of it.

Intended outcome: a trip owner can add another account to a trip as editor or
viewer; a viewer gets a genuinely read-only app rather than buttons that 403; a
checklist or a file can be personal to its author; and the first registered
account is an admin who can create, remove and reset other accounts, with
registration closed to the outside by default.

**Decided with the user up front:**

1. **Three roles**, owner / editor / viewer — not two. "Just let them look"
   is the case people actually have, and answering it with a public share link
   instead would mean handing out a URL to someone who already has an account.
2. **Add members by exact username.** No email infrastructure, no invite-token
   table. This is a self-hosted app among people who know each other, and once
   Milestone 6 lands the admin is creating those accounts anyway. Invite links
   go on the backlog next to public share links, which they overlap.
3. **Visibility ships in this stage**, as its last two milestones, because
   `todo.md` is right that the visibility column wants designing alongside the
   roles rather than after them.
4. **The administrative half ships in full** — flag, self-closing
   registration, and the management screen. A sharing feature with no way to
   create the person you want to share with is half a feature.

---

## The design, in one place

Worth reading before Milestone 1: the four decisions below are what the ten
milestones are consequences of.

### Membership

New table `trip_members(trip_id, user_id, role, created_at)`, primary key
`(trip_id, user_id)`, both foreign keys `ON DELETE CASCADE`, `role` a `TEXT`
with `CHECK (role IN ('editor','viewer'))`.

`trips.owner_id` stays authoritative for the owner, and **the owner gets no
row**. Three reasons: the two can never disagree, there is no backfill to get
wrong, and "owner" is unrepresentable in the members table by construction —
so no code path can demote or delete an owner by touching a membership. The
cost is that the members *list* unions the owner in, which is one query and
one branch.

(Deviation from `todo.md`'s working name `trip_collaborators`: `trip_members`
matches the UI word, "Members".)

### Roles

`db.TripRole` (`owner` > `editor` > `viewer`) with an `AtLeast(TripRole) bool`
method, in `internal/db/domain.go` next to `Trip`.

- **viewer** — reads everything visible to them; no writes at all.
- **editor** — all content writes: locations, itinerary, files, checklists,
  media, and the trip's own title/dates/cover. Cannot manage members or delete
  the trip.
- **owner** — everything, plus members and trip deletion. Exactly one.

### The authorization seam

`hasTripAccess` becomes

```go
func (s *Server) tripRole(ctx context.Context, tripID string) (db.Trip, db.TripRole, bool)
```

and each of the five loaders takes the minimum role it needs:

```go
trip, role, ok := s.loadTrip(w, r, db.RoleEditor)       // from the {tripId} param
item, trip, role, ok := s.loadItem(w, r, db.RoleEditor) // and checklist / file / itineraryDay
```

Status codes matter here, and the existing contract has to survive —
`internal/httpapi/ownership_test.go:9-21` pins it in a header comment:

- **No role at all → 404** "trip not found". Trip existence still isn't leaked
  to strangers.
- **Role present but insufficient → 403.** A viewer already knows the trip
  exists; a 404 there would be a lie, and one that makes the client harder to
  write because it can't tell "gone" from "not allowed".

### SQL-layer scoping

Authorization is currently enforced twice — once in the loader, once in a
`WHERE` clause — and the second copy is about to become wrong. `UpdateTrip` and
`SetTripPreviewImage` (`internal/db/sqlc/queries/trips.sql:11,25`) carry
`AND owner_id = ?`; that predicate has to go, or an editor's save silently
no-ops with a 200. `DeleteTrip` **keeps** its `AND owner_id = ?`, because the
role required there is exactly `owner`, so the second belt costs nothing and
guards the most destructive call in the app.

`ListTripsByOwner` becomes `ListTripsForUser`:

```sql
WHERE t.owner_id = sqlc.arg(user_id)
   OR EXISTS (SELECT 1 FROM trip_members m
              WHERE m.trip_id = t.id AND m.user_id = sqlc.arg(user_id))
```

One named argument used twice, valid in both dialects.

### Admins are not superusers

`users.is_admin` governs *account* administration only. An admin gets no
access to another user's trips, and `tripRole` never consults it. Anything
else would make "personal file" a lie — and a boarding pass that the instance
operator can read is not a private file.

### Visibility

Files get two states, checklists three. The axis differs because a checklist
can be *ticked* and a file cannot:

|            | `personal`                | `trip`                                        | `shared`                    |
| ---------- | ------------------------- | --------------------------------------------- | --------------------------- |
| files      | only the uploader sees it | everyone on the trip; editors may edit/delete | —                           |
| checklists | only the author           | everyone sees, only the author ticks          | everyone sees *and* ticks   |

Both tables gain `owner_user_id` (backfilled to the trip owner) and a
`visibility` column with a CHECK constraint. Defaults are **`trip`** for files
and **`shared`** for checklists, the control is shown at creation time, and it
is **hidden entirely when the trip has no members besides the owner** — so a
solo trip never sees a choice that cannot mean anything.

On not defaulting files to `personal`, which `todo.md` argues for: an invisible
privacy default produces "where did my upload go?" rather than safety, and
every file on a solo trip would be born private for no reason. The risk it
guards against is better handled by making the choice visible at upload time
and putting a lock badge on personal rows. Flagged as the call in this stage
most worth re-examining once it is in use.

Viewers cannot create *anything*, including personal rows. Read-only means
read-only.

---

## 0. Land the plan

Commit this file, plus the `notes.md` → `todo.md` fold that preceded it (the
admin bullets into "Multi-user and sharing", the Google Maps pair into "Planned
features", `notes.md` deleted — `todo.md`'s own conventions paragraph asks for
exactly that), before any implementation.

---

## 1. The role model and the authorization seam

Backend only, no user-visible change — and the milestone the other nine react
to, which is why it goes first.

- Migration `0007_add_trip_members`, up **and** down, in **both**
  `internal/db/migrations/sqlite/` and `.../postgres/`.
- `internal/db/sqlc/queries/trip_members.sql`: `GetTripMember`,
  `ListTripMembers` (joining `users` for username and display name),
  `UpsertTripMember`, `DeleteTripMember`, `CountTripMembers`. Then
  `sqlc generate` **by hand** from `internal/db/sqlc/` (it emits both
  dialects), params structs and `Store` methods in `internal/db/store.go`, and
  both implementations in `sqlite_store.go` / `postgres_store.go`.
- `db.TripRole` + `AtLeast` in `internal/db/domain.go`.
- `tripRole` replaces `hasTripAccess`. The five loaders — `loadOwnedTrip`
  (`trips.go:152`), `loadOwnedItem` (`items.go:322`), `loadOwnedChecklist`
  (`checklists.go:112`), `loadOwnedFile` (`files.go:212`),
  `loadOwnedItineraryDay` (`itinerary.go:168`) — take a minimum role and
  return the caller's role. Note `loadOwnedItem` does its own two-query owner
  check today and does *not* go through `hasTripAccess`; convert it, so there
  is one seam rather than two.
- Annotate all 37 call sites with the role they need. Reads (`handleGetTrip`,
  `handleListItems`, `handleGetTripMap`, `handleGetItinerary`,
  `handleListTripFiles`, `handleListItemFiles`, `handleListChecklists`,
  `handleGetItem`, `handleDownloadFile`, `handleServeMedia`) →
  `RoleViewer`. Every mutation → `RoleEditor`. `handleDeleteTrip` →
  `RoleOwner`.
- **Fix two pre-existing gaps while the seam is open.**
  `handleSetTripPreviewImage` (`trips.go:229`) and `handleSetItemImage`
  (`items.go:611`) pass a client-supplied `media_asset_id` straight to the
  store without checking the asset's own `trip_id` — harmless while every trip
  has exactly one owner, not harmless with members.
  `handleCreateItineraryEntry` (`itinerary.go:224`) already does the right
  check; copy that shape.

**Verify.** Extend `internal/httpapi/ownership_test.go` from a cross-user 404
matrix into a role matrix: stranger → 404 everywhere; viewer → 200 on reads and
403 on every mutation; editor → 200 except members and delete-trip; owner →
all. Plus a test that a cross-trip `media_asset_id` is now rejected. `make ci`
green.

**Done.** Landed as planned, with three deviations worth recording.

*The seam.* New `internal/httpapi/authz.go` holds all of it: `tripRole`,
`authorizeTrip` (which applies the 404-vs-403 policy in one place), the five
loaders `loadTrip` / `loadItem` / `loadChecklist` / `loadFile` /
`loadItineraryDay`, and `requireSameTrip`. `hasTripAccess` and all five
`loadOwned*` helpers are gone; `loadOwnedItem`'s separate two-query owner check
went with them, so there is now one seam rather than two. All 37 call sites
carry an explicit `db.RoleViewer` / `RoleEditor` / `RoleOwner`, which makes the
required role readable at each handler instead of implied by which loader it
called.

*Deviation 1 — the role matrix is its own file.* `roles_test.go` rather than an
extension of `ownership_test.go`. The two pin different contracts (no role at
all → 404, insufficient role → 403) and both are load-bearing, so keeping them
apart says that more clearly than one file with two modes. `ownership_test.go`
gained a header note pointing at the other.

*Deviation 2 — the store signatures changed, not just the SQL.*
`UpdateTripParams.OwnerID` and `SetTripPreviewImage`'s `ownerID` parameter were
removed rather than left in place and ignored. A parameter the query no longer
reads is a trap for the next caller.

*Deviation 3 — an extra query fix.* `sqlc` does not substitute named arguments
inside an `ON CONFLICT ... DO UPDATE` clause: `UpsertTripMember` generated
literal `SET role = sqlc.arg(role)`, which would have failed at runtime rather
than at build time. Rewritten as `SET role = excluded.role`, which both dialects
accept. Worth remembering — the generated file is the only place that mistake is
visible, so it wants reading after `sqlc generate`, not just diffing for churn.

*Also fixed, as planned:* both `media_asset_id` handlers
(`handleSetTripPreviewImage`, `handleSetItemImage`) now confirm the asset
belongs to the trip being edited.

**Verified.** `make ci` green. `roles_test.go` adds `TestRoleMatrix` (29 routes
× 4 caller kinds, fresh fixture per row because half the routes are
destructive), `TestRoleMatrixUploads` for the three multipart routes,
`TestEditorWritesActuallyLand` and `TestMediaAssetFromAnotherTripIsRejected`.
All pre-existing tests pass unchanged, which is the real evidence that the
stranger → 404 contract survived the rewrite.

Then the part that mattered most: each new test was checked against a
deliberate break, because a passing assertion proves nothing on its own.

- `AtLeast` forced to `return true` → `TestRoleMatrix` **passed**. The matrix
  was asking `db.TripRole.AtLeast` which outcome to expect, so breaking the
  production check flipped the expectation with it and every viewer write was
  permitted silently. Only `TestRoleMatrixUploads`, which hardcodes its
  statuses, caught it. Fixed by duplicating the role ordering in the test
  (`testRank` / `permitted`) as an independent statement of the rule; the
  matrix now fails on that break. This is the milestone's most useful finding
  and the reason the break-check is not optional.
- 404 changed to 403 for the no-role case → `TestTripRoutesRejectAnotherUser`,
  `TestRoleMatrix` and `TestRoleMatrixUploads` all fail.
- `requireSameTrip` neutered → `TestMediaAssetFromAnotherTripIsRejected` fails.

Live check against `make dev-restart MARKER=trip_members` (migration applied on
startup, confirmed in `data/caravel.db`): as a viewer on the seeded Iceland
trip, `other` got 200 on the trip and its items, 403 on PATCH trip, 403 on
create checklist, 403 on delete trip; promoted to editor, 200/201 on the two
writes and still 403 on delete. The membership rows and the probe checklist were
removed afterwards, so the seed is back as it was — a mutating manual test that
leaves the seed changed poisons every later run.

---

## 2. Trips list, trip payload, and the client's notion of role

- `ListTripsByOwner` → `ListTripsForUser`; `handleListTrips` (`trips.go:61`)
  passes the user twice.
- Drop `AND owner_id` from `UpdateTrip` and `SetTripPreviewImage`.
- `tripResponse` gains `"role"` and `"owner"` (`{username, display_name}`), so
  the client can both decide what to render and say who shared it.
- New `web/js/trip-role.js`: `canEdit(trip)`, `canManageMembers(trip)`,
  `isViewer(trip)`, reading `trip.role`. Every tab already receives the whole
  `trip` (`trip-detail-page.js:123-149`), so nothing needs threading through.
- Trips list: a small "shared" marker on `<trip-card>` for trips the user does
  not own.

**Verify.** Go tests — a member sees the trip in `GET /api/trips`, an editor can
PATCH it, a stranger still cannot see or touch it. Playwright once the seeder
change in Milestone 3 exists; until then, an API-level check is the honest
evidence.

**Done.** Landed as planned. Three things are worth recording.

*One query, not N+1.* `ListTripsForUser` returns the role and the owner's name
inline — `CAST(CASE WHEN t.owner_id = @user_id THEN 'owner' ELSE m.role END AS
TEXT)`, a `JOIN users` for the owner, and a `LEFT JOIN trip_members` pinned to
the one user. The LEFT JOIN cannot duplicate a trip because `(trip_id, user_id)`
is the primary key and `user_id` is fixed, which is what makes selecting the role
inline safe. The `CAST` is load-bearing: without it sqlc types the `CASE` as
`interface{}` in *both* dialects, and the store adapter would have had to
type-switch on whatever the driver handed back. Generated types were checked
after `sqlc generate` rather than assumed — the same lesson as Milestone 1's
`ON CONFLICT` gap.

*`tripToResponse` now takes a role*, and every caller had one to give: the
loaders return it, and `handleCreateTrip` passes `RoleOwner` because the creator
is the owner by construction. `handleListTrips` builds its rows directly instead
of going through the helper, which would have spent a `GetUserByID` per shared
trip re-fetching what the join already returned.

*`owner` is omitted on your own trips.* On a trip you own it would only tell you
your own name, and its presence is what the client uses as "this was shared with
me" — so `trips-page.js` tests `if (trip.owner)` rather than comparing roles. It
also carries no user id: a display label is all the feature needs, and handing
every collaborator the owner's id would disclose more than that.

Frontend: new `web/js/trip-role.js` (`canEdit` / `canManageMembers` / `isViewer`
/ `isOwner`), which ranks an unknown or absent role as 0 so a stale payload
fails closed rather than reading as permissive. `<trip-card>` gained a
`shared-label` attribute — pre-translated by the caller, the same way it already
takes a pre-formatted date range rather than a formatter, so the element stays
attribute-driven with no i18n import. New key `trips.sharedBy` in both locales.

Note for later: `trip-role.js` has no consumers yet. Milestone 4 is what uses
it; landing it here keeps the payload change and its interpretation in one
reviewable commit.

**Verified.** `make ci` green (177 keys in sync); all 59 Playwright specs pass
unchanged. Four new Go tests: `TestTripListIncludesSharedTrips` (the shared trip
appears, with role and owner; the actor's own trip does not carry an owner block;
and the owner's own list has not grown a duplicate row from the join) and
`TestTripPayloadCarriesReaderRole` across all three roles, including that the
owner block leaks no user id.

Break-checked, all three caught: reverting the list query to owner-only fails
`TestTripListIncludesSharedTrips`; hardcoding the payload role to `owner` fails
`TestTripPayloadCarriesReaderRole`; populating the owner block unconditionally
fails the list test.

Live check after `make dev-restart MARKER=ListTripsForUser`: `other`'s trip list
was empty, then showed exactly `Demo: Iceland Ring Road` with `role=viewer` and
`owner={demo, Demo User}` once a viewer row existed, while `demo` still saw 8
trips with one Iceland row, `role=owner`, `owner=null`. In the browser at
324×756 the card reads "Shared by Demo User" (12.8px, muted, 7.03:1 contrast on
light) and "Geteilt von Demo User" in German on one line at 5.81:1 in dark, with
no horizontal overflow; the marker is inside the card's shadow subtree so it
joins the `role="button"` card's accessible name. Membership rows were deleted
afterwards, so the seed is unchanged for the UI suite.

---

## 3. Members API and the Members tab

- Routes inside the existing `/api/trips/{tripId}` group:
  - `GET /members` — viewer. Everyone on a trip may see who else is on it.
  - `POST /members` `{username, role}` — owner. Resolves through the existing
    `GetUserByUsername`; an unknown username answers 404 with a *distinct*
    error code so the form can say "no such user" instead of the generic
    failure. Adding the owner, or someone already present, is a 409.
  - `PUT /members/{userId}` `{role}` — owner.
  - `DELETE /members/{userId}` — owner, **or** the caller removing themselves,
    which is "leave trip".
- New tab `{ key: "members", icon: "users", overflow: true }` in
  `web/js/trip-tabs.js:24`. Six tabs is already the 324px ceiling that file's
  header comment describes, so a seventh must be `overflow`.
- New `web/js/pages/members-tab.js`, modelled on `settings-tab.js:18` (card +
  slot + `translatePage` + an inner `render()` closure). Rows carry initials,
  display name, `@username`, and a `renderMenu` overflow holding a role radio
  group plus a danger Remove — exactly the radio-and-action mix
  `components/menu.js:33-35` documents. The owner's row is inert. A non-owner
  sees the list plus a "Leave trip" button and no per-row controls.
- Icons `users` and `user-plus`: add to `ICONS` in
  `scripts/gen_icon_sprite.py` and regenerate the sprite per `CLAUDE.md`,
  diffing to confirm the existing symbols come out byte-identical.
- i18n: `members.*` and `trip.tabs.members` in `en.json` **and** `de.json`.
- Seeder (`cmd/seed/main.go`): add `other` as an **editor** on the `full`
  scenario trip and as a **viewer** on `one-pin`. `seedCtx` already carries
  `otherID` (`main.go:81`), so this is a couple of lines — and it is what gives
  the UI suite something to assert against for the rest of the stage.

**Verify.** Go tests per route including leave-trip and both 409s. Playwright at
324×756 in both locales: add, change role, remove, and the tab's own tap
targets.

**Done.** Landed as planned. Four things worth recording.

*Error codes, not error messages.* `POST /members` can fail four ways the form
must word differently, two of them sharing a 409, so `writeErrorCode` (new,
beside `writeError`) adds a stable machine-readable `code` alongside the human
message: `user_not_found`, `already_owner`, `already_member`. The client
branches on the code and never on the prose. Recorded in `members.go`: the 404
for an unknown username *is* a deliberate disclosure — the caller learns whether
a username exists here — and it is the price of add-by-username, since otherwise
a typo and a real refusal are indistinguishable. It reveals nothing that
attempting a registration would not already reveal via `ErrUsernameTaken`.

*POST and PUT are kept distinct even though the store call is an upsert.* Adding
someone who is already on the trip is a 409 rather than a silent role change,
and a PUT against a non-member is a 404 rather than a silent add. Both needed an
explicit `GetTripMember` first, because `UpsertTripMember` would happily have
performed the other operation — which is convenient for the store and wrong for
the API.

*Remove is the one route that cannot use `loadTrip`'s minimum role.* The owner
may remove anyone; a member may remove exactly themselves ("leave trip"). So it
loads at `RoleViewer` and spells out the two cases. Removing the owner is a 409
with code `owner_cannot_leave` rather than the accidental 404 it would otherwise
be — the owner has no membership row to delete — because it is also the request
that would leave a trip with no owner if it ever worked.

*One deviation:* `confirmDialog` gained a `message` option. `open()` already
supported a ready-made interpolated string "for the rare caller that has to
interpolate" and `alertDialog` already exposed it; `confirmDialog` simply never
passed it through, and "Remove Anna from this trip?" has to name Anna. No new
mechanism, one line.

Everything else as planned: the `members` tab (necessarily `overflow: true` — a
seventh tab has no chance in a phone's row), `members-tab.js` in two shapes
decided by `canManageMembers` (owner: add form plus a per-row ⋮ holding the role
radio group and a danger Remove; everyone else: the same list read-only plus
Leave on their own row), `users` and `user-plus` added to the sprite, 24 new keys
in both locales, and the seeder putting `other` on `full` as editor and `one-pin`
as viewer. The UI-suite tab lists in `scenarios.js` and `menu.spec.js` were
updated to match.

**Verified.** `make ci` green (201 keys in sync); all Playwright specs pass,
including `menu.spec.js` in both locales now that the More menu has three items.
Six new Go tests cover the owner being synthesized into the list and coming
first, list visibility across all four caller kinds, add-by-username including
trimming, all eight add refusals with their codes, role changes proved by a
viewer's 403 becoming an editor's 201 and back again, remove-vs-leave including
that a member cannot remove someone else, and that the owner cannot be removed.

Break-checked, six breaks, all caught — and two of them had to be rewritten
first. `if false {` orphaned variables and made the package fail to *compile*,
which `todo.md` warns reads exactly like a test failure; rewritten as
`if false && <original> {` so the guard is still evaluated and the break is a
real one. Breaks confirmed: any member removing anyone, the already-member check
skipped, PUT-becomes-add, the owner being removable, role validation dropped,
and the owner omitted from the list.

Live at 324×756 and 1280: as owner, the owner row is inert and the member row
has its ⋮; all four add-form failures show their own message (including a padded
`  demo  ` resolving to "demo owns this trip", so the trim matches the server's);
demoting through the radio group persisted. As `other`, the tab is read-only with
no add form, no ⋮, and Leave only on their own row at exactly 44×44; the German
More menu reads Dateien / Mitreisende / Einstellungen with no tab-bar overflow;
leaving showed the interpolated German confirmation, landed on `/trips`, and the
trip answered 404 afterwards.

*Found and fixed during that pass:* the Leave confirmation's button said
"Löschen" — `confirmDialog` defaults `confirmKey` to `common.delete`, which is
right for "delete this trip" and misleading for an action that removes no data.
Both confirmations now pass their own short label (`members.confirmLeave` /
`members.confirmRemove`), verified as "Verlassen" at 44px with no overflow. The
dialog still shows a trash icon for Leave, which `confirmDialog` hardcodes
whenever `danger` is set; noted in `todo.md` rather than widening that API here.

The seeded memberships the manual pass consumed were restored through the API,
so `make dev-reset` state is intact.

### 3a. Milestone 3 follow-up: three pieces of review feedback

Raised on looking at the shipped tab, all three fixed before Milestone 4.

**The role was shown twice per row** — once as `.member-card__role` text and
again as the ⋮ button's label, which sat right beside it reading "Editor
Editor". Cause: `renderMenu` falls back to the *selected item's* label when
`label` is nullish, and this menu has an `activeValue`. Fixed by passing
`label: ""`, which pins the trigger to no text, plus a `member-card__trigger`
class for the bare-⋮ treatment. Decided with the user in preference to dropping
the text: the role then sits in the same place for an owner and for a viewer
(who has no menu at all), so the column lines up in both shapes, and a per-row
⋮ with no label is what the Files row already is. The current role is still
marked inside the open menu by `aria-checked` and the check mark.

**The username and role fields didn't look like any other input in the app** —
they were browser defaults, because `.members-add__field` styled the layout and
nothing styled the controls. Fixed by joining the canonical
`.trip-form`/`.password-form` label and input rules rather than adding a fourth
copy of them, which is the same consolidation this stylesheet's error-callout
comment records doing for five error paragraphs. `.members-add__field` now
carries only its own sizing. Worth noting the app still has three near-identical
input rules (`.auth-form`, `.trip-form`/`.password-form`, `.item-form`) with
small drift between them — `.auth-form` uses `var(--radius)` and `font-size:
1rem` where the others use `0.375rem` and `font: inherit`. Not touched here;
consolidating all of them is its own change.

**Username autocomplete.** New `GET /api/users/search?q=`, min 2 characters,
capped at 10, behind `RequireAuth`. Scope decided with the user: any
authenticated caller searches every account on the instance. That is a real
widening — usernames become enumerable by walking prefixes rather than one guess
at a time — accepted knowingly for a self-hosted instance whose users know each
other. Bounded by returning only username and display name (no id, no email —
`memberSuggestion` and `db.UserSummary` both exist to make that explicit), by
the cap, and by the 2-character floor. The field uses a native `<datalist>`
rather than a hand-rolled combobox, so keyboard, screen-reader and mobile
behaviour come from the browser; requests are debounced 200ms, guarded against
out-of-order responses, silent on failure (the field works fully when typed
out), and people already on the trip are filtered client-side from the already
loaded member list.

**Three tooling findings, all now recorded in `queries/users.sql`:**

1. sqlc's sqlite grammar rejects `LOWER(a) LIKE @p OR LOWER(b) LIKE @p` and
   accepts the same thing with each comparison parenthesised. The parentheses
   are load-bearing for the generator, not the SQL.
2. It also rejects `LIKE ... ESCAPE`. So `%` and `_` deliberately pass through
   as wildcards: escaping *without* an ESCAPE clause works in postgres
   (backslash is its default) and silently does not in sqlite (no default), and
   one documented behaviour beats two dialects disagreeing. Nothing is at risk —
   the query is parameterised, and the widest a lone `%` reaches is the same
   first ten rows any two-letter query returns.
3. **Do not put backticks in a comment in that file.** sqlc's sqlite lexer
   reads them as identifier quotes even inside a `--` comment, swallows the line
   boundary, and reports a syntax error pointing at the SQL *below* the comment.
   This cost the most time of anything in the milestone, because the error
   points at correct code.

**Verified.** `make ci` green; full Playwright suite passing. `TestSearchUsers`
covers prefix and substring matching, the 2-character floor returning an empty
list rather than a 400, the result cap, that the payload has exactly two fields,
and that anonymous callers get 401. Break-checked: removing the floor, removing
the cap and switching substring to prefix each fail it.

**One break did *not* fail it, and that is the interesting result.** Dropping the
lowercasing in `likeContains` entirely leaves the suite green, because sqlite's
`LIKE` is already case-insensitive for ASCII — the normalisation only does
anything on postgres, which nothing here ever runs. Rather than leave an
assertion that looks like coverage, the test now says so in a comment and
`todo.md`'s postgres entry cites it as the first *measured* example of what that
gap costs.

Live at 1280 and 324×756: role appears exactly once per row, the ⋮ carries no
label and an accessible name of "Member actions"; the members input, select and
label have computed styles byte-identical to the trip form's; typing suggests
`anna`/`annabel` at two characters, nothing at one, matches `klein` by display
name, matches case-insensitively, and omits `other` who is already on the trip;
adding from a suggestion worked, and at phone width the ⋮ is 44×44, the fields
44 tall and full width, and the role wraps below the name with no overflow. The
three accounts registered for the search test were deleted afterwards, leaving
`demo` and `other` and the two seeded memberships exactly as `make dev-reset`
produces them.

---

## 4. Read-only mode for viewers

The milestone that makes `viewer` real rather than a 403 generator. Gate each
surface on `canEdit(trip)` / `canManageMembers(trip)`:

- `locations-tab.js` — hide "New location" and the cards' edit affordances.
- `location-view-page.js` — hide Edit; `location-editor-page.js` sends a
  viewer back to the view page rather than rendering a form that cannot save.
- `itinerary-tab.js` — hide add-entry dropdowns, day-notes editing, entry
  deletion.
- `file-list.js` — nothing new to build: the component already has a
  documented `readOnly` mode (`components/file-list.js:9-34`, used by the
  location view) that suppresses the row menus and the drop zone. Pass it
  through from the trip Files tab.
- `checklist-list.js` — hide both create forms and the delete button, and
  disable the checkboxes (a viewer ticking a shared list is a write).
- `settings-tab.js` — an editor may edit the trip; only the owner may delete
  it, so these two split rather than moving together.
- `trip-detail-page.js` — a small "Viewer" indication in the header, so the
  absence of buttons reads as deliberate rather than broken.

**Verify.** Playwright as `other` on the `one-pin` trip (viewer), asserting DOM
*counts* rather than screenshots: zero `[data-action="new-item"]`, zero
`.file-drop`, zero `.checklist-new-form`, checkboxes `disabled`. The `full`
trip (editor) is the negative control in the same spec — a read-only assertion
that would also pass on a broken editor view proves nothing.

---

## 5. Admin flag, and registration that closes itself

- Migration `0008`: `users.is_admin` (default false) and
  `app_settings(key TEXT PRIMARY KEY, value TEXT NOT NULL)` seeded with
  `open_signup = 'false'`.
- `auth.Register` (`internal/auth/auth.go:50`) sets `is_admin` when the users
  table is empty — the first account bootstraps the instance's administrator,
  inside the `WithTx` that already creates the user and its identity together.
- `handleRegister` (`internal/httpapi/auth.go:53`) allows registration when
  the `open_signup` setting is true **or** no users exist yet.
- **`CARAVEL_OPEN_SIGNUP`, `config.OpenSignup` and `Server.OpenSignup` are
  removed.** The database setting is the single source of truth; two sources
  for one answer is the class of bug this milestone exists to prevent, and an
  env var that silently overrides what the admin screen shows would be exactly
  that.
- Since `NewServer`'s signature is changing anyway, take the refactor `todo.md`
  asks for: replace the (now seven) positional parameters with an options
  struct, updating `cmd/caravel/main.go:45` and
  `internal/httpapi/testing_test.go:73`. `NewServer(..., false, true, "")`
  says nothing about which flag is which. Then delete that `todo.md` entry.
- New unauthenticated `GET /api/auth/config` → `{"open_signup": bool}`, so
  `login-page.js:51-53` stops offering a register toggle that 403s. Deliberately
  *not* rate-limited: it is one boolean fetched on page load, and the login
  limiter's 10/min/IP would break reloads.
- `is_admin` on `userResponse` (`internal/httpapi/auth.go:13`). The `geocoding`
  field there is the precedent for a capability riding along on `/auth/me`
  rather than earning its own endpoint.

**Verify.** Go tests against a fresh database: the first register succeeds and
yields an admin, the second 403s, flipping the setting lets it through.
Playwright: the register toggle is absent on a closed instance. Backend
milestone — `make dev-restart` before testing by hand, since a stale binary
reads exactly like a missing feature.

---

## 6. Admin user management

- `/api/admin/*` behind `RequireAuth` plus a new `requireAdmin` middleware:
  - `GET /users` — list, with each user's trip count.
  - `POST /users` — create (username, display name, password, admin flag).
  - `PATCH /users/{id}` — display name, admin flag.
  - `POST /users/{id}/password` — reset. `auth.SetPassword`
    (`internal/auth/auth.go:191`) is already exactly this primitive — it skips
    the current-password check and keeps sessions — and has been reachable only
    from the seeder until now.
  - `DELETE /users/{id}`.
  - `PUT /api/admin/settings/open-signup`.
  Needs `ListUsers` / `UpdateUser` / `DeleteUser` / `CountUsers` in
  `queries/users.sql`, which today has only a create and two lookups.
- Guard rails, each with a test: an admin cannot clear their own admin flag or
  delete themselves while they are the last admin; deleting a user cascades
  their trips through the existing FK, so the confirm dialog must say so
  plainly, naming the trip count.
- `web/js/pages/admin-page.js` at route `/admin`, registered beside `/settings`
  in `app.js:36`, following the settings-page card-and-slot idiom with the
  `.back-link` treatment (it sits outside the trip tab bar). Entry point: a
  third `action: true` item in `components/user-menu.js:32-35`, rendered only
  when `getCurrentUser().is_admin`.
- Icon `shield-user`; `admin.*` in both locales.

**Verify.** Go tests for every route including the guard rails and a
non-admin's 403. Playwright: the menu item is absent for a non-admin, and
create → reset password → delete drives end to end. Isolate the way
`tests/ui/settings.spec.js` does — it already solves the problem that changing
a password destroys the shared session, and its lesson that a *silently*
failing cleanup poisons every later run applies directly here.

---

## 7. File visibility

- Migration `0009`: `files.owner_user_id` (backfilled to the trip owner) and
  `files.visibility` (`'personal' | 'trip'`, default `'trip'`).
- `ListTripFiles` and `ListItemFiles` gain
  `AND (visibility = 'trip' OR owner_user_id = sqlc.arg(user_id))`, and the
  same predicate guards `loadFile`, so a direct download of someone else's
  personal file 404s rather than merely being absent from a list.
  `uploadFile` (`files.go:101`) stamps the uploader and the requested
  visibility; `UpdateFileNote` grows a visibility change, restricted to the
  file's own owner.
- `file-list.js`: `visibility` into the row view model (`:114-116`, the single
  place staged-`File` and API rows are normalised), a lock badge in `metaTail`
  (`:145`), a radio group in the existing per-row menu (`:187-190`), and a
  selector on the drop zone whose value rides along on the multipart POST
  (`:292-297`) — the whole control hidden when the trip has no members. Staged
  uploads on a not-yet-created location carry `visibility` alongside
  `{file, note}`.
- Decide and document member-removal semantics: removing a member **deletes
  their personal files on that trip, blobs included**. Bytes that nobody can
  ever reach again are worse than a confirm dialog that names the count, so the
  Members tab's removal dialog says exactly what will go.

**Verify.** Go tests — A's personal file is invisible to B in both list
endpoints and 404s on direct download, while a `trip` file is visible to both.
Playwright: upload as personal, confirm the badge, confirm `other` does not see
the row.

---

## 8. Checklist visibility

Same shape, three states.

- Migration `0010`: `owner_user_id` and
  `visibility ('personal' | 'trip' | 'shared')`, default `'shared'`.
- List predicate `visibility <> 'personal' OR owner_user_id = ?`, plus a tick
  guard — only the author may tick a `trip` list, anyone on the trip may tick a
  `shared` one — enforced in `handleSetChecklistItemChecked`
  (`checklists.go:187`), not merely hidden in the UI.
- Frontend: replace the bare delete icon in the checklist card header
  (`checklist-list.js:35-38`) with a `renderMenu` overflow holding the
  visibility radio group and a danger Delete. That is also the ⋮-menu the
  long-standing `todo.md` checklist entry asks for, so **fold in checklist
  renaming and in-place item editing here** rather than shipping a menu that is
  half of what that entry describes. List *duplication* stays deferred — it
  needs its own call on whether a copy keeps checked state.

**Verify.** Go tests for the tick guard across all three states; Playwright for
the menu and for a `trip`-visibility list being visible-but-not-tickable to a
second user.

---

## 9. UI suite: the multi-user flows

The suite arrives as one authenticated demo user (`tests/ui/auth.setup.js`),
which after eight milestones is the largest coverage gap in the tree.

- A second storage state for `other` in the `setup` project — one login per
  run, because `/api/auth/login` is limited to 10/min/IP.
- Extend `tests/ui/helpers/scenarios.js`: `buildRoutes` (`:152`) for `/members`
  and `/admin`, `TRIP_TABS` (`:28`), and the expected label maps in
  `menu.spec.js:26-30`.
- New `sharing.spec.js`: add → viewer sees read-only → promote to editor →
  leave trip, in both locales at 324×756.
- This also finally covers the **login and register pages**, which no spec
  renders today because the suite starts authenticated — the second-user
  context begins unauthenticated, so they come for free. Close that `todo.md`
  entry only to the extent it is actually true.

---

## 10. Sweep-up

Re-run everything: `make ci`, the full Playwright suite, `tests/ui/contrast.js`,
`scripts/i18n.py unused`. Then check the new screens against the sweeps' known
blind spot — `todo.md` records that an element only rendered when data exists is
invisible to the route sweeps until some scenario creates that data, so ask
"which scenario renders this?" of the members list, the personal-file lock
badge and the admin table.

Update `todo.md` in both directions. Delete what this stage implemented:
sharing/collaboration/permissions, per-visibility checklists, per-visibility
files, administrative tooling, the `NewServer` positional-parameters entry, and
the checklist ⋮-menu entry (minus duplication). Add what it deferred: public
share links (rewritten to reference the shipped role model), expenses, trip
ownership transfer, invite-link joining, checklist duplication, and the
file-visibility default that is worth re-examining in use.

---

## Build order

1 → 2 → 3 → 4, which is roles end to end and usable. Then 5 → 6, the
administrative half, which is independent of the role work. Then 7 → 8, the
visibility pair, which needs roles to mean anything. Then 9 → 10.

Milestones 5–6 could go first — nothing in them depends on roles — but roles
are the risky half and the half everything else reacts to, so they go while
there is the most appetite for reworking them.

## Workflow

Per `CLAUDE.md`: implement → verify (`make ci` green **plus** a
Playwright/manual pass proving behaviour changed, assertions preferred over
screenshots) → update `docs/plans/stage-14.md` and `docs/plans/todo.md` →
commit → leave `make dev` running → **stop and wait** for the go-ahead before
the next milestone.

Milestones 1, 2, 5, 6, 7 and 8 are backend changes: `make dev-restart` before
testing them by hand. Milestones 1, 5, 7 and 8 add migrations, which run on
startup — so a restart is also what applies them.

## Verification (stage level)

- `make ci` green before every commit: build, vet, JS syntax, i18n key parity
  across `en.json` + `de.json`, `go test ./...`.
- The Go role matrix in `ownership_test.go` is the load-bearing test of the
  stage. Every route added from Milestone 3 on gets a row for stranger /
  viewer / editor / owner / admin.
- Playwright against a running `make dev` with `make dev-reset` seed data,
  asserting DOM counts, disabled states and accessible names rather than
  regenerating screenshots.
- A manual pass at 324×756 in both locales: as `demo`, share the `full` trip
  with `other` as viewer; as `other` in a second browser profile, confirm the
  trip is genuinely read-only; then as admin create a third user, reset its
  password, and delete it.
- Every migration must at least survive `sqlc generate` in both dialects. Note
  the standing gap that **nothing ever runs the Postgres dialect** — four new
  migrations is the largest schema change since `0001`, which makes this stage
  the strongest argument yet for the Postgres CI job `todo.md` describes.
  Milestone 10 should restate that entry rather than let it slide, because
  "compiles" is the only evidence this stage will have produced for half its
  schema.

## Out of scope

- **Public shareable links** for unauthenticated viewers — a token model, not
  a role model.
- **Expenses / cost-splitting** — depends on multi-user, isn't part of it.
- **Trip ownership transfer** — needs a call on what happens to the old
  owner's row and their personal files.
- **Invite links / joining by token** — overlaps the share-link design;
  add-by-username covers the self-hosted case.
- **Checklist duplication** — the ⋮-menu it was waiting on lands in Milestone
  8, but whether a copy keeps checked state is still an open question.
- **Per-visibility media assets** (location and trip cover images). A cover
  photo is inherently trip-wide, so "personal" has no meaning there.
