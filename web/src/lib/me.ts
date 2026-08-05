import { writable } from 'svelte/store';
import { api, type Me } from './api';

// Populated once after auth is set (see +layout.svelte) and cleared on
// logout — mainly so the UI knows whether to show admin-only affordances
// (the "Admin" nav link, the /admin page's own content) without decoding
// the JWT client-side. This is a UX convenience only: the actual
// authorization decision is made server-side (middleware.RequireAdmin) on
// every request regardless of what this store says.
export const me = writable<Me | null>(null);

export async function refreshMe(): Promise<void> {
	try {
		me.set(await api.me());
	} catch {
		me.set(null);
	}
}

export function clearMe() {
	me.set(null);
}
