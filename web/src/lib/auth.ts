import { writable } from 'svelte/store';

// Both tokens are kept in localStorage rather than the access-token-in-memory
// + refresh-token-in-an-httpOnly-cookie split noted as the eventual hardening
// target in docs/ARCHITECTURE.md — the backend currently hands back both
// tokens in the JSON body, not a cookie, so this is the straightforward
// client-side counterpart to that as-built API shape, not the final design.
export interface AuthState {
	accessToken: string;
	refreshToken: string;
}

const STORAGE_KEY = 'denizen.auth';

function loadInitial(): AuthState | null {
	if (typeof localStorage === 'undefined') return null;
	const raw = localStorage.getItem(STORAGE_KEY);
	if (!raw) return null;
	try {
		return JSON.parse(raw) as AuthState;
	} catch {
		return null;
	}
}

export const auth = writable<AuthState | null>(loadInitial());

auth.subscribe((value) => {
	if (typeof localStorage === 'undefined') return;
	if (value) {
		localStorage.setItem(STORAGE_KEY, JSON.stringify(value));
	} else {
		localStorage.removeItem(STORAGE_KEY);
	}
});

export function setAuth(state: AuthState) {
	auth.set(state);
}

export function clearAuth() {
	auth.set(null);
}
