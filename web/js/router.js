// Navigates client-side from anywhere (not just inside a rendered page),
// e.g. after a card's open event or a form's save/cancel/delete handler.
//
// Every pushed entry records the path it was pushed *from*. Two things read
// that: leaveEditor below, and the fact that its presence at all means "this
// entry was created by an in-app navigation" -- a typed URL, a bookmark or a
// reload has no state, which is exactly the case where popping the entry
// would leave the app entirely.
//
// With {replace: true} the current entry is overwritten instead. That is for
// a page that should not be a stop on the way back: see leaveEditor.
export function navigate(path, { replace = false } = {}) {
  if (path === window.location.pathname) return;
  if (replace) {
    // The replaced entry inherits the "from" of the entry it overwrites: the
    // editor stood in the same slot, so what came before it is unchanged.
    window.history.replaceState({ from: window.history.state?.from }, "", path);
  } else {
    window.history.pushState({ from: window.location.pathname }, "", path);
  }
  window.dispatchEvent(new PopStateEvent("popstate"));
}

// Leaves an editor route without leaving it in the history.
//
// The problem this solves: overview -> location -> edit -> save put three
// entries behind you, so getting back to the overview took three presses of
// Back, twice through a form nobody wants to see again.
//
// Two cases, and the difference is whether the destination is the entry the
// editor was pushed on top of:
//
//   - Editing an existing location returns to the location it came from, so
//     the editor entry can simply be popped. Back() is better than a replace
//     here: it restores the view page's scroll position, and it leaves the
//     history exactly the length it was before Edit was pressed.
//   - Creating one returns to a location that has never been in the history,
//     so there is nothing to pop back to and the editor entry is overwritten
//     instead. Same result -- one Back from the new location reaches the
//     overview -- by the only means available.
//
// Falls back to a replace when the editor entry was not pushed by us (a typed
// /edit URL, or a reload while in the form): back() would then navigate away
// from the app, which is not what saving a form should do.
export function leaveEditor(path) {
  if (window.history.state?.from === path) {
    window.history.back();
    return;
  }
  navigate(path, { replace: true });
}

// Minimal History API router. Routes are {pattern, render} where pattern
// segments starting with ":" are captured as params, e.g. "/trips/:tripId".
//
// One pattern is special: "*" is the catch-all, rendered when nothing else
// matches. It's a route in the list like any other (see app.js) rather than
// an option on createRouter, so reading the routes array tells you what an
// unknown URL does. Unmatched paths used to be redirected to /trips instead,
// which silently pretended the URL had been something else.
export function createRouter(routes, container) {
  function match(path) {
    for (const route of routes) {
      if (route.pattern === "*") continue;
      const patternParts = route.pattern.split("/").filter(Boolean);
      const pathParts = path.split("/").filter(Boolean);
      if (patternParts.length !== pathParts.length) continue;

      const params = {};
      const isMatch = patternParts.every((part, i) => {
        if (part.startsWith(":")) {
          params[part.slice(1)] = decodeURIComponent(pathParts[i]);
          return true;
        }
        return part === pathParts[i];
      });
      if (isMatch) return { route, params };
    }
    const catchAll = routes.find((r) => r.pattern === "*");
    return catchAll ? { route: catchAll, params: {} } : null;
  }

  async function render() {
    const result = match(window.location.pathname);
    if (!result) return;
    await result.route.render(container, result.params);
  }

  function navigate(path) {
    if (path !== window.location.pathname) {
      // Same state shape as the exported navigate above -- a data-link click
      // is an in-app navigation like any other, and leaveEditor has to be
      // able to tell one from a typed URL whichever path pushed the entry.
      window.history.pushState({ from: window.location.pathname }, "", path);
    }
    render();
  }

  window.addEventListener("popstate", render);

  // Intercept clicks on same-origin links marked data-link so navigation
  // stays client-side instead of doing a full page load.
  document.addEventListener("click", (e) => {
    const link = e.target.closest("[data-link]");
    if (!link) return;
    e.preventDefault();
    // An editor page's own back-link carries data-leave-editor, so pressing it
    // pops the editor entry instead of pushing its destination on top -- the
    // same treatment its Cancel button gets. Without this the one exit that is
    // an <a> rather than a button would still leave the form in the history.
    if (link.hasAttribute("data-leave-editor")) leaveEditor(link.getAttribute("href"));
    else navigate(link.getAttribute("href"));
  });

  return { render, navigate };
}
