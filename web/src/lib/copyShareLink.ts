import { api } from './api';

// The "Copy link" quick action (file browser + file preview page's own
// kebab menus) — creates a fresh public share (no login required, never
// expires: the same quick defaults Drive's own "Copy link" uses) and
// copies its URL straight to the clipboard, skipping ShareDialog's own
// form entirely. That dialog is still there for anyone who actually wants
// requires_auth/an expiry — this is purely the fast path for the common
// case of "just give me a link".
//
// Always mints a *new* share rather than reusing an existing one for the
// same item: the raw token is only ever shown once, right after creation
// (see docs/ARCHITECTURE.md on why), so there's no earlier link this could
// find and reuse even if one already exists.
//
// `copied: false` (not a thrown error) is the Clipboard API's own failure
// mode — it needs a secure context (https or localhost), quite possibly
// unavailable for a home server reached over plain http on the LAN (see
// ShareDialog's own identical comment on this). The share still got
// created either way, so the caller can fall back to showing `url` itself
// rather than losing it.
export async function copyShareLink(itemId: string): Promise<{ url: string; copied: boolean }> {
	const share = await api.createShare(itemId, false, null);
	const url = `${location.origin}${share.url}`;
	try {
		await navigator.clipboard.writeText(url);
		return { url, copied: true };
	} catch {
		return { url, copied: false };
	}
}
