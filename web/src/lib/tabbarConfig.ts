import { writable } from 'svelte/store';
import { loadPersisted, savePersisted } from './persistedState';

export type TabKey = 'recent' | 'favorites' | 'shares' | 'shared-with-me' | 'trash';

// Everything the tab bar (routes/+layout.svelte) and the settings page can
// place in positions 2-4 — Home is fixed in position 1 and isn't part of
// this pool. labelKey reuses the sidebar's own nav.* i18n keys, no
// duplicate strings.
export const TAB_POOL: Record<TabKey, { href: string; labelKey: string }> = {
	recent: { href: '/recent', labelKey: 'nav.recent' },
	favorites: { href: '/favorites', labelKey: 'nav.favorites' },
	shares: { href: '/shares', labelKey: 'nav.myShares' },
	'shared-with-me': { href: '/shared-with-me', labelKey: 'nav.sharedWithMe' },
	trash: { href: '/trash', labelKey: 'nav.trash' }
};

// The pool's own keys, in the fixed order used both by the settings page's
// dropdown options and by the sidebar drawer's link order (routes/
// +layout.svelte) — one ordering, not two to keep in sync.
export const ALL_KEYS = Object.keys(TAB_POOL) as TabKey[];

const STORAGE_KEY = 'denizen.tabbar';

// Matches the tab bar's pre-customization fixed order (feature/bottom-tabbar)
// so nobody's layout changes just from updating to a build with this feature.
const DEFAULT_TABS: TabKey[] = ['shares', 'shared-with-me', 'trash'];

// A store (not plain persistedState load/save) because, unlike a page's own
// sort field, this is read by +layout.svelte — which stays mounted across
// navigation — while written by routes/settings/+page.svelte: two different
// components need to see the same live value. Same pattern as lib/viewMode.ts.
export const tabbarConfig = writable<TabKey[]>(loadPersisted<TabKey[]>(STORAGE_KEY, DEFAULT_TABS));

tabbarConfig.subscribe((value) => savePersisted(STORAGE_KEY, value));
