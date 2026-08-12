import { writable } from 'svelte/store';

// Folder ids the sidebar's own folder tree (FolderTreeItem.svelte) should
// auto-expand — set from routes/+layout.svelte whenever the current folder
// changes (see its own ancestorChain walk), read by every mounted tree node
// to decide whether to expand itself independently of the user's own
// click-to-toggle state. A plain id set, not an ordered path, since a node
// only ever needs "am I on the way to wherever the user is right now".
export const autoExpandFolderIds = writable<Set<string>>(new Set());

// Bumped by every place that creates/renames/moves/deletes/restores a
// folder (routes/+page.svelte's load() and routes/trash/+page.svelte's own
// load() — the one function each of those pages already calls after every
// such mutation to refresh its own view) — a plain version counter rather
// than tracking which parent was actually affected, since the tree only
// ever refetches a node that's currently expanded and visible (see
// FolderTreeItem.svelte/+layout.svelte's own effects), so the odd redundant
// refetch of an unrelated open branch is cheap and not worth precise
// invalidation bookkeeping for.
export const treeVersion = writable(0);

export function invalidateTree() {
	treeVersion.update((v) => v + 1);
}
