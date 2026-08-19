import { t } from "../i18n.js";

// Fills `target` with a loading line, to be overwritten when the data lands.
//
// Every route used to paint nothing at all while its fetch was in flight -
// either a blank container (the pages that await before their first paint) or
// a toolbar over an empty void (the ones that paint a shell first). On a fast
// connection that's a flash; on a slow one it's a page that looks broken.
// `common.loading` already existed for app boot (app.js) and had exactly one
// caller; this is the same string, per route.
//
// Pass whatever element the data will fill: a page container for the
// await-then-paint routes, or just the list container for the shell-first
// ones, so their toolbar and heading stay put while the list loads.
//
// role="status" makes it an assertive-enough live region that a screen reader
// announces the wait without stealing focus. It carries no heading, so a
// route caught mid-load has no <h1> - the UI suite waits for in-flight
// fetches to settle before asserting heading outlines (tests/ui/helpers).
export function renderLoading(target) {
  target.innerHTML = `<p class="loading" role="status"></p>`;
  target.querySelector(".loading").textContent = t("common.loading");
}
