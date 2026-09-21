/* Minimal SW so Chrome can install MikroLLM as an app. Network-first, no offline HTML. */
self.addEventListener("install", function (e) {
  self.skipWaiting();
});
self.addEventListener("activate", function (e) {
  e.waitUntil(self.clients.claim());
});
self.addEventListener("fetch", function () {});
