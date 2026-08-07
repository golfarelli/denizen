import type { Item } from './api';

export type SortField = 'name' | 'modified' | 'size';
export type SortDirection = 'asc' | 'desc';

// Shared by the file browser and Trash (both use the same Name/Modified/
// Size column shape — see app.css's .item-row grid) so "click a column to
// sort by it" behaves identically in both places. Folders always sort
// before files regardless of field/direction — the same convention Drive,
// Nextcloud, and every desktop file manager use — then the chosen field
// breaks ties within each group.
export function sortItems<T extends Item>(items: T[], field: SortField, direction: SortDirection): T[] {
	const sign = direction === 'asc' ? 1 : -1;
	return [...items].sort((a, b) => {
		if (a.type !== b.type) return a.type === 'folder' ? -1 : 1;
		switch (field) {
			case 'name':
				return sign * a.name.localeCompare(b.name, undefined, { numeric: true, sensitivity: 'base' });
			case 'modified':
				return sign * (a.updated_at - b.updated_at);
			case 'size':
				return sign * (a.size_bytes - b.size_bytes);
		}
	});
}
