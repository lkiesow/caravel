# Stage 38 — The map legend moves below the map

A one-milestone interstage change, like Stage 37. No new behaviour: the
same control, in a place that costs less.

## Context

The legend doubles as the map's category filter, and it sat in the most
valuable place on the page while being opened on a minority of visits.
Measured before the change:

- **Phone, 324x756.** The legend was in the flow *above* the map and
  stood **158px** tall -- three wrapped rows, up from two before Stage 37
  added three categories. It pushed the map's top edge to **y=714 of a
  756px viewport**, so opening the Map tab showed a cover banner, a
  title, a filter box and **42px of map**.
- **Desktop, 1280x900.** A different mechanism: `position: absolute` in
  the map's top-right corner, a 148x182 box. It cost no layout height
  but occluded a corner of the cartography, and pins could sit under it.

The overlay at wide widths was not a design choice so much as a
consequence of the layout: `:host` carried the height and `.map-wrap`
took the leftover with `flex: 1`, so anything added in the flow was
subtracted from the map. Floating the legend was the only way to add it
without shrinking the thing it describes.

## 1. Below the map, and one layout model

The legend moves out of `.map-wrap` and becomes the last thing in the
component, after the OpenStreetMap credit. The height moves from `:host`
to `.map-wrap`, so the map is the size it says it is and the component
grows by whatever is stacked underneath.

**Done.** Landed as planned, with four things worth recording:

- **The height swap needed a second custom property.** `--map-min-height`
  beside `--map-height`: the floor and the height have to move together,
  or the 24rem minimum silently inflates the editor's 20rem picker. The
  old code expressed this as `min-height: 0` overrides on `:host([lat])`
  and `:host([pick])`, which is the same idea in the shape the old model
  allowed.
- **The mobile block mostly disappeared.** It existed largely to undo the
  desktop model -- `height: auto` on the host and all three mounts, a
  second flex column on `.map-wrap`, `order: -1` on the legend. What is
  left is the trip map's `85vh` and the 44px tap-target floor on the
  legend's labels. `.gesture-hint` goes back to a plain `inset: 0`, since
  `.map-wrap` is now exactly the map at every width.
- **The legend is a `<fieldset>` with a `<legend>` reading "Show on map"
  / "Auf der Karte zeigen"** (one new key, `map.legend.label`). Over the
  map its meaning was positional; under a copyright line, seven
  unlabelled checkboxes are not self-explanatory, and the group had no
  accessible name at all before.
- **A guard inverted mid-edit.** The locate button is gated on the
  `locate` attribute, not on `chromeless`; moving the legend out of the
  wrapper briefly swapped the two. Caught immediately by reading the
  result back, but it is the kind of thing that would have rendered a
  locate button on the editor picker and passed a syntax check.

**Verified.** Measured on a running `make dev` at both widths. Phone:
the map's top edge moved **714 -> 548**, so **208px of map is visible on
load instead of 42**, with the map still 643px (85vh); the legend is
after the credit at y=1238, is a `FIELDSET` named "Show on map", and
every label is 44px. Desktop: the map is now exactly 60vh (540px, up
from 516 -- it was previously 60vh *minus* the credit), the top-right
corner hit-tests to `maplibregl-canvas` rather than to the legend, and
the legend is one 64px row under the credit. The other two mounts, whose
height rules were rewritten, are unchanged at both widths: 256px on the
location view, 320px in the editor picker, no legend, and the credit
still inside the host -- the Stage 34 overflow bug did not come back.
Toggling categories from the new position removes and restores the right
pins (3 -> 1 -> 3 with site and stay off). German at 324px fits in four
rows with no page overflow, and the dark app theme resolves the card's
surface, border and muted heading through the shadow boundary.

`make ci` green. `map.spec.js` (83), `map-theme.spec.js`,
`routes.spec.js` and `locations.spec.js` all pass -- `routes.spec.js` is
the one that matters most here, since it sweeps every route for
horizontal overflow and 44px tap targets in mobile/desktop x light/dark
x en/de.

**The test that had to invert.** `map.spec.js`'s "the map fills the
screen it is the subject of" asserted the legend sat *above* the map,
with a comment recording that rendering it after the map had put it at
y=769, past the fold, "with nothing hinting it was there". That
reasoning is reversed here deliberately, so the comment now records both
reversals -- the 20rem cap Stage 23 lifted, and this one -- rather than
being quietly deleted. The assertions are the opposite of the old ones
plus the number the change was *for*: `innerHeight - map.top > 150`. A
second case asserts the legend still filters from its new position and
is still announced as a named group, because moving a control is exactly
the change that can leave it looking right and wired to nothing.

**Deferred.** The committed docs screenshots were already three stages
stale (last regenerated Stage 31; Stage 34 moved the credit, Stage 37
added categories) and this makes it four. Regenerating needs `pngquant`
and a photo directory, neither present here, or the set comes out ~3x
larger with test-sheet images. Noted in `plans/todo.md`.

**Also removed from `plans/todo.md`:** "The map component means two
different things by a height" (Stage 34) -- that entry asked for exactly
this refactor, "the number is the map, the host is auto everywhere".

**Still open, deliberately:** the map's category filter and the locations
tab's filter menu remain two separate implementations of the same idea.
Unifying them is a real piece of work, not a tidy-up, and it is not this.
