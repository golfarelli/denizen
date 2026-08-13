// Advanced search's own filter state and pure logic — shared between
// routes/+page.svelte (where the filtering actually happens, entirely
// client-side over the search results already fetched — see its own
// comment on why) and AdvancedSearchDialog.svelte (the modal that sets
// this state).
import type { FileKind } from './fileKind';

export type TypeFilter = 'any' | FileKind;
export type DateFilter = 'any' | 'today' | 'week' | 'month' | 'year';
export type OwnerFilter = 'any' | 'mine' | 'shared';

export function isSearchFilterActive(type: TypeFilter, date: DateFilter, owner: OwnerFilter): boolean {
	return type !== 'any' || date !== 'any' || owner !== 'any';
}

// Unix-seconds cutoff for a date filter — items updated before this are
// excluded. null for 'any' (no cutoff, nothing filtered on date).
export function dateFilterCutoff(filter: DateFilter): number | null {
	if (filter === 'any') return null;
	const cutoff = new Date();
	if (filter === 'today') {
		cutoff.setHours(0, 0, 0, 0);
	} else if (filter === 'week') {
		cutoff.setDate(cutoff.getDate() - 7);
	} else if (filter === 'month') {
		cutoff.setMonth(cutoff.getMonth() - 1);
	} else if (filter === 'year') {
		cutoff.setMonth(0, 1);
		cutoff.setHours(0, 0, 0, 0);
	}
	return Math.floor(cutoff.getTime() / 1000);
}
