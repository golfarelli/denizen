// Registers static/sw.js (see its own doc comment for what it does).
// serviceWorker registration itself is a secure-context-only browser API
// (HTTPS or localhost) — on a plain-HTTP LAN deployment this silently
// stays undefined, so installability/share-target just aren't available
// there, but the rest of the app is entirely unaffected. Best-effort by
// design: a registration failure here must never break page load.
export function registerServiceWorker() {
	if (!('serviceWorker' in navigator)) return;
	navigator.serviceWorker.register('/sw.js').catch(() => {});
}
