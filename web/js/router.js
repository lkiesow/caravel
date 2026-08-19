// Navigates client-side from anywhere (not just inside a rendered page),
// e.g. after a card's open event or a form's save/cancel/delete handler.
export function navigate(path) {
  if (path === window.location.pathname) return;
  window.history.pushState({}, "", path);
  window.dispatchEvent(new PopStateEvent("popstate"));
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
      window.history.pushState({}, "", path);
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
    navigate(link.getAttribute("href"));
  });

  return { render, navigate };
}
