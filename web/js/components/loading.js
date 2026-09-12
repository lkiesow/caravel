import { t } from "../i18n.js";

// The brand mark's two paths, inline rather than referenced out of
// web/brand/mark.svg with <use>. The loader is the first thing a route paints,
// and a second network round trip to fetch the thing that covers a wait is
// exactly the wait it exists to cover. Kept byte-identical to mark.svg (and to
// the copies in web/icons/favicon.svg and the lockups) - if the mark is ever
// redrawn, gen_icons.py regenerates those and this one has to follow by hand.
const MARK_PATHS =
  `<path d="M43.2 4C31.4 14.8 21.4 31 14.6 50.4L39 40.6C42.2 28.6 43.6 15.4 43.2 4Z"></path>` +
  `<path d="M27.4 46.6 60.8 37.9 41.2 60.8 27.4 46.6Z" opacity=".72"></path>`;

// The markup for the animation alone, without the live region around it.
// index.html carries a static copy of this for the pre-JS boot screen; keep
// the two in step.
//
// aria-hidden because it is decoration: the wait is announced by the
// .sr-only label its callers pair it with, and a screen reader has no use for
// a ring and a sail.
export function loaderMarkup(size) {
  const lg = size === "lg" ? " loader--lg" : "";
  return (
    `<span class="loader${lg}" aria-hidden="true">` +
    `<svg class="loader__ring" viewBox="0 0 64 64">` +
    `<circle class="loader__track" cx="32" cy="32" r="28" fill="none" stroke-width="3"></circle>` +
    // 48 of the 176px circumference, so roughly a quarter of the ring sweeps.
    `<circle class="loader__arc" cx="32" cy="32" r="28" fill="none" stroke-width="3"` +
    ` stroke-linecap="round" stroke-dasharray="48 128"></circle>` +
    `</svg>` +
    `<span class="loader__sail"><svg viewBox="0 0 64 64">${MARK_PATHS}</svg></span>` +
    `</span>`
  );
}

// Fills `target` with a loading state, to be overwritten when the data lands.
//
// Every route used to paint nothing at all while its fetch was in flight -
// either a blank container (the pages that await before their first paint) or
// a toolbar over an empty void (the ones that paint a shell first). On a fast
// connection that's a flash; on a slow one it's a page that looks broken.
//
// Pass whatever element the data will fill: a page container for the
// await-then-paint routes, or just the list container for the shell-first
// ones, so their toolbar and heading stay put while the list loads. Those two
// cases are what `size` picks between - "lg" is the page-sized loader with
// height reserved under it, the default is the smaller one that sits in a
// list body without leaving a hole.
//
// role="status" makes it an assertive-enough live region that a screen reader
// announces the wait without stealing focus. It carries no heading, so a
// route caught mid-load has no <h1> - the UI suite waits for in-flight
// fetches to settle before asserting heading outlines (tests/ui/helpers).
export function renderLoading(target, { size } = {}) {
  const page = size === "lg" ? " loading--page" : "";
  target.innerHTML =
    `<div class="loading${page}" role="status">` +
    loaderMarkup(size) +
    `<span class="sr-only"></span>` +
    `</div>`;
  target.querySelector(".sr-only").textContent = t("common.loading");
}
