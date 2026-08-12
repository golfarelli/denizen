import { api, type Item } from './api';

// Walks from `folderId` up through parent_id to the root, returning the
// chain root-first. Stops (without throwing) the moment an ancestor can't
// be fetched — a shared subfolder nested inside parts of the owner's drive
// we were never granted the rest of (see ItemService.resolveGrant) — so a
// caller always gets back everything it *could* see, never a hard failure.
// Shared by the breadcrumb (routes/+page.svelte) and the sidebar folder
// tree's auto-expand (routes/+layout.svelte), which both need this same
// walk for different reasons (crumb labels vs. which node ids to open).
export async function walkAncestors(folderId: string): Promise<Item[]> {
	const chain: Item[] = [];
	let current: string | null = folderId;
	while (current) {
		let item: Item;
		try {
			item = await api.getItem(current);
		} catch {
			break;
		}
		chain.unshift(item);
		current = item.parent_id;
	}
	return chain;
}
