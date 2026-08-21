// App-shell service worker: cache-first for static assets, with runtime
// population instead of an exhaustive precache list (this app has many
// small JS modules; enumerating them all here would be brittle — see plan
// Section 4.6). /api/* is never touched; there's no offline data sync in v1.
//
// Bump this on any static-asset change you want clients to pick up
// immediately; old caches are dropped on activate.
const CACHE_VERSION = "caravel-shell-v2";

const SHELL_URLS = ["/", "/index.html", "/css/base.css", "/manifest.webmanifest"];

// Dev mode (CARAVEL_WEB_DIR) serves static files with a no-cache header
// specifically so live-reload works; honor that here too, or the service
// worker would defeat it by serving stale cached files.
function isCacheable(response) {
  const cacheControl = response.headers.get("Cache-Control") || "";
  return response.ok && !cacheControl.includes("no-store") && !cacheControl.includes("no-cache");
}

self.addEventListener("install", (event) => {
  event.waitUntil(
    caches
      .open(CACHE_VERSION)
      .then((cache) =>
        Promise.all(
          SHELL_URLS.map((url) =>
            fetch(url).then((response) => (isCacheable(response) ? cache.put(url, response) : null))
          )
        )
      )
      .then(() => self.skipWaiting())
  );
});

self.addEventListener("activate", (event) => {
  event.waitUntil(
    caches
      .keys()
      .then((keys) => Promise.all(keys.filter((k) => k !== CACHE_VERSION).map((k) => caches.delete(k))))
      .then(() => self.clients.claim())
  );
});

self.addEventListener("fetch", (event) => {
  const url = new URL(event.request.url);

  // Never intercept the API — no offline data sync in v1.
  if (url.pathname.startsWith("/api/")) return;
  if (event.request.method !== "GET") return;
  if (url.origin !== self.location.origin) return;

  event.respondWith(
    caches.match(event.request).then((cached) => {
      if (cached) return cached;
      return fetch(event.request).then((response) => {
        if (isCacheable(response)) {
          const copy = response.clone();
          caches.open(CACHE_VERSION).then((cache) => cache.put(event.request, copy));
        }
        return response;
      });
    })
  );
});
