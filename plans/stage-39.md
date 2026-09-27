# Stage 39: `@` links a location from the trip notepad

## Context

The trip notepad (`/trips/:id/notes`, Stage 31) is a plain markdown textarea. A
trip's places already live one route away at `/trips/:tripId/locations/:itemId`,
but nothing connects the two: writing "we ate at Kex" in a note is prose, not a
link, and the only way to make it one is to open the location page in another
tab, copy the URL, and hand-write the markdown.

This stage makes typing `@` in the notepad open a dropdown of the trip's
locations; picking one inserts a normal markdown link at the caret. It also
fixes a smaller existing defect it would otherwise make very visible: internal
links in rendered notes currently cause a **full page reload**, because the SPA
router only intercepts anchors carrying `data-link` and server-rendered
markdown HTML carries none.

### Decisions already taken

- **Storage is a plain markdown link** — `[Kex Hostel](/trips/T/locations/I)` —
  not a `[[loc:id]]` reference resolved at render time. The source stays
  portable markdown, `internal/markdown` is untouched, and the sanitization
  boundary does not move. `bluemonday.UGCPolicy()` allows relative URLs
  (`AllowStandardURLs` → `AllowRelativeURLs(true)`), so these survive
  sanitisation today. The cost is that a later rename leaves stale link text;
  that goes to `plans/todo.md`, not into this stage.
- **Trip notepad only.** The location form's notes field
  (`web/js/components/location-form.js`) gets the same treatment later if it
  proves useful — the component is built so wiring it in is a few lines.
  Itinerary day notes are plain text, not markdown, and are out of scope.
- **`@` means locations.** No member mentions, no `#tag` trigger. The trigger
  character is not parameterised; adding a second trigger later is a small
  change to one regex-owning module.

## Why a stage rather than a single patch

Three genuinely separate concerns, each independently revertible: a change to a
shared component two other features depend on; a new caret-geometry module; and
an unrelated router fix. The first one in particular wants its own checkpoint —
`suggest-input.js` backs the tag field and the member picker, and a regression
there is silent.

---

## Milestone 1 — `bindSuggestInput` learns to read part of a field

`web/js/components/suggest-input.js` is the app's one combobox and gets
generalized rather than copied: the ~180 lines that are not the two hard-coded
expressions are exactly the parts that are painful to get right (ARIA
`aria-activedescendant`, the touch `pointerdown` preventDefault, the `seq` race
guard, listener tear-down). A parallel `mention-input.js` would duplicate all of
it and the two would drift — the module's own header comment already makes this
argument against `menu.js`.

Two whole-field assumptions are hard-coded today: `input.value.trim()` as the
query (line 137, and again in the race guard at line 156) and `input.value =
item.value` on pick (line 123). Both become defaulted options:

```js
bindSuggestInput(input, listEl, {
  search, minChars = 2, delay = 200, onPick,
  readQuery,          // (el) => { query, start, end } | null
  applyPick,          // (el, item, range) => void
  autoActivateFirst,  // default false
  onOpen,             // (el, listEl) => void, after the list is visible
})
```

with the current behavior as the default for the first two, so `tag-field.js:134`
and `members-tab.js:249` are behaviourally byte-identical.

Changes inside the module:

- New `range` state, cleared by `close()`, captured when results land, read in
  `pick()` before `close()` empties it.
- `pick()` reorders to **`close()` before `applyPick`**. This is load-bearing:
  the mention `applyPick` fires a real `input` event, which would otherwise
  re-enter the input handler with `items`/`range` still live.
- `lastQuery` becomes position-keyed (`` `${start}:${query}` ``). Two `@kex` runs
  in one note are two different questions; the text-only dedupe silences the
  second.
- The race guard re-reads via `readQuery` instead of re-comparing `input.value`,
  because the caret may have moved while the lookup was out and the range being
  applied into must be current.
- Composition guards: skip the `input` pass while `composing`, re-dispatch on
  `compositionend`, and bail out of `keydown` on `event.isComposing ||
  event.keyCode === 229`. This is a **fix for the two existing callers too** —
  today Enter committing an IME candidate in the member field picks a
  suggestion.
- When `readQuery` is supplied, `open()` also attaches a `selectionchange`
  listener that closes the list if the caret leaves the run; gated so existing
  callers attach exactly the listeners they attach today.
- The header comment's contract paragraph (lines 23-31) becomes wrong and is
  rewritten.

**Verify:** `make ci`, plus the existing Playwright specs that exercise the two
callers green unchanged — `tests/ui/sharing.spec.js` (member picker,
`.suggest__option` at ~line 211) and `tests/ui/locations.spec.js` (tag field).
No new behavior is visible to a user in this milestone; that is the point.

**Done.** `bindSuggestInput` now takes an options object rather than a
destructured signature, so `readQuery`/`applyPick` can be detected as *supplied*
(`partial`) instead of merely defaulted -- that flag is what gates the new
`selectionchange` listener, keeping the two whole-field callers on exactly the
listener set they had. Landed as planned: `range` state cleared by `close()`,
`close()` before `applyPick` in `pick()`, position-keyed `lastQuery`, a
`readQuery` re-read in the race guard, `autoActivateFirst`, and an `onOpen` hook
that fires on *every* batch of results rather than only on the
hidden-to-visible transition, since a new batch is a new height for whoever is
positioning the list. Composition guards went in too
(`compositionstart`/`compositionend` plus `isComposing || keyCode === 229` in
`keydown`), which fixes the pre-existing bug where Enter committing an IME
candidate in the member picker also picked a suggestion. Header comment
rewritten, since it documented whole-field semantics as the contract.

Verified: `make ci` green, and `make test-ui GREP="sharing|locations|notes"` --
63 specs, all passing, covering both existing callers (the member picker's
"suggests users to add, by keyboard and by tap" in en and de, and the tag field
through the locations suite). No user-visible change in this milestone, which
was the point.

---

## Milestone 2 — the `@` picker in the notepad

**New `web/js/components/caret-coords.js`** (~45 lines) — `caretCoords(el)`
returns `{ top, left, lineHeight }` for the caret, via the mirror-div technique
(render `value.slice(0, selectionStart)` into a hidden div with the field's
computed styles, end it with a `​` span, ask the span where it landed).
Anchoring the list under the field the way `.suggest__list { top: 100% }` does
is not an option here: these textareas auto-grow to full content height, so a
40-line note puts the popup ~900px below the word being typed.

Gotchas, each handled: `box-sizing` + all paddings and border widths copied,
borders added back into the offsets (the same arithmetic both existing
`autoGrow` functions do); `- el.scrollTop` because `resize: vertical` means a
dragged-short box can scroll; `white-space: pre-wrap` + `overflow-wrap:
break-word` + copied `width` so wrapping matches; styles re-read on every open,
never cached, because the app serves a subsetted Montserrat and a measurement
taken before the webfont lands would be wrong for the rest of the session;
`parseFloat("normal")` is `NaN`, so `line-height` falls back to `1.2 ×
font-size`.

Only the **vertical** offset is computed. The list keeps `left: 0; right: 0`
(full field width), which is what it wants at 324px anyway and which is also why
there is no RTL horizontal case to get wrong. On `window.resize` /
`orientationchange` the list **closes** rather than repositioning — Android's
soft keyboard fires resize, and a popup chasing a moving viewport looks broken.

**New `web/js/components/mention-picker.js`** (~90 lines) —
`bindMentionPicker(textarea, listEl, { tripId })` → `{ destroy, close }`,
composing `bindSuggestInput` and `caretCoords`. It owns:

- The trigger rule — `@` only at start-of-text or after whitespace (so an email
  address in a note never opens it), caret collapsed, no `@` or newline in the
  run, at most two interior spaces (location titles are phrases: "Blue Lagoon"
  has to be reachable; a whole sentence after a stray `@` is not a query).
  Backspacing past the `@`, a third space, or no matches all close it.
- Candidates from `GET /trips/{id}/items`, lazily on first focus into one
  memoized promise, failures silent and empty — the same shape as
  `tag-field.js`'s `loadVocabulary()` (line 119). No new endpoint: this is the
  same client-side title match `locations-tab.js:116` `matches()` already does.
  Prefix hits sort first, capped at 8, `hint` is the translated category.
  `minChars: 1, delay: 0`, as the tag field does for local data.
- `autoActivateFirst: true`. Enter picks whenever the list is open. This is the
  one debatable call — someone typing `@home` as prose and pressing Enter gets a
  link — but the alternative costs mobile users the keyboard path entirely,
  since there is no ArrowDown on a phone keyboard, and Escape / a space / a
  backspace are all one keystroke away. No form-submit conflict exists: Enter in
  a textarea never triggers implicit submission.
- Markdown-safe insertion: the link text is whatever somebody typed, so `[`,
  `]` and `\` get backslash-escaped or the link breaks into literal text. The
  URL half is app-generated. Insertion prefers `document.execCommand("insertText")`,
  the only way to write into a textarea that keeps the browser's undo stack and
  fires a real `input` event, falling back to a `value` splice plus a dispatched
  `input` event. That event is required, not cosmetic: `notes-tab.js`'s handler
  keeps `draft` and runs `autoGrow`, so without it a picked link is lost on the
  next re-render.

**`web/js/pages/notes-tab.js`** — wrap the textarea in `<div class="suggest
suggest--caret">` with a sibling `<ul class="suggest__list" role="listbox"
hidden>`. `.trip-notes__form textarea` is a descendant selector, so the wrapper
changes no styling. Bind in `bindEditor()`. Add a module-scoped `picker` and
`picker?.destroy()` as the first statement of `render()` — that function
re-renders on every save and cancel and has no tear-down at all today, so
without it each save leaks a document listener.

**`web/css/base.css`** (~line 3968) — one `.suggest--caret .suggest__list` rule;
`top` is set inline per open, flipping above the caret line when the popup would
run off the bottom of the visual viewport.

**`web/locales/{en,de}.json`** — `tripNotes.placeholder` gains a mention of
`@`. Key parity is enforced by `scripts/check_i18n.py` in `make ci`.

**New `tests/ui/mentions.spec.js`** — `pressSequentially("@kex")` opens with the
right options; Enter inserts exactly `[Kex Hostel](/trips/<id>/locations/<id>)`;
a location seeded as `Museum [north]` inserts `\[north\]` and round-trips
through `POST /api/markdown/preview` as one anchor; Escape closes; a third space
closes; backspacing over the `@` closes; the popup's `boundingBox().y` is within
one line-height of the caret line on a note with 40 lines above it; and the same
at 324×756 with the popup fully inside the viewport.

**Verify:** `make ci`, the new spec, `tests/ui/notes.spec.js` still green, and a
manual pass at 324×756 against `make dev`.

---

## Milestone 3 — internal links in rendered notes stay in the app

Independent of the picker, and the reason to do it now is that the picker makes
it the common case. `web/js/router.js:101-112` intercepts only
`a[data-link]`; bluemonday would strip such an attribute from markdown anyway,
so the marking has to happen in JS after insertion, keyed on the href shape.

After each of the three `innerHTML` assignments that show note HTML —
`web/js/pages/notes-tab.js:117`, `web/js/components/location-form.js:156`
(preview), `web/js/pages/location-view-page.js:233` — mark same-app anchors:

```js
markInternalLinks(rendered); // a[href^="/"] that is not "//..." gets data-link
```

as one small exported helper (alongside `safeHref` in `web/js/url.js`, or a new
`web/js/components/rendered-notes.js` if it grows a second job). Guard against
protocol-relative `//evil.example`, and leave anchors with a target or a
modified click alone — `map-view.js:1247-1274` is the existing precedent for
letting modified clicks fall through.

Links pointing at a location additionally get a small pin icon via CSS, so a
place reads as a place rather than as a bare link. Dead links (the location was
deleted) are **not** detected — that goes to `plans/todo.md`.

**Verify:** `make ci`; a Playwright assertion that clicking a location link in a
rendered note changes `window.location.pathname` without a navigation event
(`page.on("load")` never fires), which is a stronger check than a screenshot.

---

## Build order

1. Milestone 1 — the shared component, proven by the existing specs before
   anything new depends on it.
2. Milestone 2 — caret coords, the picker, the notepad wiring, the new spec.
3. Milestone 3 — the router fix.

## Workflow

Per `CLAUDE.md`: one milestone at a time. Implement → verify (`make ci` green
plus a Playwright/manual pass proving behavior actually changed) → add a
"**Done.**" paragraph to `plans/stage-39.md` and update `plans/todo.md` in both
directions → one commit per milestone, saying what changed, why, and how it was
verified → make sure `make dev` is running → stop and hand back control. Do not
start the next milestone until told to.



## Verification (whole stage)

- `make ci` green.
- `tests/ui/mentions.spec.js` plus the untouched `notes.spec.js`,
  `sharing.spec.js`, `locations.spec.js`, `notes-preview.spec.js`.
- Manual pass against `make dev` + `make dev-seed` at both desktop width and
  324×756: type `@` mid-note, pick with the keyboard and by tap, confirm the
  popup sits next to the caret and not off-screen, save, and click the rendered
  link — it should land on the location page with no white flash.

## Deferred to `plans/todo.md`

- Renaming a location leaves stale link text in notes that reference it.
- A link to a deleted location renders as a live link that 404s.
- The same `@` picker in the location form's notes field and, if day notes ever
  become markdown, the itinerary.
- A `#` trigger for tags, and `@` for trip members, if either is ever wanted —
  note that member mentions would want `@` and so would collide.
