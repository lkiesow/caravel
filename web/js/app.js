import { initI18n, t, translatePage } from "./i18n.js";
import { api } from "./api.js";
import { renderLoginPage } from "./pages/login-page.js";
import { renderTripsPage } from "./pages/trips-page.js";
import { renderTripDetailPage } from "./pages/trip-detail-page.js";
import { createRouter } from "./router.js";

const app = document.getElementById("app");

const routes = [
  { pattern: "/trips", render: renderTripsPage },
  { pattern: "/trips/:tripId", render: renderTripDetailPage },
];

async function renderAuthenticated(user) {
  app.innerHTML = `
    <header class="app-header">
      <strong>${t("app.name")}</strong>
      <span>${user.display_name}</span>
      <button data-action="logout" data-i18n="auth.logout"></button>
    </header>
    <main id="main"></main>
  `;
  translatePage(app);
  app.querySelector('[data-action="logout"]').addEventListener("click", async () => {
    await api.post("/auth/logout");
    boot();
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
