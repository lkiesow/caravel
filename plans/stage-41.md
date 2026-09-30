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
