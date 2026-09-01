// App-shell service worker: network-first for the shell and the code,
// stale-while-revalidate for static media, with runtime population instead of
// an exhaustive precache list (this app has many small JS modules; enumerating
// them all here would be brittle — see plan Section 4.6). /api/* is never
// touched; there's no offline data sync in v1.
//
// CACHE_VERSION is substituted by the server (handleServiceWorker in
// internal/httpapi/staticassets.go), which replaces the placeholder below
// with the build version plus a fingerprint of the whole asset tree. It used
// to be a constant edited by hand, which is a step that was documented and
// still missed: four edits in the project's life against a web/js directory
// that changes constantly, and a missed edit meant clients kept the old files
// indefinitely. The bytes of this file now change whenever a served asset
// does, which is the only signal a browser watches to decide this worker has
// been updated.
//
// The placeholder must sit inside a string literal: scripts/check_js.sh runs
// node --check over this file as a classic script.
const CACHE_VERSION = "caravel-shell-__CARAVEL_BUILD__";

// The brand face is in the shell rather than left to runtime population: it
// is in the first paint, so an offline load without it shows the fallback and
// reflows once the cache warms.
const SHELL_URLS = [
  "/",
  "/index.html",
  "/css/base.css",
  "/manifest.webmanifest",
  "/fonts/montserrat-500.woff2",
  "/fonts/montserrat-700.woff2",
];

// Dev mode (CARAVEL_WEB_DIR) serves static files with a no-store header
// specifically so live-reload works; honor that here too, or the service
// worker would defeat it by serving stale cached files.
//
// Note what is *not* refused: `no-cache`. Since Stage 23 Milestone 1 every
// production asset carries `Cache-Control: no-cache`, which means "keep this,
// but revalidate before using it" — not "do not keep it". Treating the two as
// the same thing (as this function did when only `no-store` existed in
// practice) would switch the cache off entirely in production and take the
// offline shell with it. Revalidating is exactly what the strategy below
// does.
// allowHTML is passed by the two callers that are legitimately fetching the
// shell — the precache and a navigation. It is deliberately not derived from
// request.mode: a Request built in script cannot have mode "navigate" (the
// constructor refuses it), so the precache below would fail its own check.
function isCacheable(response, allowHTML) {
  if (!response || response.status !== 200 || response.type === "opaque") return false;
  if ((response.headers.get("Cache-Control") || "").includes("no-store")) return false;

  // An asset request answered with HTML is the SPA fallback catching a path
  // this build no longer serves. Caching that stores a document under a
  // module URL, and the app then stays broken until the cache is dropped —
  // the failure Milestone 1 fixed on the server side. This is the same guard
  // on this side, for any client that meets an older server.
  if (!allowHTML && (response.headers.get("Content-Type") || "").includes("text/html")) {
    return false;
  }
  return true;
}

self.addEventListener("install", (event) => {
  event.waitUntil(
    caches
      .open(CACHE_VERSION)
      .then((cache) =>
        Promise.all(
          SHELL_URLS.map((url) =>
            fetch(url)
              .then((response) => (isCacheable(response, true) ? cache.put(url, response) : null))
              .catch(() => null)
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

// The shell and the code go to the network first. Serving a cached shell is
// what made an upgrade invisible: the tab came up on the old one and every
// module request was then answered from the same stale cache, for as long as
// the cache lived — which was forever, since nothing revalidated and the key
// only changed when somebody edited it by hand. Falling back to the cache
// keeps the app usable offline, which is the reason there is a worker at all.
async function networkFirst(request) {
  const isNavigation = request.mode === "navigate";
  try {
    const response = await fetch(request);
    if (isCacheable(response, isNavigation)) {
      const copy = response.clone();
      caches.open(CACHE_VERSION).then((cache) => cache.put(request, copy));
    }
    return response;
  } catch (err) {
    // Offline. A cached copy of this exact URL first; for a navigation, the
    // shell as a last resort, since every route renders from it.
    const cached = await caches.match(request);
    if (cached) return cached;
    if (isNavigation) {
      const shell = (await caches.match("/index.html")) || (await caches.match("/"));
      if (shell) return shell;
    }
    throw err;
  }
}

// Fonts, icons and images are served from the cache when they are there and
// refreshed in the background either way. They are the assets where an
// instant hit is worth having (they are in the first paint) and where being
// one version behind for a single load costs nothing, because they do not
// have to agree with anything else. Code does not get this treatment — see
// isCodeRequest.
async function staleWhileRevalidate(event, request) {
  const cache = await caches.open(CACHE_VERSION);
  const cached = await cache.match(request);

  const network = fetch(request)
    .then((response) => {
      if (isCacheable(response, false)) cache.put(request, response.clone());
      return response;
    })
    .catch(() => null);

  if (cached) {
    // Keep the worker alive for the refresh, or it may be killed as soon as
    // the cached response is returned and the cache never updates.
    event.waitUntil(network);
    return cached;
  }

  const response = await network;
  if (response) return response;
  throw new Error("offline and not cached: " + request.url);
}

// Code — the modules, the stylesheet, the locale files — goes to the network
// first, like the shell it has to agree with.
//
// The tempting alternative is stale-while-revalidate for everything, and it
// is subtly wrong here: it answers the first load after a deploy from the old
// cache and only then refreshes, so the new build takes *two* reloads to
// appear. That is a smaller version of the bug this milestone exists to fix.
// Network-first costs little now that Stage 23 Milestone 1 gives every asset
// an ETag: an unchanged module is a conditional request and a 304, not a
// refetch. Offline still works, from the same cache as the fallback.
//
// ".mjs" is here because ".js" does not cover it -- endsWith(".js") reads the
// last three characters, and those are "mjs". Vendored MapLibre ships three
// .mjs files (Stage 30 Milestone 1); without this line they would take the
// stale-while-revalidate path below, which is exactly the two-reload bug the
// paragraph above describes.
function isCodeRequest(pathname) {
  return (
    pathname.endsWith(".js") ||
    pathname.endsWith(".mjs") ||
    pathname.endsWith(".css") ||
    pathname.endsWith(".json")
  );
}

self.addEventListener("fetch", (event) => {
  const url = new URL(event.request.url);

  // Never intercept the API — no offline data sync in v1.
  if (url.pathname.startsWith("/api/")) return;
  if (event.request.method !== "GET") return;
  if (url.origin !== self.location.origin) return;

  if (event.request.mode === "navigate" || isCodeRequest(url.pathname)) {
    event.respondWith(networkFirst(event.request));
    return;
  }
  event.respondWith(staleWhileRevalidate(event, event.request));
});
