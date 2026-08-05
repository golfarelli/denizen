import { get } from 'svelte/store';
import { goto } from '$app/navigation';
import { auth, setAuth, clearAuth } from './auth';

/** Mirrors the backend's {"error": {code, message}} envelope — see
 * internal/apperr/apperr.go and internal/httpio/response.go. */
export class ApiError extends Error {
	constructor(
		public status: number,
		public code: string,
		message: string
	) {
		super(message);
	}
}

async function parseError(res: Response): Promise<ApiError> {
	const body = await res.json().catch(() => null);
	return new ApiError(res.status, body?.error?.code ?? 'unknown', body?.error?.message ?? res.statusText);
}

async function rawFetch(path: string, init: RequestInit = {}): Promise<Response> {
	const state = get(auth);
	const headers = new Headers(init.headers);
	if (state?.accessToken) {
		headers.set('Authorization', `Bearer ${state.accessToken}`);
	}
	return fetch(path, { ...init, headers });
}

/** Exported for internal/upload.ts's tus client too — a large upload can
 * easily outlive the access token's TTL (default 15 minutes), so it needs
 * to be able to refresh mid-upload the same way apiFetch does between two
 * ordinary requests. */
export async function refreshAccessToken(): Promise<boolean> {
	const state = get(auth);
	if (!state?.refreshToken) return false;
	const res = await fetch('/api/v1/auth/refresh', {
		method: 'POST',
		headers: { 'Content-Type': 'application/json' },
		body: JSON.stringify({ refresh_token: state.refreshToken })
	});
	if (!res.ok) return false;
	const tokens = await res.json();
	setAuth({ accessToken: tokens.access_token, refreshToken: tokens.refresh_token });
	return true;
}

/** Fetch with the access token attached, transparently refreshing it once
 * on a 401 before giving up and sending the user back to the login page. */
export async function apiFetch(path: string, init: RequestInit = {}): Promise<Response> {
	let res = await rawFetch(path, init);
	if (res.status === 401 && get(auth)) {
		if (await refreshAccessToken()) {
			res = await rawFetch(path, init);
		} else {
			clearAuth();
			await goto('/login');
		}
	}
	return res;
}

async function req<T>(path: string, init: RequestInit = {}): Promise<T> {
	const res = await apiFetch(path, init);
	if (!res.ok) throw await parseError(res);
	if (res.status === 204) return undefined as T;
	return (await res.json()) as T;
}

async function publicReq<T>(path: string, init: RequestInit = {}): Promise<T> {
	const res = await fetch(path, init);
	if (!res.ok) throw await parseError(res);
	if (res.status === 204) return undefined as T;
	return (await res.json()) as T;
}

function jsonInit(body: unknown, method = 'POST'): RequestInit {
	return { method, headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) };
}

export interface Item {
	id: string;
	parent_id: string | null;
	name: string;
	type: 'file' | 'folder';
	size_bytes: number;
	mime_type?: string;
	created_at: number;
	updated_at: number;
	deleted_at?: number;
}

export interface Me {
	id: string;
	username: string;
	is_admin: boolean;
	quota_bytes: number;
	storage_used_bytes: number;
	disabled: boolean;
}

interface TokenPair {
	access_token: string;
	refresh_token: string;
}

export interface Share {
	id: string;
	item_id: string;
	requires_auth: boolean;
	expires_at?: number;
	created_at: number;
	// token/url are only ever present in the response to createShare — the
	// backend never persists the raw token, only its hash (see
	// docs/ARCHITECTURE.md), so there's no "look it up again later".
	token?: string;
	url?: string;
}

export const api = {
	login: (username: string, password: string) =>
		publicReq<TokenPair>('/api/v1/auth/login', jsonInit({ username, password })),

	register: (inviteCode: string, username: string, password: string) =>
		publicReq<{ id: string; username: string; is_admin: boolean }>(
			'/api/v1/auth/register',
			jsonInit({ invite_code: inviteCode, username, password })
		),

	logout: (refreshToken: string) => publicReq<void>('/api/v1/auth/logout', jsonInit({ refresh_token: refreshToken })),

	me: () => req<Me>('/api/v1/me'),

	listItems: (parentId: string | null) => {
		const qs = parentId ? `?parent_id=${encodeURIComponent(parentId)}` : '';
		return req<Item[]>(`/api/v1/items${qs}`);
	},

	getItem: (id: string) => req<Item>(`/api/v1/items/${id}`),

	createFolder: (name: string, parentId: string | null) =>
		req<Item>('/api/v1/items', jsonInit({ type: 'folder', name, parent_id: parentId })),

	move: (id: string, name: string, parentId: string | null) =>
		req<Item>(`/api/v1/items/${id}`, jsonInit({ name, parent_id: parentId }, 'PATCH')),

	deleteItem: (id: string) => req<void>(`/api/v1/items/${id}`, { method: 'DELETE' }),

	copyItem: (id: string, parentId: string | null) =>
		req<Item>(`/api/v1/items/${id}/copy`, jsonInit({ parent_id: parentId })),

	listTrash: () => req<Item[]>('/api/v1/trash'),

	restoreItem: (id: string) => req<Item>(`/api/v1/items/${id}/restore`, jsonInit({})),

	permanentlyDeleteItem: (id: string) => req<void>(`/api/v1/trash/${id}`, { method: 'DELETE' }),

	// A plain <a href> can't carry the Authorization header a download
	// needs, so callers fetch the response themselves (as a blob) instead
	// of following a link — see the download handler in routes/+page.svelte.
	downloadContent: async (id: string): Promise<Blob> => {
		const res = await apiFetch(`/api/v1/items/${id}/content`);
		if (!res.ok) throw await parseError(res);
		return res.blob();
	},

	createShare: (itemId: string, requiresAuth: boolean, expiresAt: number | null) =>
		req<Share>(`/api/v1/items/${itemId}/shares`, jsonInit({ requires_auth: requiresAuth, expires_at: expiresAt })),

	listShares: () => req<Share[]>('/api/v1/shares'),

	revokeShare: (id: string) => req<void>(`/api/v1/shares/${id}`, { method: 'DELETE' }),

	// Admin-only — enforced server-side by middleware.RequireAdmin; a
	// non-admin calling these just gets a 403 back, same as any other route.
	listUsers: () => req<Me[]>('/api/v1/users'),

	updateUser: (id: string, changes: { quota_bytes?: number; disabled?: boolean }) =>
		req<Me>(`/api/v1/users/${id}`, jsonInit(changes, 'PATCH')),

	createInvite: (quotaBytes: number | null) =>
		req<{ code: string; expires_at: number }>('/api/v1/invites', jsonInit({ quota_bytes: quotaBytes }))
};
