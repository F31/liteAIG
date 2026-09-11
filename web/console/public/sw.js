const CACHE_NAME = "liteaig-shell-v4";
const STATIC_ASSETS = ["/", "/index.html"];

self.addEventListener("install", (event) => {
  event.waitUntil(
    caches.open(CACHE_NAME).then((cache) => cache.addAll(STATIC_ASSETS)),
  );
  self.skipWaiting();
});

self.addEventListener("activate", (event) => {
  event.waitUntil(
    caches
      .keys()
      .then((keys) =>
        Promise.all(
          keys
            .filter((key) => key !== CACHE_NAME)
            .map((key) => caches.delete(key)),
        ),
      ),
  );
  self.clients.claim();
});

// Cache only static navigations; never cache Admin API, Usage, Prompt,
// Response, Secret, or Security Event responses.
self.addEventListener("fetch", (event) => {
  const url = new URL(event.request.url);
  if (
    !["http:", "https:"].includes(url.protocol) ||
    url.origin !== self.location.origin
  ) {
    return;
  }
  if (url.pathname.startsWith("/api/")) {
    return;
  }
  if (event.request.method !== "GET") {
    return;
  }
  if (event.request.mode !== "navigate" && url.pathname !== "/index.html") {
    return;
  }
  event.respondWith(
    fetch(event.request)
      .then((response) => {
        const copy = response.clone();
        caches.open(CACHE_NAME).then((cache) => cache.put(event.request, copy));
        return response;
      })
      .catch(() => caches.match(event.request)),
  );
});
