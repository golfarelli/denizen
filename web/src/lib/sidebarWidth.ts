// The desktop sidebar's user-adjustable width (drag handle on its right
// edge — see routes/+layout.svelte and .sidebar-resizer in app.css).
//
// Remembered for the current browser session only (sessionStorage), on
// purpose unlike every other persisted preference in this app (view mode,
// sort, language — see persistedState.ts, all localStorage): it's a
// "for now" layout tweak, not a preference to carry into next week's
// visit. Closing the tab/browser forgets it and the default width returns.

/** Below this, folder names and the tree's indentation stop being readable. */
export const SIDEBAR_MIN = 200;
/** Above this the sidebar starts crowding the file list it's secondary to. */
export const SIDEBAR_MAX = 420;

const STORAGE_KEY = 'denizen.sidebarWidth';

// The CSS custom property app.css's desktop-only rule reads (falling back
// to --sidebar-width when unset). Deliberately not --sidebar-width itself:
// the drawer (mobile) width must stay whatever app.css says, even if this
// was set on a desktop-sized window that was later shrunk below the
// breakpoint.
const CSS_VAR = '--sidebar-width-user';

export function clampSidebarWidth(px: number): number {
	return Math.min(SIDEBAR_MAX, Math.max(SIDEBAR_MIN, Math.round(px)));
}

/** The width saved earlier in this session, or null if none (or unusable). */
export function loadSidebarWidth(): number | null {
	try {
		const raw = sessionStorage.getItem(STORAGE_KEY);
		if (raw === null) return null;
		const px = Number(raw);
		return Number.isFinite(px) ? clampSidebarWidth(px) : null;
	} catch {
		return null; // sessionStorage unavailable (e.g. blocked by browser settings)
	}
}

export function saveSidebarWidth(px: number): void {
	try {
		sessionStorage.setItem(STORAGE_KEY, String(clampSidebarWidth(px)));
	} catch {
		// Not being able to remember it isn't worth surfacing.
	}
}

/** Applies px (clamped) live, without saving — used while a drag is in flight. */
export function applySidebarWidth(px: number): number {
	const clamped = clampSidebarWidth(px);
	document.documentElement.style.setProperty(CSS_VAR, `${clamped}px`);
	return clamped;
}
