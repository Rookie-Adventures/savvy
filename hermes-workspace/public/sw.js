// Hermes Workspace Service Worker
// Network-only PWA registration: enables installability without caching app assets.
// This avoids stale bundles after PM2/Vite preview deploys while keeping iOS/Chrome
// standalone launches on the normal live application shell.
//
// ponytail: no 'fetch' listener on purpose. A fetch handler that never calls
// event.respondWith() (network-only passthrough) triggers the Chrome warning
// "Fetch event handler is recognized as no-op" and adds per-navigation overhead.
// Omitting the listener entirely is functionally identical (requests fall through
// to the network stack) and keeps PWA installability intact (only install/activate
// + a registered SW + manifest are required).

self.addEventListener('install', () => {
  self.skipWaiting()
})

self.addEventListener('activate', (event) => {
  event.waitUntil(
    caches
      .keys()
      .then((names) => Promise.all(names.map((name) => caches.delete(name))))
      .then(() => self.clients.claim()),
  )
})
