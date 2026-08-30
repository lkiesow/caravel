# Stage 28 — The location page at desktop width

## Context

The read-only location page (`web/js/pages/location-view-page.js`) reads well at
324px and badly at 960px, for two independent reasons that had never been looked
at together.

**The cover was capped at 20rem.** `.location-view__image` was `width: 100%;
max-width: 20rem` with no `aspect-ratio` and no `object-fit`. In the 60rem
content column that put the photograph in the left third with a wide empty
gutter beside it — the picture looked lost rather than deliberate. The trip
detail page had already solved exactly this for its own cover
(`.trip-detail__cover`), and the comment above that rule explains the reasoning.

**Category and tags were two stacked blocks with no room before the image.** The
page emitted `.location-view__meta` (a coloured dot plus the category label) and
then a separate `<ul class="tag-list location-view__tags">` whose only spacing
was `margin-top: 0.5rem`. The image that followed had no top margin of its own,
so the chips sat directly on top of the photograph.

### Decisions taken before planning

- **The cover is full content width with a 16:9 crop**, `object-fit: cover`,
  capped in height so a 928px column does not produce a 522px-tall banner.
- **Category and tags are one wrapping row inside a bordered bar, at every
  width** — one code path, one look. At 324px the chips wrap onto a second line
  inside the bar rather than the layout changing at a breakpoint.

No user-facing copy changes, so no `web/locales/` work.

## 1. One meta bar, one cover banner

Three changes, all on the location view.

**Merge the category row and the tag row.** The `<ul class="tag-list
location-view__tags">` moves inside `.location-view__meta`, and that element
becomes an outlined, wrapping bar: `flex-wrap`, a two-axis `gap`, padding, a
1px border, `border-radius: 1rem` and `1.5rem` of margin below it — the space
that was missing before the picture. Background is `--color-bg`, not
`--color-surface`: the chips inside are already surface-coloured with a border,
so a surface bar would swallow them.

The class names stay exactly as they were. Four UI specs select on
`.location-view__tags .tag-chip`, including one asserting the `<ul>` is absent
for an untagged location, and all must keep passing unedited.

**Wrap the cover and its credit in a `<figure>`.** `.image-credit` carried
`margin: -0.5rem 0 1rem` — a negative margin tuned against the image's
`margin-bottom`, which this change removes. A `figure.location-view__cover`
holding the `<img>` and a `<figcaption class="image-credit">` makes that a real
flex `gap` instead. `<figure>`'s browser default `margin: 1em 40px` means the
explicit margin on it is load-bearing, not cosmetic.

**Give the cover a fixed shape.** `width: 100%; aspect-ratio: 16 / 9;
max-height: 22rem; object-fit: cover`, the same treatment `.trip-detail__cover`
gets. `max-height` is what keeps the desktop banner sane: at the 928px content
width 16/9 alone would be 522px, so the cap takes over and the visible crop is
nearer 16/6 there, while at 324px the full 16/9 is honoured.

While in the file: `.location-view__meta .type-label` had been dead since
Stage 26 retired the free-text type into the tag set — the page emits
`.category-label`, which had never been styled at all.

**Done.** Landed as planned, in `web/js/pages/location-view-page.js` (the meta
block and the cover block in the template literal) and `web/css/base.css`
(`.location-view__cover` and a rewritten `.location-view__image`,
`.location-view__meta` and a new `.location-view__meta .category-label`
replacing the dead `.type-label` rule, the deleted `.location-view__tags`
margin, and `.image-credit`'s negative margin reduced to `0`). One deviation
worth noting: a credit with *no* image is now not rendered at all, since the
`<figcaption>` lives inside the figure the image gates. `image_credit` is only
ever written alongside `image_url`, so this is unreachable in practice, and the
line it removes was a stray attribution for nothing.

Verified with `make ci` green, plus Playwright MCP against `make dev` on a
seeded location with a cover, a credit and three tags. Measured rather than
eyeballed: at 1280x800 the image is 928x352 with `object-fit: cover` and
`aspect-ratio: 16 / 9` (so the 22rem cap is what binds), and the gap between the
bar's bottom edge and the image's top edge is exactly 24px; at 324x756 the same
image is 292x164.25, which is 16/9 to the pixel, the bar grows from 44px to 74px
because the chips wrap onto their own row inside it, and neither the bar nor the
document scrolls horizontally. The tag chips resolve under
`.location-view__meta` at both widths, and a location with no cover renders no
`.location-view__cover`, no `.location-view__image` and no `.image-credit`, with
the notes following the bar directly. Both themes were looked at: the bar reads
as an outline against the page in each.

`tests/ui/locations.spec.js` gained one assertion beside the existing tag check,
pinning the chips to `.location-view__meta` so the merge cannot be silently
undone. `locations.spec.js`, `assist.spec.js` and `image-search.spec.js` were
run on both browsers: everything passes on chromium, and three firefox failures
in `assist.spec.js`/`image-search.spec.js` were confirmed pre-existing by
stashing this milestone's three files and reproducing them on a clean tree.

## Build order

One milestone, so none. If this stage grows, the location page's desktop layout
is the theme.

## Files this touches

- `web/js/pages/location-view-page.js`
- `web/css/base.css`
- `tests/ui/locations.spec.js`

## Out of scope, deliberately

- **A two-column desktop layout for this page.** The `.trip-detail` grid pattern
  is available if the page ever wants one, but the complaint was the picture's
  size and the tag spacing, not the column count.
- **The Location card, map, links, dates, files and Edit button.** Untouched.
- **`make screenshots`.** The committed set includes location-view captures; if
  the difference is visible there it is a follow-up commit, not part of this
  one.

## Verification

`make ci` green, plus the measured Playwright pass recorded in the milestone's
Done paragraph: computed geometry at 1280x800 and 324x756 rather than
screenshots, the three existing specs unedited, and a look at both themes.

## Workflow

One milestone at a time. For each: implement, verify (`make ci` plus the
milestone's own proof), add a **Done.** paragraph to that milestone's section
here describing what actually landed — including any deviation from this plan —
and how it was verified, reconcile `plans/todo.md` in both directions, commit
(one commit per milestone; a same-day follow-up fix gets its own
"... follow-up: ..." commit), make sure `make dev` is running, then stop and
hand back control.
