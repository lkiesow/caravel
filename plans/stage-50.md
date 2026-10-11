# Stage 50: Link back to the project (About dialog and login footer)

## Context

Nothing in the running app points to where Caravel comes from. There is no
link to the website and documentation (https://lkiesow.github.io/caravel/) or
to the repository (https://github.com/lkiesow/caravel). The UI also never
shows the running version, although the binary knows it:
`internal/buildinfo.Version` is stamped by the Makefile and the Dockerfile but
appears only in `/api/health`, the startup log, metrics and outgoing
User-Agents. A bug report today cannot say which build it is about.

Decided on 2026-10-11:
- An **About Caravel** entry in the user menu opens a dialog with:
  - the tagline;
  - the running version, linked to its GitHub release (tag builds) or commit
    (SHA builds);
  - links to the website and docs, GitHub, and "Report a problem".
- A **footer on the login and registration screens** carries only the links
  (Documentation, GitHub), each with a generic Lucide icon. It has no name and
  no slogan, because the hero above already shows both.
- The version reaches the frontend through **`/api/auth/me`**. Only logged-in
  users see it: a public version number makes unpatched instances easy to
  spot.
- Lucide no longer ships brand icons, so GitHub is shown as `code` and the
  docs as `book-open`.

## 0. Land the plan

Commit this as `plans/stage-50.md`, on its own.

## 1. Version in `/api/auth/me`

- **`internal/buildinfo/buildinfo.go`**: add `URL() string`.
  - A semver-looking version (`^\d+\.\d+\.\d+`) gives `…/releases/tag/v<version>`.
    The repo has no tags yet; this assumes future tags are `v1.2.3`, which is
    what the `type=semver` metadata in `publish-docker-image.yaml` parses and strips.
  - A plain hex SHA (7–40 chars) gives `…/commit/<sha>`.
  - Anything else gives `""`: `dev`, `unknown`, `latest` and any `-dirty` build,
    because a dirty build is not the commit it names.
  - The repo URL is one const in this package.
  - Table-driven `buildinfo_test.go`.
- **`internal/httpapi/auth.go`**: add `Build buildResponse \`json:"build"\``
  to `userResponse` with `{version, url}`. It follows the reasoning of the
  existing `Capabilities` comment: this is about the server, not the user, but
  `/auth/me` is the one call made at boot.
  - Extend an existing `/auth/me` test (pattern: `health_test.go:18`
    `TestHealthReportsStatusAndVersion`) to assert both fields.
- **CI version check** (`.github/workflows/publish-docker-image.yaml:75`):
  `VERSION=${{ steps.meta.outputs.version }}`. On a push to main this is
  probably `latest` (the raw tag outranks the sha tag), and the About dialog
  would then show "Version latest".
  - Confirm against the metadata-action docs.
  - If confirmed, replace it with a small step that outputs `${GITHUB_REF_NAME#v}`
    on tag pushes and `scripts/version.sh` otherwise.
  - This can only be fully verified on the next real CI run; say so in the commit.

**Done.** `/api/auth/me` now carries `"build": {"version", "url"}`.
- **`buildinfo.URL()`**:
  - A release (`1.4.0`, also `1.4.0-rc.1`) links to `releases/tag/v…`.
  - A bare 7–40 hex SHA links to `commit/…`.
  - Everything else gets `""`: `dev`, `unknown`, `latest`, `-dirty`.
  - `RepoURL` is an exported const in the same package.
- **CI workflow**: the metadata-action docs confirm the suspicion. `version` is
  the highest-priority tag, which on main is the raw `latest` (200) and not the
  sha tag (100). A new "Determine the version" step now passes
  `${GITHUB_REF_NAME#v}` on tag pushes and `scripts/version.sh` otherwise, so
  main images report their short SHA.
- **Deviation from the Context section**: keeping the version behind login does
  not hide it. `/api/health` has been unauthenticated and has returned it since
  Stage 10, which was a deliberate choice (see `handleHealth`).
  `/auth/me` is still the right place, because it is the call the app already
  makes at boot.

Verified:
- `buildinfo_test.go`: table test over release, rc, short/long SHA, dirty, dev,
  unknown, latest and too-short.
- `TestAuthMeReportsBuild` (`health_test.go`): sets the version to `1.4.0`, so
  an unwired URL cannot pass as `"" == ""`. It fails with `URL:` empty when the
  field is left out.
- The workflow step was dry-run locally for both ref types: `v1.4.0` gives
  `1.4.0`, and `main` gives the SHA. The real check is the next push to main.
- Against `make dev`, logged in as demo, `/api/auth/me` returns
  `{"version":"bc3d84e-dirty","url":""}`, and the endpoint answers 401 without
  a session.
- `make ci` green.

## 2. About Caravel dialog

- **Icons:** add `book-open`, `code` and `bug` to `ICONS` in
  `scripts/gen_icon_sprite.py`, then regenerate the sprite as CLAUDE.md
  describes. The existing symbols must come out byte-identical (diff before
  committing). The footer in milestone 3 reuses `book-open` and `code`.
- **`web/js/project-links.js`** (new, pure): `WEBSITE_URL`, `REPO_URL`,
  `ISSUES_URL` (`…/issues/new`). One source for the dialog and the footer.
- **`web/js/components/dialog.js`**: add an optional `content` (DOM node) to
  the private `open()`, appended after the message. Also add a small export
  `contentDialog({ messageKey, message, content })` with a single Close button,
  modelled on `alertDialog` (line 105). The message `<p>` stays the accessible
  name through `aria-labelledby`. Back/Escape handling comes for free.
- **`web/js/components/about-dialog.js`** (new): `openAboutDialog()` builds the content:
  - the `app-brand` lockup (brand-mark + wordmark, as in `login-page.js:43-46`)
  - the tagline, reusing `auth.hero.title` ("Every trip, one place.") and `auth.hero.subtitle`
  - `about.version` ("Version {version}"). When `build.url` is set, the version
    is a link opening in a new tab.
  - a link list with icons:
    - Website & documentation (`book-open`)
    - Source code on GitHub (`code`)
    - Report a problem (`bug`)
  - All links use `target="_blank" rel="noopener noreferrer"`, built with
    `textContent` and DOM APIs, as `assist-run.js:201` does.
  - The version comes from `getCurrentUser().build` (`web/js/session.js`).
    No extra request.
- **`web/js/components/user-menu.js`**: add an `about` entry (`about.menuItem`,
  icon `info`) between Administration and Log out. `onSelect` returns
  `openAboutDialog()`, so menu.js stays busy while the dialog is open.
- **CSS** (`web/css/base.css`, near `.dialog` ~1207):
  - `.about-dialog__*` for the lockup, tagline, muted version line and link list
  - it must fit at 324px wide
  - tokens only, so dark mode follows
- **i18n**: new `about.*` keys in `en.json` and `de.json`.
- **Tests**:
  - Update `USER_MENU_LABELS` in `tests/ui/menu.spec.js:375`.
  - New `tests/ui/about.spec.js`:
    - the dialog opens from the menu and its accessible name is "About Caravel"
    - the version text equals `/api/auth/me` `build.version`
    - the version link is present exactly when `build.url` is set
    - the three link hrefs, `target` and `rel`
    - Escape closes it
    - no horizontal overflow at 324×756
    - both locales

## 3. Login/registration footer

- **`web/js/pages/login-page.js`**: add a `<footer class="auth-footer">` after
  `.auth-hero`, inside `.auth-screen` (line 34ff), with two links:
  Documentation (`book-open`) and GitHub (`code`). They use the URLs from
  `project-links.js`, open in a new tab, and carry the same `rel`. It sits
  outside the `mode` conditional, so it shows on both login and registration.
- **CSS**: `.auth-footer`, small and muted. Use `--color-text-muted`-style
  tokens and keep the link colour readable in both themes; the
  `make check-contrast` pass covers it.
- **i18n**: `auth.footer.docs` and `auth.footer.github`. The links get an
  `aria-label` hint that they open in a new tab only if that pattern already
  exists elsewhere; otherwise they don't.
- **Tests**:
  - extend `tests/ui/unauthenticated.spec.js` so the footer links are present,
    with correct hrefs, in both modes, both locales and both viewports, with no overflow
  - make sure `headings.spec.js` and `brand.spec.js` still pass, since the footer adds no heading

## Build order

0 → 1 → 2 → 3. Milestone 2 depends on 1's `build` field. Milestone 3 depends only
on 2's icons and `project-links.js`.

## Workflow

The CLAUDE.md milestone loop applies to each milestone:
1. Implement.
2. Run `make ci` until it is green, then do a Playwright/MCP pass at 324×756
   and on desktop against `make dev`.
3. Add a **Done.** paragraph to `plans/stage-50.md` and update `plans/todo.md`.
4. Make one commit per milestone.
5. Make sure `make dev` is running, then stop and wait.

## Verification

- `make ci` green after every milestone. Milestone 1 also needs
  `go test ./internal/buildinfo ./internal/httpapi`.
- `make test-ui` (bundled build) for `menu`, `about`, `unauthenticated`,
  `headings` and `brand`.
- `make check-contrast` after milestones 2 and 3, covering dark and light themes.
- By hand via Playwright MCP against `make dev`:
  - open the user menu → About and check that the version shows the dev SHA
    (`-dirty` builds unlinked)
  - log out and check the footer on login and registration at 324×756
- Optionally `make screenshots` if the login screenshot in `docs/` should show
  the footer; then run `make docs`.
