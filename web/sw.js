// Simple Service Worker for offline caching
const CACHE_NAME = 'rms-cache-v1';
const PRECACHE_URLS = [
  '/',
  '/index.html',
  '/css/styles.css',
  '/css/video-js.css',
  '/js/app.js',
  '/js/video.min.js',
  '/favicon.ico',
];

self.addEventListener('install', e => {
  e.waitUntil(
    caches.open(CACHE_NAME).then(cache => cache.addAll(PRECACHE_URLS))
  );
  self.skipWaiting();
});

self.addEventListener('activate', e => {
  e.waitUntil(
    caches.keys().then(names => Promise.all(
      names.filter(n => n !== CACHE_NAME).map(n => caches.delete(n))
    ))
  );
  self.clients.claim();
});

self.addEventListener('fetch', e => {
  const request = e.request;
  // Only handle GET requests
  if (request.method !== 'GET') return;
  e.respondWith(
    caches.match(request).then(cached => cached || fetch(request))
  );
});
