# Stage 41: the body text is whatever the operating system happens to have

## Context

`base.css` sets `body { font-family: system-ui, -apple-system, "Segoe UI",
sans-serif }`. Everything on the page that is not the wordmark or a hero title
— every label, every form field, every itinerary row, every expense amount — is
therefore drawn in a face the app never chose and cannot see. Segoe UI on
Windows, Roboto on Android, San Francisco on Apple, and on Linux whatever
fontconfig decides, which is Noto Sans on the developer's Fedora box and DejaVu
Sans on a GitHub runner.

Stage 40 Milestone 4 measured what that costs. The same element,
`.suggest-page__context`, came out 44px tall locally and 38px on CI, and the
German editor row overflowed its container at 324px under DejaVu and not under
Noto. Both were genuine defects rather than CI artefacts: the element had no
`min-height` at all, so its tap target came entirely from whatever default line
box the system font happened to hand it. It cleared the guideline on one
machine by nothing and missed it on any reader whose system font has a shorter
one.

That is the argument for this stage. The brand face is already self-hosted —
`scripts/gen_brand_fonts.py` subsets the OFL Montserrat, `base.css` declares
two `@font-face` blocks, `sw.js` precaches both, `web/` is embedded in the
binary and `web/fonts/OFL.txt` ships the licence. Every mechanism a UI face
needs already exists and is already proven. What is missing is the face.

### What this stage does not assume

**A web font does not by itself make the metrics deterministic.** With
`font-display: swap` — what the brand face uses, and rightly — the fallback
stack still paints the first frame, so every layout still has to survive DejaVu
and Segoe and Roboto, and every reader still sees a reflow when the real face
arrives. Shipping Inter narrows the variance to the first paint; it does not
remove it. Milestone 2 is where that is confronted rather than assumed away,
and it is deliberately a separate milestone because it is a separate decision:
Milestone 1 is worth landing even if Milestone 2 concludes that `swap` and a
tolerant layout is the right answer.

For the same reason `tests/ui/fonts.conf` stays. Stage 40 pinned DejaVu for the
suite precisely because it is the adversarial case, and after this stage the
fallback path it exercises still exists.

### Why Inter

Measured, not estimated — each candidate subset with the coverage and flags
`gen_brand_fonts.py` already uses (latin, latin-ext, the punctuation the copy
needs; `kern,liga`; woff2):

| face | subset size |
| --- | --- |
| Inter 400 / 500 / 600 | 19.7 / 20.0 / 20.2 KiB |
| Source Sans 3 400 / 600 | 21.7 / 22.0 KiB |
| IBM Plex Sans 400 / 600 | 19.7 / 20.8 KiB |
| Atkinson Hyperlegible Next 400 / 600 | 13.5 / 15.4 KiB |

Inter is drawn for user interfaces at small sizes — tall x-height, open
apertures, and a `tnum` figure set that `.expenses__row-amount` already asks
for through `font-variant-numeric: tabular-nums`. It is OFL. It is packaged as
`rsms-inter-fonts` on Fedora, so it fits the generator's existing convention of
subsetting a distribution package rather than downloading anything. And a
neutral humanist UI face under a geometric display face is a conventional
pairing that will not fight Montserrat.

The alternatives were real. IBM Plex Sans is more characterful and reads
audibly "IBM". Source Sans 3 is the safe, dull choice. Atkinson Hyperlegible
Next is the interesting one — smallest files, drawn for low vision, and a
genuine accessibility position for a travel app — but it is distinctive enough
to be a brand decision rather than a technical one, and it would compete with
Montserrat rather than support it. Recorded here so the next person does not
re-derive the list.

Three weights, not four: the two `font-weight: 700` rules outside the
`@font-face` blocks are both on `var(--font-brand)`, so Montserrat covers bold.
Body text needs 400 (the default), 500 (three rules) and 600 (fourteen). No
italic face: two rules use `font-style: italic`, both small muted text, and
synthetic oblique is adequate there — a real italic would be another 20.7 KiB
for `.expenses__row-shares` and `.expenses__payer-name--none`.

## Milestone 1: the UI face ships

Make `scripts/gen_brand_fonts.py` family-aware and add Inter to it, then wire
the faces into the app.

The generator today hardcodes one `SOURCE_DIR`, one `WEIGHTS` map and one
`DESTINATIONS` list, and writes everything to both `web/fonts/` and
`docs/assets/fonts/`. That second destination exists because the documentation
site cannot reach files outside its own `docs_dir`. It wants Montserrat and
only Montserrat — the site sets its own body text through Material, and
`todo.md` already records that it ships a Montserrat 500 it never loads.
Writing Inter there too would make that entry worse. So the restructure is per
family: source directory, weights, licence path and destinations all move into
a family record.

Then, in the app:

- three `@font-face` blocks for Inter 400/500/600, alongside the two for
  Montserrat, with the same `font-display: swap` for now — Milestone 2 revisits
  that deliberately;
- a `--font-ui` token next to `--font-brand`, holding `"Inter"` followed by the
  existing `system-ui, -apple-system, "Segoe UI", sans-serif` stack, so the
  fallback is exactly what ships today;
- `body { font-family: var(--font-ui) }`. The `font: inherit` floor on inputs,
  selects and textareas carries the form controls for free;
- the three new URLs in the `sw.js` precache list. Nothing to do about cache
  busting — since Stage 23 Milestone 2 the server fingerprints the asset tree
  into `sw.js` on the way out;
- Inter's OFL alongside Montserrat's. They are separate copyright holders, so
  it is two files, not one merged one.

Verification: `make ci` green, the Montserrat output byte-identical to what is
committed (a family restructure that quietly re-subsets the brand face is a
regression), and a Playwright assertion that the three Inter faces actually
load and that a body element's used font is Inter rather than the fallback —
`document.fonts.check` plus a rendered-width comparison against the fallback
stack, the same technique `capabilities.setup.js` already uses to prove the
fontconfig pin took. A screenshot is not evidence here; a width that changed is.

**Done.** Inter 400/500/600 ships, subset from `rsms-inter-fonts` with the same
coverage and flags Montserrat already used: 19.7 + 20.0 + 20.2 KiB, against a
556 KiB `maplibre-gl.mjs`.

`scripts/gen_brand_fonts.py` is now a list of family records, each carrying its
own source directory, package name, licence path, weights and destinations.
Montserrat goes to `web/fonts/` and `docs/assets/fonts/`; Inter goes to
`web/fonts/` only, for the reason the plan gave — the site sets its body text
through Material and would only be carrying dead weight. Two families from two
copyright holders means two licence files rather than one, so `OFL.txt` became
`montserrat-OFL.txt` and `inter-OFL.txt`; `README.md` and the `base.css` header
comment follow the rename.

In the app: three `@font-face` blocks beside Montserrat's two, a `--font-ui`
token holding `"Inter"` in front of the exact stack `body` used before, `body`
switched to the token, and the three URLs added to `sw.js`'s `SHELL_URLS`.

*Verified.* `make ci` green, and `make docs` green (the site's own copy was
renamed underneath it). The Montserrat woff2 files the generator now produces
are **byte-identical** to the committed ones — `cmp` against a copy taken
before the restructure — so the family split changed nothing about the brand
face.

The behavioral proof is a new assertion in `brand.spec.js`, "the UI face loads
in every shipped weight and is what body is set in": it loads 400, 500 and 600
separately (a 404ing 500 would otherwise hide behind a loaded 400 as a
synthesised weight), asserts `getComputedStyle(document.body).fontFamily`
contains Inter, and measures a mixed string with umlauts and typographic
punctuation under Inter against the fallback stack, requiring them to differ.
The three new woff2 files also joined the content-type sweep in the same spec.
Ten tests pass under `make test-ui GREP="brand assets"`.

That assertion was then checked for vacuity rather than assumed: reverting
`body` to the old hard-coded stack makes it fail, and restoring the token makes
it pass. No screenshots — a width that changed is the evidence here.

*Two things worth recording.* The prose in `tests/ui/fonts.conf` and
`capabilities.setup.js` described a `body` set in `system-ui`, which stopped
being true with this milestone, so both were corrected to describe the pin as
governing the stack *behind* Inter — the pin itself is untouched, and the
question of whether the suite should still measure the fallback is Milestone
2's to answer. Writing that correction walked straight into the Stage 14 XML
trap from the other direction: the custom property is spelled `--font-ui`, and
a double hyphen inside an XML comment is exactly what silently stopped
fontconfig loading this file in Stage 40. A guard in the edit script caught it;
the comment now says "the font-ui custom property", and
`FONTCONFIG_FILE=... fc-match sans-serif` still answers DejaVu Sans.

*One deviation.* Neither font package was installed on the development machine,
and installing them needs root. The faces were generated by driving the real
`build()` against RPMs extracted with `rpm2cpio` — which is what makes the
byte-identical Montserrat check above load-bearing, since it proves the
alternate source produced exactly the committed output. `plans/todo.md` now
carries an entry proposing a `--source-root` argument so this is a supported
path rather than a trick.

## Milestone 2: what the first frame looks like

Confront the caveat from the Context section rather than inheriting it.

With `swap`, the first paint of every cold load is the fallback and the second
is Inter, which is both a reflow and a second set of metrics every layout must
survive. Three options, and this milestone is partly about choosing between
them with measurements rather than taste:

- **keep `swap`** and accept the reflow, on the grounds that the service worker
  precaches the faces so it is a first-visit-only cost;
- **`font-display: optional`**, which removes the reflow entirely at the price
  of showing the fallback for the whole of a cold first load. For a PWA whose
  service worker will have the faces by the second visit this is a better fit
  than it sounds;
- **a metric-adjusted fallback** — a second `@font-face` over
  `local("DejaVu Sans")`, `local("Segoe UI")`, `local("Roboto")` and so on with
  `size-adjust`, `ascent-override` and `descent-override` computed so the
  fallback occupies Inter's boxes. Most work, best result, and the overrides
  must be computed from the actual font metrics rather than guessed.

Whatever is chosen, the UI suite needs a look afterwards. `tests/ui/fonts.conf`
pins DejaVu, so the suite now measures the fallback while production measures
Inter. That may still be the signal we want — it is the adversarial case, and
under `swap` it is a real first frame — but it should be a decision with a
comment on it, not an accident.

Verification: whichever route is taken, a measurement that distinguishes it
from the others. For the metric-adjusted fallback that is a layout-shift number
(the bounding boxes of a fixed set of elements before and after the face
loads); for `optional` it is that the face does not apply on a cold load and
does on a warm one.

**Done.** `swap` stays; the fallback was made to fit instead.

*The measurement came first, and the first version of it was wrong.* A sweep
harness rendered all 25 routes twice — once with the Inter files aborted, once
normally — and reported a perfect zero shift on every route. It was measuring
nothing: the service worker answered the font request from its own cache,
straight past the interception, and a second bug meant the page-level
catch-all route from `blockExternalRequests` shadowed the abort anyway. A
`document.fonts.check` probe in the snapshot is what exposed it, reporting
Inter as loaded in the pass that was supposed to be without it. Rebuilt with a
fresh context per pass and `serviceWorkers: "block"`, the real numbers at 324px
were: 77 to 94 percent of elements on a route move, worst single element 68px,
page heights changing by up to 68px. Body text measured 298.4px in the fallback
against 280.2px in Inter.

*So `swap` alone was not defensible, and `optional` was rejected on a different
ground.* It would remove the shift by never swapping, but it decides *per page
load* whether the face is used at all, on a 100ms timer. A loaded CI runner
that misses that window would silently render every layout assertion in the
suite against a different font. That is precisely the class of failure Stage 40
spent itself on, and it is not worth buying with the thing this stage exists to
remove.

*What landed is the metric-adjusted fallback.* `scripts/gen_font_fallbacks.py`
computes, for each platform font it can measure, a `size-adjust` plus
`ascent-override` / `descent-override` / `line-gap-override`, and writes the
faces into `base.css` between generated-region markers. `--font-ui` now reads
`"Inter", var(--font-ui-fallback, system-ui), system-ui, ...` — the `var()`
default matters, because a `var()` that resolves to nothing invalidates the
whole declaration rather than falling through to the next entry.

*The interesting part is where `size-adjust` comes from.* It is a ratio of
average character width, and the usual recipe averages a flat pass over the
alphabet and digits. That gives 97.57% for DejaVu Sans. Weighting each
character by how often it actually occurs in `web/locales/*.json` gives
94.03%. The browser, measured directly, says 93.91%. Interface copy is mostly
lowercase and spaces, which is exactly where Inter is proportionally narrowest,
and the flat average is dominated by capitals nobody types. So the generator
reads the app's own locale files as its corpus — which also means the numbers
are re-derived when the copy changes.

Seven faces ship: Roboto, DejaVu Sans, Noto Sans, Cantarell, Adwaita Sans,
Liberation Sans, Open Sans. Every one was measured from a font file on the
machine that ran the script, and the script exits rather than skipping a
missing one, so the committed CSS cannot quietly depend on what happened to be
installed. Segoe UI and the Apple system font are deliberately absent: they are
almost certainly the two most common fallbacks in the world and neither can be
measured here, and a number nobody in this repository can check is what Stage
14 and Stage 40 were both about. Arial is absent for a different reason — it
could be adjusted accurately through metric-compatible Liberation Sans, but it
sits ahead of `system-ui` in the cascade, so listing it would take Windows
readers off Segoe UI and onto Arial for the sake of one frame.

*Verified.* Body text now measures 280.5px in the fallback against 280.2px in
Inter, a 0.1% error where it was 6.5%. Worst element displacement across the
full 25-route sweep fell from 68px to 5.4px at 324px and from 21.2px to 7.0px
at 1280px; page heights land within 5px. `make ci`, `make docs` and the full UI
suite all green -- 297 passed.

One note for whoever sees it next: `register.spec.js`'s "registering an account
logs the newcomer straight in" failed with a 500 on one of the intermediate
full-suite runs and passed alone and on both later full runs. It registers a
fixed username and nothing here touches registration, so it is an existing
flake under parallel load rather than anything this stage did -- recorded
because a one-off 500 that nobody wrote down is a bug that gets rediscovered.

The sweep harness is gone, replaced by `tests/ui/font-metrics.spec.js` — three
dense routes rather than 25, 8 seconds rather than 1.6 minutes. Worst
displacement across the three: 4.2px, against a 12px ceiling.

*Getting that test right took three attempts, and each failure is worth
recording because each one passed while measuring nothing.* The first blocked
the woff2 files with `page.route` and reported a flawless zero shift on every
route — the service worker was answering from its own cache straight past the
interception, and a page-level catch-all from `blockExternalRequests` was
shadowing the abort as well. A `document.fonts.check` probe added to the
snapshot is what exposed it. The second fixed that with a fresh context per
pass and two navigations; it passed alone and failed in the parallel suite
claiming a 76.5px shift, because another worker created and deleted trips
between the two passes and it was comparing two different pages. The third
measured both states on one page load by aborting and un-aborting the font URL
per route, and deadlocked under parallel load into a 180s timeout.

What ships does none of that. It renders the page once, reads the computed
`--font-ui`, and re-renders with Inter stripped off the front of that stack —
no interception, no service worker, no second navigation, nothing another
worker can change underneath it. It reads the stack from the page rather than
restating it, so it cannot drift from `base.css`. The two methods agree to the
tenth of a pixel (3.4 / 4.2 / 3.1px), which is the cross-check that the simpler
one is measuring the same thing.

Three guards, all confirmed by breaking them: removing the fallback from the
stack fails on width with a diagnostic naming the generator; an override that
changes nothing fails on "not one box moved", because a zero shift is the
signature of a measurement that measured nothing rather than a perfect score;
and a `--font-ui` that does not start with Inter fails before measuring
anything.

One trap worth recording from the attempt that used the Font Loading API:
`document.fonts.check("400 16px Inter")` cannot serve as the did-it-load
signal while the files are blocked. The CSS-connected faces stay in the set
permanently in status `error`, and one errored face in a family makes `check`
answer false no matter how many working faces are added beside it — the page
renders in Inter while the font set denies having it.

*The `fonts.conf` question is answered, and the answer inverted the pin's
purpose.* It stays, but it is no longer the adversarial case — it is the
*known* case. `font-metrics.spec.js` has to know which font it is measuring
against, and the pin is what tells it; pinning a family the generator has no
numbers for would fail the shift test for a reason that is not a bug. The two
files now move together, which is written into `fonts.conf`.

## Milestone 3: the pictures and the paperwork

Everything under `docs/assets/screenshots/` is committed and every one of them
now shows the wrong body font — they were captured in Noto Sans. `make
screenshots` regenerates them against a throwaway server, so this is mostly a
run and a careful look at the diff, but it is a real part of the change and it
is the part most likely to be forgotten.

Also in this milestone: `docs/assets/brand/README.md` documents the palette,
the clear space and the self-hosted Montserrat, and should now say what the UI
face is and where the line between the two is drawn. And `todo.md`'s entry
about the site shipping an unloaded Montserrat 500 should be revisited — the
family restructure in Milestone 1 makes fixing it a one-line change to a
destination list.

## Build order

1. Milestone 1 — the faces, the generator, the wiring.
2. Milestone 2 — the first frame, once there is a real face to measure against.
3. Milestone 3 — the screenshots and the documentation, last, so they capture
   the settled result rather than an intermediate one.

## Workflow

One milestone at a time. For each: implement, verify with `make ci` plus a
manual or Playwright pass that proves the behavior actually changed, add a
"**Done.**" paragraph to that milestone's section here describing what landed
and how it was checked, update `plans/todo.md` in both directions, commit,
leave `make dev` running, and stop and hand back control. Do not start the next
milestone until told to continue; feedback at a checkpoint gets fixed and
re-verified before moving on.

## Verification

- `make ci` green at every milestone.
- The app renders in Inter, proven by measurement rather than by screenshot.
- The Montserrat files committed today are unchanged by the generator
  restructure.
- The UI suite is green, and the fallback path it exercises is exercised on
  purpose.
- `make docs` green for anything touching `docs/`.
