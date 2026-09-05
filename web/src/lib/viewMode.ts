import { writable } from 'svelte/store';

export type ViewMode = 'list' | 'grid';

const STORAGE_KEY = 'denizen.viewMode';

function loadInitial(): ViewMode {
	if (typeof localStorage === 'undefined') return 'list';
	const raw = localStorage.getItem(STORAGE_KEY);
	return raw === 'grid' ? 'grid' : 'list';
}

// The file browser's own list/grid toggle (routes/+page.svelte) —
// persisted across sessions (unlike sort field/direction, which resets
// each visit) since switching to grid and having it silently revert back
// would be more annoying than useful for someone who actually prefers it.
export const viewMode = writable<ViewMode>(loadInitial());

viewMode.subscribe((value) => {
	if (typeof localStorage === 'undefined') return;
	localStorage.setItem(STORAGE_KEY, value);
});
