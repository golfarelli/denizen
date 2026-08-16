// Reads/writes a JSON value in localStorage — the "remember it next time"
// half of a $state rune, paired with the caller's own $effect that calls
// savePersisted on change (see routes/+page.svelte's sortField/
// sortDirection for the pattern, added 2026-08-16: sort used to reset on
// every visit, unlike lib/viewMode.ts's own list/grid toggle, which
// already persisted this same way). Not a store — the value itself stays
// an ordinary local $state, this only handles the initial read and the
// write-back.
export function loadPersisted<T>(key: string, fallback: T): T {
	if (typeof localStorage === 'undefined') return fallback;
	const raw = localStorage.getItem(key);
	if (raw === null) return fallback;
	try {
		return JSON.parse(raw) as T;
	} catch {
		return fallback;
	}
}

export function savePersisted<T>(key: string, value: T): void {
	if (typeof localStorage === 'undefined') return;
	localStorage.setItem(key, JSON.stringify(value));
}
