// Denizen's service worker. Two jobs, both requiring a real SW (not just a
// manifest): (1) intercept the OS share-sheet's POST to /share-target so a
// shared file can be handed to the SPA for a *real*, authenticated upload
// (see below for why this can't just be a normal server route), and (2) a
// light same-origin asset cache, mostly so there's an active fetch handler
// at all — that's still what most browsers check for PWA installability.
//
// Bump this on any change to what/how this file caches — it's the cache
// name, so a bump makes activate() below throw away everything from the
// previous version instead of mixing old and new entries.
const SHELL_CACHE = 'denizen-shell-v1';
const SHARED_FILES_CACHE = 'denizen-shared-files';

self.addEventListener('install', () => {
	self.skipWaiting();
});

self.addEventListener('activate', (event) => {
	event.waitUntil(
		(async () => {
			const keys = await caches.keys();
			await Promise.all(
				keys
					.filter((key) => key !== SHELL_CACHE && key !== SHARED_FILES_CACHE)
					.map((key) => caches.delete(key))
			);
			await self.clients.claim();
		})()
	);
});

self.addEventListener('fetch', (event) => {
	const url = new URL(event.request.url);

	if (event.request.method === 'POST' && url.pathname === '/share-target') {
		event.respondWith(handleShareTarget(event.request));
		return;
	}

	if (event.request.method !== 'GET') return; // e.g. /api/* writes — always straight to the network, never cached

	const isShell =
		url.origin === self.location.origin &&
		(url.pathname === '/' ||
			url.pathname.startsWith('/_app/') ||
			url.pathname.startsWith('/icons/') ||
			url.pathname === '/manifest.json' ||
			url.pathname === '/favicon.svg');
	if (isShell) {
		event.respondWith(cacheFirstOrNetwork(event.request));
	}
	// Everything else — API calls, /s/* share links, other SPA routes on a
	// fresh navigation — is left alone entirely and goes straight to the
	// network, exactly as if there were no service worker.
});

// Files a real person shares from another app arrive here as a browser
// POST, not a fetch() call this app's own code made — there's no chance to
// attach the Authorization header the actual upload endpoint needs (see
// lib/api.ts), and this app deliberately doesn't use cookie-based auth
// that a plain POST would carry automatically. So instead of trying to
// authenticate this request at all, it's stashed in the Cache Storage API
// (private per-origin, not the shared filesystem) and hydrated back into a
// real File object by /share-target's own page (share-target/+page.svelte)
// once the browser navigates there — where it's a normal authenticated
// client running the app's existing startUpload(), same as any drag-
// and-drop upload.
async function handleShareTarget(request) {
	const formData = await request.formData();
	const files = formData.getAll('files').filter((f) => f instanceof File);

	// crypto.randomUUID() is safe here specifically because a service
	// worker only ever runs in a secure context in the first place (its
	// own registration requires HTTPS or localhost) — unlike the identical-
	// looking call this app once had in its upload progress panel, which
	// ran on an ordinary page that a plain-HTTP LAN deployment could reach.
	const shareId = crypto.randomUUID();
	const cache = await caches.open(SHARED_FILES_CACHE);

	for (let i = 0; i < files.length; i++) {
		const file = files[i];
		await cache.put(
			shareKey(shareId, i),
			new Response(file, {
				headers: {
					'Content-Type': file.type || 'application/octet-stream',
					'X-Denizen-Filename': encodeURIComponent(file.name)
				}
			})
		);
	}
	await cache.put(shareKey(shareId, 'count'), new Response(String(files.length)));

	return Response.redirect(`/share-target?share_id=${shareId}`, 303);
}

function shareKey(shareId, part) {
	return `/__shared/${shareId}/${part}`;
}

async function cacheFirstOrNetwork(request) {
	const cache = await caches.open(SHELL_CACHE);

	// index.html/manifest.json can change shape across a rebuild (new
	// hashed asset filenames), so these two are network-first — cache is
	// only the offline fallback, never preferred over a live response.
	const url = new URL(request.url);
	if (url.pathname === '/' || url.pathname === '/manifest.json') {
		try {
			const fresh = await fetch(request);
			cache.put(request, fresh.clone());
			return fresh;
		} catch {
			return (await cache.match(request)) ?? Response.error();
		}
	}

	// Everything else under the shell (/_app/*, /icons/*) is Vite's
	// content-hashed, immutable output — a given URL never changes body,
	// so cache-first is always correct, not just faster.
	const cached = await cache.match(request);
	if (cached) return cached;
	const fresh = await fetch(request);
	cache.put(request, fresh.clone());
	return fresh;
}
