import { writable } from 'svelte/store';

// Mobile-only: whether the sidebar nav is open as a slide-in drawer. On
// desktop the sidebar is always visible as a static column (see app.css's
// breakpoint) and nothing here is ever read — this only matters below the
// breakpoint, where the sidebar becomes an overlay instead of taking up
// permanent width.
export const sidebarOpen = writable(false);

export function closeSidebar() {
	sidebarOpen.set(false);
}
