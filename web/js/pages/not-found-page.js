import { translatePage } from "../i18n.js";
import { icon } from "../icon.js";

// The one "there's nothing here" page, used for both ways of getting one:
//
// 1. A URL that matches no route. That used to silently redirect to /trips,
//    which told the user nothing and - worse for testing - meant a typo in a
//    test's URL quietly exercised the trips list instead of failing (called
//    out in tests/ui/helpers/scenarios.js).
// 2. A well-formed URL whose resource is gone: a stale bookmark, a deleted
//    trip, a link to a location someone else removed. The three pages that
//    fetch by ID each rendered their own bare `<p>Not found.</p>` plus a raw
//    link, flush against x=0 with no page container - the only screens in the
//    app that didn't look like the app.
//
// Registered as the router's catch-all (pattern "*", see app.js) for the
// first case and called directly for the second, which is why the back link
// is a parameter: "Home" for an unknown URL, but back to the trip for a
// location that no longer exists.
export function renderNotFoundPage(container, { href = "/trips", labelKey = "common.home" } = {}) {
  container.innerHTML = `
    <div class="page not-found">
      <a href="${href}" data-link class="back-link">${icon("arrow-left")} <span data-i18n="${labelKey}"></span></a>
      <div class="page__header">
        <h1 data-i18n="notFound.title"></h1>
      </div>
      <p data-i18n="notFound.body"></p>
    </div>
  `;
  translatePage(container);
}
