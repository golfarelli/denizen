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
	// False only for an item reached through someone else's direct share
	// grant (a file, or one nested under a shared folder) — see
	// internal/handler/item.go's own comment. Always true for anything
	// returned by listTrash, which only ever surfaces the caller's own
	// items; listItems/getItem can return either.
	owned: boolean;
	// Whether the caller may modify this item (rename/move/delete it, and
	// for a folder, create/upload within it) — always true when owned is.
	// When it isn't, this is the caller's own share grant's permission
	// (direct or inherited from a shared ancestor folder).
	can_edit: boolean;
	// Usernames this item has been directly shared with — only ever
	// populated when owned is true (it's the owner's own information to
	// see, not a recipient's) and only some of the time even then; see
	// ShareDialog's own "people with access" section for the full list
	// with revoke buttons, this is just the file browser's row badge.
	shared_with?: string[];
	// Present only on a shortcut ("Aggiungi collegamento") — the real
	// item's id it points at. type/mime_type/name above still describe
	// this row itself (a snapshot taken when the shortcut was created, not
	// re-resolved live from the target — see the backend's own comment on
	// why), but opening/navigating should always go to target_id instead
	// of id.
	target_id?: string;
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

// A direct grant of one item (file or folder) to one specific person, at
// "view" or "edit" permission — distinct from Share above (a link anyone
// holding it can use). A folder grant is inherited by everything nested
// inside it (see internal/service/item.go's resolveGrant). The two
// response shapes below mirror the backend's own two DTOs
// (internal/handler/user_share.go): the owner's "who has access" view
// carries the recipient's name, the recipient's "Shared with me" view
// carries the owner's — never both on the same row, since which one you
// get already tells you which side of the share you're looking at.
export type SharePermission = 'view' | 'edit';

export interface GrantedShare {
	id: string;
	item_id: string;
	shared_with_username: string;
	permission: SharePermission;
	created_at: number;
}

export interface ReceivedShare {
	id: string;
	item_id: string;
	owner_username: string;
	permission: SharePermission;
	created_at: number;
}

// The "pick a person to share with" source — GET /api/v1/users/directory,
// available to any authenticated user (unlike listUsers below, admin-only)
// and trimmed to just enough to identify someone, not the admin-facing
// quota/storage/disabled fields Me carries.
export interface DirectoryUser {
	id: string;
	username: string;
}

// What GET /s/{token}/meta returns — deliberately less than Item, since
// whoever's asking may not have (or need) a Denizen account at all. See
// routes/s/[token]/+page.svelte, the only caller.
export interface PublicShareItem {
	name: string;
	type: 'file' | 'folder';
	size_bytes: number;
	mime_type?: string;
	// Whether *this share* needs a logged-in visitor — not whether the
	// current request satisfied it (reaching this response at all already
	// proves that). Used to decide whether a direct <video src> is safe
	// (no way for it to attach an Authorization header) or whether the
	// bytes have to be fetched as an authenticated blob instead.
	requires_auth: boolean;
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

	// The caller's own most-recently-modified files, across the whole drive
	// — see ItemService.ListRecent's own doc comment for scope (owned
	// files only, folders excluded).
	listRecent: () => req<Item[]>('/api/v1/recent'),

	// Name/content search across the caller's whole drive — see
	// ItemService.Search's own comment for scope and ranking.
	search: (q: string) => req<Item[]>(`/api/v1/search?q=${encodeURIComponent(q)}`),

	createFolder: (name: string, parentId: string | null) =>
		req<Item>('/api/v1/items', jsonInit({ type: 'folder', name, parent_id: parentId })),

	// ext is 'docx' | 'xlsx' | 'pptx' — same endpoint as createFolder, a
	// blank Word/Excel/PowerPoint document instead (see internal/
	// blanktemplates). name must already end with .{ext}.
	createBlankDocument: (ext: string, name: string, parentId: string | null) =>
		req<Item>('/api/v1/items', jsonInit({ type: ext, name, parent_id: parentId })),

	move: (id: string, name: string, parentId: string | null) =>
		req<Item>(`/api/v1/items/${id}`, jsonInit({ name, parent_id: parentId }, 'PATCH')),

	deleteItem: (id: string) => req<void>(`/api/v1/items/${id}`, { method: 'DELETE' }),

	copyItem: (id: string, parentId: string | null) =>
		req<Item>(`/api/v1/items/${id}/copy`, jsonInit({ parent_id: parentId })),

	// id is the real item being pointed at; parentId is where the new
	// shortcut lands (null = the caller's own root) — see Item.target_id's
	// own comment for what makes the response a shortcut.
	createShortcut: (id: string, parentId: string | null) =>
		req<Item>(`/api/v1/items/${id}/shortcut`, jsonInit({ parent_id: parentId })),

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

	// Same blob-over-authenticated-fetch shape as downloadContent above —
	// a thumbnail is small and doesn't need Range streaming, so there's no
	// reason to reach for the content-token/direct-<img>-src pattern
	// getContentToken exists for. Throws on any non-2xx (unsupported file
	// type, none cached/generatable yet, ...) — see Thumbnail.svelte, the
	// only caller, for the fallback-to-icon behavior that expects this.
	getThumbnail: async (id: string): Promise<Blob> => {
		const res = await apiFetch(`/api/v1/items/${id}/thumbnail`);
		if (!res.ok) throw await parseError(res);
		return res.blob();
	},

	// <video>/<audio> can't attach the Authorization header either (same
	// reason downloadContent above fetches bytes itself) — but unlike an
	// image or a PDF, a video benefits from real HTTP Range streaming
	// (instant start, seeking without downloading the whole file first),
	// which only a direct element src gets, not a blob: URL. This mints a
	// token scoped to exactly this item that rides along in that src's
	// query string instead of a header — see internal/token.ContentClaims.
	getContentToken: (id: string) =>
		req<{ token: string; expires_at: number }>(`/api/v1/items/${id}/content-token`, jsonInit({})),

	// Reports "disabled" rather than erroring when no OnlyOffice Document
	// Server is configured (internal/handler/onlyoffice.go) — always safe
	// to call, whether or not the feature is in use on this deployment.
	getOnlyOfficeStatus: () => req<{ enabled: boolean; api_js_url?: string }>('/api/v1/onlyoffice/status'),

	// The full signed editor config DocsAPI.DocEditor expects — see
	// internal/onlyoffice.EditorConfig. Typed loosely here (this app never
	// reads into it, just hands it straight to the editor script).
	// editorType picks OnlyOffice's own "desktop" (full ribbon UI) vs
	// "mobile" (touch-sized) interface — has to be requested here, not
	// decided by the caller after the fact, since it's part of what the
	// server signs (see OnlyOfficeViewer.svelte, which is the one place
	// that actually decides which one to ask for).
	getOnlyOfficeConfig: (id: string, editorType: 'desktop' | 'mobile') =>
		req<Record<string, unknown>>(`/api/v1/items/${id}/onlyoffice-config?type=${editorType}`),

	createShare: (itemId: string, requiresAuth: boolean, expiresAt: number | null) =>
		req<Share>(`/api/v1/items/${itemId}/shares`, jsonInit({ requires_auth: requiresAuth, expires_at: expiresAt })),

	listShares: () => req<Share[]>('/api/v1/shares'),

	revokeShare: (id: string) => req<void>(`/api/v1/shares/${id}`, { method: 'DELETE' }),

	// Direct, per-user shares — see GrantedShare/ReceivedShare's own
	// comment for how the two listing shapes differ.
	createUserShare: (itemId: string, userId: string, permission: SharePermission) =>
		req<GrantedShare>(`/api/v1/items/${itemId}/user-shares`, jsonInit({ user_id: userId, permission })),

	listUserSharesForItem: (itemId: string) => req<GrantedShare[]>(`/api/v1/items/${itemId}/user-shares`),

	// Every direct grant the caller has made, across all of their items —
	// the "My shares" page's counterpart to listShares (token links)
	// below, shown alongside it.
	listMyUserShares: () => req<GrantedShare[]>('/api/v1/user-shares'),

	updateUserSharePermission: (id: string, permission: SharePermission) =>
		req<GrantedShare>(`/api/v1/user-shares/${id}`, jsonInit({ permission }, 'PATCH')),

	revokeUserShare: (id: string) => req<void>(`/api/v1/user-shares/${id}`, { method: 'DELETE' }),

	listSharedWithMe: () => req<ReceivedShare[]>('/api/v1/shared-with-me'),

	listUserDirectory: () => req<DirectoryUser[]>('/api/v1/users/directory'),

	// The public /s/{token}/* routes back routes/s/[token]/+page.svelte,
	// reachable with no Denizen account at all — but still going through
	// req()/apiFetch (not a bare fetch) rather than publicReq, so a visitor
	// who *does* happen to be logged in gets their Authorization header
	// attached automatically, which is what actually satisfies a
	// requires_auth share for them (see internal/service/share.go's
	// Resolve — "authenticated" there just means *some* valid access
	// token, not ownership of anything).
	getShareMeta: (token: string) => req<PublicShareItem>(`/s/${token}/meta`),

	downloadSharedContent: async (token: string): Promise<Blob> => {
		const res = await apiFetch(`/s/${token}/content`);
		if (!res.ok) throw await parseError(res);
		return res.blob();
	},

	// Admin-only — enforced server-side by middleware.RequireAdmin; a
	// non-admin calling these just gets a 403 back, same as any other route.
	listUsers: () => req<Me[]>('/api/v1/users'),

	updateUser: (id: string, changes: { quota_bytes?: number; disabled?: boolean }) =>
		req<Me>(`/api/v1/users/${id}`, jsonInit(changes, 'PATCH')),

	createInvite: (quotaBytes: number | null) =>
		req<{ code: string; expires_at: number }>('/api/v1/invites', jsonInit({ quota_bytes: quotaBytes }))
};
