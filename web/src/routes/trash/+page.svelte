<script lang="ts">
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { api, ApiError, type Item } from '$lib/api';
	import FileIcon from '$lib/FileIcon.svelte';

	let items = $state<Item[]>([]);
	let loading = $state(true);
	let error = $state('');

	// Same pattern as routes/+page.svelte's own row action menu — see its
	// own comment on why a plain hand-rolled outside-click/Escape close,
	// not the native Popover API.
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

	async function load() {
		loading = true;
		error = '';
		try {
			items = await api.listTrash();
		} catch (err) {
			error = err instanceof ApiError ? err.message : 'Could not load the trash.';
		} finally {
			loading = false;
		}
	}

	onMount(load);

	// A trashed file is still openable for a quick look (GetIncludingTrashed
	// server-side — see internal/handler/item.go) before deciding whether
	// to restore or delete it forever, same as Drive/Nextcloud's own trash.
	// A folder has nothing to open — this app has no "browse into a
	// trashed folder" view, only restore-the-whole-thing-or-not.
	function openFile(item: Item) {
		if (item.type !== 'file') return;
		goto(`/file/${item.id}?from=trash`);
	}

	async function handleRestore(item: Item, event: MouseEvent) {
		event.stopPropagation();
		closeMenu();
		try {
			await api.restoreItem(item.id);
			await load();
		} catch (err) {
			error = err instanceof ApiError ? err.message : 'Could not restore this item.';
		}
	}

	async function handleDeleteForever(item: Item, event: MouseEvent) {
		event.stopPropagation();
		closeMenu();
		if (!confirm(`Permanently delete "${item.name}"? This cannot be undone.`)) return;
		try {
			await api.permanentlyDeleteItem(item.id);
			await load();
		} catch (err) {
			error = err instanceof ApiError ? err.message : 'Could not delete this item.';
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
	<title>Trash · Denizen</title>
</svelte:head>

<h1>Trash</h1>
<p class="hint">
	Items here are permanently deleted automatically after 30 days. Trashed items still count
	against your storage quota until then.
</p>

{#if error}
	<p class="error-text">{error}</p>
{/if}

{#if loading}
	<p>Loading…</p>
{:else if items.length === 0}
	<div class="empty-state">Trash is empty.</div>
{:else}
	<div class="item-list-header">
		<span class="item-icon"></span>
		<span class="item-name-header">Name</span>
		<span class="item-modified">Modified</span>
		<span class="item-size">Size</span>
		<span class="row-menu"></span>
	</div>
	<div class="item-list">
		{#each items as item (item.id)}
			<div class="item-row">
				<span class="item-icon">
					<FileIcon type={item.type} name={item.name} mimeType={item.mime_type} />
				</span>
				<button
					class="item-name"
					disabled={item.type !== 'file'}
					onclick={() => openFile(item)}
				>
					{item.name}
				</button>
				<span class="item-modified">{formatDate(item.updated_at)}</span>
				<span class="item-size">{formatSize(item.size_bytes)}</span>
				<div class="row-menu">
					<button
						class="btn icon-btn"
						aria-label="Actions for {item.name}"
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
							{#if item.type === 'file'}
								<button role="menuitem" onclick={() => openFile(item)}>
									<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" aria-hidden="true">
										<path d="M2 12s3.5-7 10-7 10 7 10 7-3.5 7-10 7-10-7-10-7Z" stroke-linejoin="round" />
										<circle cx="12" cy="12" r="3" />
									</svg>
									Open
								</button>
							{/if}
							<button role="menuitem" onclick={(e) => handleRestore(item, e)}>
								<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" aria-hidden="true">
									<path d="M4 9a8 8 0 1 1 1.5 8.5" stroke-linecap="round" />
									<path d="M4 4v5h5" stroke-linecap="round" stroke-linejoin="round" />
								</svg>
								Restore
							</button>
							<button role="menuitem" class="danger" onclick={(e) => handleDeleteForever(item, e)}>
								<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" aria-hidden="true">
									<path
										d="M5 7h14M9 7V5a1 1 0 0 1 1-1h4a1 1 0 0 1 1 1v2M7 7l1 13a1 1 0 0 0 1 1h6a1 1 0 0 0 1-1l1-13"
										stroke-linecap="round"
										stroke-linejoin="round"
									/>
								</svg>
								Delete forever
							</button>
							<button class="dropdown-menu-cancel" onclick={closeMenu}>Cancel</button>
						</div>
					{/if}
				</div>
			</div>
		{/each}
	</div>
{/if}
