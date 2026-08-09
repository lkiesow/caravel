// Minimal History API router. Routes are {pattern, render} where pattern
// segments starting with ":" are captured as params, e.g. "/trips/:tripId".
export function createRouter(routes, container) {
  function match(path) {
    for (const route of routes) {
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
    return null;
  }

  async function render() {
    const result = match(window.location.pathname);
    if (!result) {
      navigate("/trips");
      return;
    }
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
