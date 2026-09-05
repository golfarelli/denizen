<script lang="ts">
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { api, ApiError, type Item } from '$lib/api';
	import FileIcon from '$lib/FileIcon.svelte';
	import SkeletonList from '$lib/SkeletonList.svelte';
	import { t } from '$lib/i18n';

	// Flat list of starred items — unlike Recenti, can contain folders (Drive
	// lets you star those too) and items you don't own (favoriting a shared
	// item is explicitly in scope — see ItemService.ListFavorites). A
	// folder opens into the main browser (List already knows how to walk
	// into someone else's shared folder via parent_id); a file opens the
	// preview page, same as every other flat list here.

	let items = $state<Item[]>([]);
	let loading = $state(true);
	let error = $state('');

	async function load() {
		loading = true;
		error = '';
		try {
			items = await api.listFavorites();
		} catch (err) {
			error = err instanceof ApiError ? err.message : $t('favorites.errors.couldNotLoad');
		} finally {
			loading = false;
		}
	}

	onMount(load);

	function openItem(item: Item) {
		if (item.type === 'folder') {
			goto(`/?folder=${encodeURIComponent(item.id)}`);
			return;
		}
		goto(`/file/${item.id}?from=favorites`);
	}

	let openMenuFor = $state<string | null>(null);

	function toggleMenu(id: string, event: MouseEvent) {
		event.stopPropagation();
		openMenuFor = openMenuFor === id ? null : id;
	}

	function closeMenu() {
		openMenuFor = null;
	}

	$effect(() => {
		if (!openMenuFor) return;
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

	async function handleRemove(item: Item, event: MouseEvent) {
		event.stopPropagation();
		closeMenu();
		try {
			await api.removeFavorite(item.id);
			items = items.filter((i) => i.id !== item.id);
		} catch (err) {
			error = err instanceof ApiError ? err.message : $t('common.errors.couldNotUpdateFavorite');
		}
	}

	function formatSize(bytes: number): string {
		if (bytes === 0) return '';
		const units = ['B', 'KB', 'MB', 'GB'];
		let value = bytes;
		let unit = 0;
		while (value >= 1024 && unit < units.length - 1) {
			value /= 1024;
			unit++;
		}
		return `${value.toFixed(unit === 0 ? 0 : 1)} ${units[unit]}`;
	}

	function formatDate(unixSeconds: number): string {
		return new Date(unixSeconds * 1000).toLocaleDateString(undefined, {
			year: 'numeric',
			month: 'short',
			day: 'numeric'
		});
	}
</script>

<svelte:head>
	<title>{$t('nav.favorites')} · Denizen</title>
</svelte:head>

<div class="toolbar">
	<h1 style="margin:0">{$t('nav.favorites')}</h1>
</div>
<p class="hint">{$t('favorites.hint')}</p>

{#if error}
	<p class="error-text">{error}</p>
{/if}

{#if loading}
	<SkeletonList />
{:else if items.length === 0}
	<div class="empty-state">{$t('favorites.empty')}</div>
{:else}
	<div class="item-list-header">
		<span class="item-icon"></span>
		<span class="item-name-header">{$t('common.name')}</span>
		<span class="item-modified">{$t('common.modified')}</span>
		<span class="item-size">{$t('common.size')}</span>
		<span class="row-menu"></span>
	</div>
	<div class="item-list">
		{#each items as item (item.id)}
			<div class="item-row">
				<span class="item-icon">
					<FileIcon type={item.type} name={item.name} mimeType={item.mime_type} />
				</span>
				<button class="item-name" onclick={() => openItem(item)}>
					{item.name}
				</button>
				<span class="item-modified">{formatDate(item.updated_at)}</span>
				<span class="item-size">{item.type === 'file' ? formatSize(item.size_bytes) : ''}</span>
				<div class="row-menu">
					<button
						class="btn icon-btn"
						aria-label={$t('common.actionsFor', { name: item.name })}
						aria-haspopup="true"
						aria-expanded={openMenuFor === item.id}
						onclick={(e) => toggleMenu(item.id, e)}
					>
						<svg width="18" height="18" viewBox="0 0 24 24" aria-hidden="true">
							<circle cx="12" cy="5" r="1.6" fill="currentColor" />
							<circle cx="12" cy="12" r="1.6" fill="currentColor" />
							<circle cx="12" cy="19" r="1.6" fill="currentColor" />
						</svg>
					</button>
					{#if openMenuFor === item.id}
						<!-- svelte-ignore a11y_no_static_element_interactions -->
						<!-- svelte-ignore a11y_click_events_have_key_events -->
						<div class="menu-backdrop" onclick={closeMenu}></div>
						<!-- svelte-ignore a11y_no_static_element_interactions -->
						<!-- svelte-ignore a11y_interactive_supports_focus -->
						<!-- svelte-ignore a11y_click_events_have_key_events -->
						<div class="dropdown-menu" onclick={(e) => e.stopPropagation()} role="menu">
							<button role="menuitem" onclick={() => openItem(item)}>
								<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" aria-hidden="true">
									<path d="M2 12s3.5-7 10-7 10 7 10 7-3.5 7-10 7-10-7-10-7Z" stroke-linejoin="round" />
									<circle cx="12" cy="12" r="3" />
								</svg>
								{$t('common.open')}
							</button>
							<button role="menuitem" onclick={(e) => handleRemove(item, e)}>
								<svg width="16" height="16" viewBox="0 0 24 24" fill="currentColor" stroke="currentColor" stroke-width="1.8" aria-hidden="true">
									<path d="M12 3.5l2.7 5.9 6.3.7-4.7 4.4 1.3 6.2-5.6-3.2-5.6 3.2 1.3-6.2-4.7-4.4 6.3-.7L12 3.5Z" stroke-linejoin="round" />
								</svg>
								{$t('common.removeFromFavorites')}
							</button>
							<button class="dropdown-menu-cancel" onclick={closeMenu}>{$t('common.cancel')}</button>
						</div>
					{/if}
				</div>
			</div>
		{/each}
	</div>
{/if}
