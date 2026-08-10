import { initI18n, t, translatePage } from "./i18n.js";
import { api } from "./api.js";
import { renderLoginPage } from "./pages/login-page.js";
import { renderTripsPage } from "./pages/trips-page.js";
import { renderTripDetailPage } from "./pages/trip-detail-page.js";
import { renderTripEditorPage } from "./pages/trip-editor-page.js";
import { renderLocationEditorPage } from "./pages/location-editor-page.js";
import { renderLocationViewPage } from "./pages/location-view-page.js";
import { renderUserMenu } from "./components/user-menu.js";
import { createRouter } from "./router.js";

const app = document.getElementById("app");

const routes = [
  { pattern: "/trips", render: renderTripsPage },
  // "/trips/new" must precede "/trips/:tripId", and "/trips/:tripId/locations/new"
  // must precede "/trips/:tripId/locations/:itemId" - same segment counts, and
  // the router's match() takes the first pattern that fits, so the literal
  // route would otherwise never be reached (":param" swallows "new"/"edit").
  { pattern: "/trips/new", render: renderTripEditorPage },
  { pattern: "/trips/:tripId/edit", render: renderTripEditorPage },
  { pattern: "/trips/:tripId/locations/new", render: renderLocationEditorPage },
  { pattern: "/trips/:tripId/locations/:itemId/edit", render: renderLocationEditorPage },
  { pattern: "/trips/:tripId/locations/:itemId", render: renderLocationViewPage },
  { pattern: "/trips/:tripId", render: renderTripDetailPage },
];

async function renderAuthenticated(user) {
  app.innerHTML = `
    <header class="app-header">
      <strong>${t("app.name")}</strong>
      <div class="user-menu-slot"></div>
    </header>
    <main id="main"></main>
  `;
  translatePage(app);

  renderUserMenu(app.querySelector(".user-menu-slot"), user, {
    onLogout: async () => {
      await api.post("/auth/logout");
      boot();
    },
  });

  const router = createRouter(routes, document.getElementById("main"));
  router.render();
}

async function boot() {
  app.textContent = t("common.loading");
  try {
    const user = await api.get("/auth/me");
    await renderAuthenticated(user);
  } catch {
    const user = await renderLoginPage(app);
    await renderAuthenticated(user);
  }
}

initI18n().then(boot);

if ("serviceWorker" in navigator) {
  window.addEventListener("load", () => {
    navigator.serviceWorker.register("/sw.js").catch(() => {
      // Non-fatal: the app works fully online without the service worker.
    });
  });
}
