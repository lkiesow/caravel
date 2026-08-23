import { initI18n, t, translatePage } from "./i18n.js";
import { eventBus } from "./eventbus.js";
import { api } from "./api.js";
import { renderLoginPage } from "./pages/login-page.js";
import { renderTripsPage } from "./pages/trips-page.js";
import { renderTripDetailPage } from "./pages/trip-detail-page.js";
import { renderTripEditorPage } from "./pages/trip-editor-page.js";
import { renderLocationEditorPage } from "./pages/location-editor-page.js";
import { renderLocationViewPage } from "./pages/location-view-page.js";
import { renderSettingsPage } from "./pages/settings-page.js";
import { renderAdminPage } from "./pages/admin-page.js";
import { renderNotFoundPage } from "./pages/not-found-page.js";
import { renderUserMenu } from "./components/user-menu.js";
import { createRouter, navigate } from "./router.js";
import { initTheme } from "./theme.js";
import { TRIP_TABS } from "./trip-tabs.js";
import { setCurrentUser } from "./session.js";

const app = document.getElementById("app");

// Trip detail tabs (Locations/Map/Itinerary/Files/Checklists/Settings)
// are each a real 3-segment route so the URL reflects the active tab and
// back/forward works - see trip-detail-page.js. They're all distinct
// literals at the same depth, so there's no ordering conflict between
// them; "/trips/:tripId" (no tab segment) is handled last and
// canonicalizes itself to "/trips/:tripId/locations" on render.
const tripTabRoutes = TRIP_TABS.map(({ key }) => ({
  pattern: `/trips/:tripId/${key}`,
  render: (container, params) => renderTripDetailPage(container, { ...params, tab: key }),
}));

const routes = [
  // index.html is served at "/", so that's where a fresh visit lands.
  // Canonicalize it to the trips list rather than letting it fall through to
  // the catch-all below and report the app's own entry point as not found.
  { pattern: "/", render: () => navigate("/trips") },
  { pattern: "/trips", render: renderTripsPage },
  // Account settings (not a trip's Settings tab - see pages/settings-page.js).
  { pattern: "/settings", render: renderSettingsPage },
  // Account *administration*, for admins only. The page checks that itself and
  // renders not-found otherwise, since the URL is typeable.
  { pattern: "/admin", render: renderAdminPage },
  // "/trips/new" must precede "/trips/:tripId", and "/trips/:tripId/locations/new"
  // must precede "/trips/:tripId/locations/:itemId" - same segment counts, and
  // the router's match() takes the first pattern that fits, so the literal
  // route would otherwise never be reached (":param" swallows "new"/"edit").
  { pattern: "/trips/new", render: renderTripEditorPage },
  { pattern: "/trips/:tripId/locations/new", render: renderLocationEditorPage },
  { pattern: "/trips/:tripId/locations/:itemId/edit", render: renderLocationEditorPage },
  { pattern: "/trips/:tripId/locations/:itemId", render: renderLocationViewPage },
  ...tripTabRoutes,
  { pattern: "/trips/:tripId", render: renderTripDetailPage },
  // The catch-all. Must be last: the router takes the first pattern that
  // fits, and "*" fits everything.
  { pattern: "*", render: renderNotFoundPage },
];

// The listener that re-renders the app when the language changes, kept at module
// scope so a re-mount (logging out and back in) replaces it instead of stacking
// a second copy that renders into a detached header.
let onLocaleChanged = null;

async function renderAuthenticated(user) {
  app.innerHTML = `
    <header class="app-header">
      <!-- The lockup, and a link home: the mark is decorative (the wordmark
           beside it already names the app, so a second accessible name would
           be read twice), and data-link lets the router handle the click
           instead of the browser reloading the whole app. -->
      <a class="app-brand" href="/trips" data-link>
        <span class="brand-mark" aria-hidden="true"></span>
        <span class="app-brand__wordmark">${t("app.name")}</span>
      </a>
      <div class="user-menu-slot"></div>
    </header>
    <main id="main"></main>
  `;
  translatePage(app);

  async function onLogout() {
    await api.post("/auth/logout");
    boot();
  }

  renderUserMenu(app.querySelector(".user-menu-slot"), user, { onLogout });

  const router = createRouter(routes, document.getElementById("main"));

  // i18n.js's setLocale re-runs translatePage, which only rewrites declarative
  // data-i18n attributes - every string built through t() in JS (menu labels,
  // category filters, day headings) would keep the old language until the next
  // navigation. So a locale change re-renders the header and the current route.
  if (onLocaleChanged) eventBus.removeEventListener("locale-changed", onLocaleChanged);
  onLocaleChanged = () => {
    renderUserMenu(app.querySelector(".user-menu-slot"), user, { onLogout });
    router.render();
  };
  eventBus.addEventListener("locale-changed", onLocaleChanged);

  router.render();
}

async function boot() {
  app.textContent = t("common.loading");
  try {
    const user = await api.get("/auth/me");
    setCurrentUser(user);
    await renderAuthenticated(user);
  } catch {
    const user = await renderLoginPage(app);
    setCurrentUser(user);
    await renderAuthenticated(user);
  }
}

// The theme is already on <html> from index.html's inline script; this hooks up
// the part that needs a live listener - following the OS while the preference is
// "auto" and the tab stays open.
initTheme();

initI18n().then(boot);

if ("serviceWorker" in navigator) {
  window.addEventListener("load", () => {
    navigator.serviceWorker.register("/sw.js").catch(() => {
      // Non-fatal: the app works fully online without the service worker.
    });
  });
}
