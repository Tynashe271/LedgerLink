// BranchLedger service worker: caches the installed application shell so the
// pages listed below still load offline, per BranchLedger_Architecture.pdf's
// offline path ("the service worker serves the previously installed
// application shell") and System Documentation 5.1 ("Use a cached browser
// application shell ... Offline forms must already be cached").
//
// Scope is the whole origin (served from /, not /js/) so it can control
// every page below. It only ever caches GET navigations and this app's own
// static JS — /api/* requests always go straight to the network and are
// never cached or queued here. Writing while offline is the page-level
// IndexedDB outbox's job (public/js/offline/sync.js), not the service
// worker's; mixing the two would hide failures behind a cache that silently
// "succeeds" a POST no server ever saw.

const CACHE_NAME = 'branchledger-shell-v2';

// Every screen a signed-in user can land on. A route added here without a
// visit while online won't be cached yet; SHELL_URLS covers first-run
// installability for the pages that matter most, and the fetch handler
// below caches every other page the user actually visits.
const SHELL_URLS = [
    '/login',
    '/dashboard',
    '/sales',
    '/expenses',
    '/purchases',
    '/transfers',
    '/closes',
    '/approvals',
    '/sync',
    '/products',
    '/customers',
    '/js/offline/db.js',
    '/js/offline/sync.js',
    '/manifest.webmanifest',
];

self.addEventListener('install', (event) => {
    event.waitUntil(
        caches.open(CACHE_NAME)
            .then((cache) => Promise.allSettled(SHELL_URLS.map((url) => cache.add(url))))
            .then(() => self.skipWaiting())
    );
});

self.addEventListener('activate', (event) => {
    event.waitUntil(
        caches.keys()
            .then((names) => Promise.all(names.filter((n) => n !== CACHE_NAME).map((n) => caches.delete(n))))
            .then(() => self.clients.claim())
    );
});

self.addEventListener('fetch', (event) => {
    const url = new URL(event.request.url);

    // Never intercept the API gateway or cross-origin requests — these must
    // always reach the network (or fail visibly) so the outbox's own
    // pending/failed accounting stays accurate.
    if (url.origin !== self.location.origin || url.pathname.startsWith('/api/')) {
        return;
    }

    if (event.request.mode === 'navigate' || event.request.method === 'GET') {
        event.respondWith(networkFirstWithCacheFallback(event.request));
    }
});

async function networkFirstWithCacheFallback(request) {
    const cache = await caches.open(CACHE_NAME);
    try {
        const response = await fetch(request);
        // Only cache successful responses — never an error page. A cached
        // page still carries whatever was rendered for the signed-in user at
        // fetch time (company name, CSRF token), which is consistent with
        // this app's existing one-user-per-device model ("Never share
        // accounts," User Manual) but means a shared/public device should
        // not use the installed offline app across different accounts.
        if (response.ok) {
            cache.put(request, response.clone());
        }
        return response;
    } catch (err) {
        const cached = await cache.match(request);
        if (cached) return cached;
        throw err;
    }
}
