<script lang="ts">
	import type { TabKey } from './tabbarConfig';

	// Same path data as the sidebar drawer's own hardcoded links for these
	// five destinations — pulled out here only because the bottom tab bar
	// now renders them from a user-configurable list (lib/tabbarConfig.ts)
	// instead of four fixed <a> elements, so the icon needs to be picked at
	// runtime. The drawer's own links are left inline; unlike the tab bar
	// they're still hardcoded, so there's nothing there to dedupe against.
	let { kind }: { kind: TabKey } = $props();
</script>

{#if kind === 'recent'}
	<svg width="22" height="22" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" aria-hidden="true">
		<circle cx="12" cy="12" r="9" />
		<path d="M12 7v5l3.5 2" stroke-linecap="round" stroke-linejoin="round" />
	</svg>
{:else if kind === 'favorites'}
	<svg width="22" height="22" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" aria-hidden="true">
		<path d="M12 3.5l2.7 5.9 6.3.7-4.7 4.4 1.3 6.2-5.6-3.2-5.6 3.2 1.3-6.2-4.7-4.4 6.3-.7L12 3.5Z" stroke-linejoin="round" />
	</svg>
{:else if kind === 'shares'}
	<svg width="22" height="22" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" aria-hidden="true">
		<circle cx="6" cy="12" r="2.2" />
		<circle cx="17" cy="6" r="2.2" />
		<circle cx="17" cy="18" r="2.2" />
		<path d="M8 10.8 15 7M8 13.2 15 17" stroke-linecap="round" />
	</svg>
{:else if kind === 'shared-with-me'}
	<svg width="22" height="22" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" aria-hidden="true">
		<circle cx="12" cy="8" r="3.2" />
		<path d="M5 20c0-3.9 3.1-7 7-7s7 3.1 7 7" stroke-linecap="round" stroke-linejoin="round" />
	</svg>
{:else if kind === 'trash'}
	<svg width="22" height="22" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" aria-hidden="true">
		<path
			d="M5 7h14M9 7V5a1 1 0 0 1 1-1h4a1 1 0 0 1 1 1v2M7 7l1 13a1 1 0 0 0 1 1h6a1 1 0 0 0 1-1l1-13"
			stroke-linecap="round"
			stroke-linejoin="round"
		/>
	</svg>
{/if}
