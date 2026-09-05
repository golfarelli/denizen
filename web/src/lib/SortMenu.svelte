<script lang="ts" generics="T extends string">
	import SortArrow from './SortArrow.svelte';
	import { t } from './i18n';

	// A mobile-reachable way to change sort field/direction — the column
	// headers this mirrors (routes/+page.svelte etc.) are the *primary*
	// way to sort, but .item-list-header is display:none below 640px (see
	// app.css's own mobile media query, from well before sorting existed —
	// it hid the header entirely for the two-line mobile row layout), which
	// silently took the header's own sort buttons down with it. This is
	// the fix: a toolbar button reachable regardless of viewport width,
	// reusing the exact same .dropdown-menu component (and its mobile
	// fullscreen-sheet behavior, see app.css) every row's own kebab menu
	// already uses.
	//
	// Generic over T (each page's own SortField union, e.g. 'name' |
	// 'modified' | 'size') rather than a plain string, so bind:sortField
	// stays type-safe against the page's own $state<SortField> instead of
	// widening it to string.
	let {
		fields,
		sortField = $bindable(),
		sortDirection = $bindable()
	}: {
		fields: { key: T; label: string }[];
		sortField: T;
		sortDirection: 'asc' | 'desc';
	} = $props();

	let open = $state(false);

	// The trigger itself now shows what it's sorting by, not just a generic
	// icon — always one of fields (sortField always starts as one of the
	// keys the caller passes in), so the fallback here is only ever a
	// type-narrowing safety net, not a real runtime case.
	let currentLabel = $derived(fields.find((f) => f.key === sortField)?.label ?? '');

	function choose(key: T) {
		if (sortField === key) {
			sortDirection = sortDirection === 'asc' ? 'desc' : 'asc';
		} else {
			sortField = key;
			sortDirection = 'asc';
		}
		open = false;
	}

	function closeMenu() {
		open = false;
	}

	$effect(() => {
		if (!open) return;
		function handlePointerDown() {
			closeMenu();
		}
		function handleKeydown(e: KeyboardEvent) {
			if (e.key === 'Escape') closeMenu();
		}
		window.addEventListener('click', handlePointerDown);
		window.addEventListener('keydown', handleKeydown);
		return () => {
			window.removeEventListener('click', handlePointerDown);
			window.removeEventListener('keydown', handleKeydown);
		};
	});
</script>

<div class="sort-menu">
	<button
		class="btn sort-menu-trigger"
		aria-label={$t('common.sortBy')}
		aria-haspopup="true"
		aria-expanded={open}
		onclick={(e) => {
			e.stopPropagation();
			open = !open;
		}}
	>
		<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" aria-hidden="true">
			<path d="M4 6h16M4 12h10M4 18h6" stroke-linecap="round" />
		</svg>
		{currentLabel}
		<SortArrow direction={sortDirection} />
	</button>
	{#if open}
		<!-- svelte-ignore a11y_no_static_element_interactions -->
		<!-- svelte-ignore a11y_click_events_have_key_events -->
		<div class="menu-backdrop" onclick={closeMenu}></div>
		<!-- svelte-ignore a11y_no_static_element_interactions -->
		<!-- svelte-ignore a11y_interactive_supports_focus -->
		<!-- svelte-ignore a11y_click_events_have_key_events -->
		<div class="dropdown-menu" onclick={(e) => e.stopPropagation()} role="menu">
			{#each fields as field (field.key)}
				<button role="menuitem" onclick={() => choose(field.key)}>
					{field.label}
					{#if sortField === field.key}<SortArrow direction={sortDirection} />{/if}
				</button>
			{/each}
			<button class="dropdown-menu-cancel" onclick={closeMenu}>{$t('common.cancel')}</button>
		</div>
	{/if}
</div>
