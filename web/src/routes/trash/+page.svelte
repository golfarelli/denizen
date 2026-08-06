<script lang="ts">
	import { onMount } from 'svelte';
	import { api, ApiError, type Item } from '$lib/api';
	import FileIcon from '$lib/FileIcon.svelte';

	let items = $state<Item[]>([]);
	let loading = $state(true);
	let error = $state('');

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

	async function handleRestore(item: Item) {
		try {
			await api.restoreItem(item.id);
			await load();
		} catch (err) {
			error = err instanceof ApiError ? err.message : 'Could not restore this item.';
		}
	}

	async function handleDeleteForever(item: Item) {
		if (!confirm(`Permanently delete "${item.name}"? This cannot be undone.`)) return;
		try {
			await api.permanentlyDeleteItem(item.id);
			await load();
		} catch (err) {
			error = err instanceof ApiError ? err.message : 'Could not delete this item.';
		}
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
	<div class="item-list">
		{#each items as item (item.id)}
			<div class="item-row">
				<span class="item-icon">
					<FileIcon type={item.type} name={item.name} mimeType={item.mime_type} />
				</span>
				<span class="item-name" style="cursor: default">{item.name}</span>
				<button class="btn" onclick={() => handleRestore(item)}>
					<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" aria-hidden="true">
						<path d="M4 9a8 8 0 1 1 1.5 8.5" stroke-linecap="round" />
						<path d="M4 4v5h5" stroke-linecap="round" stroke-linejoin="round" />
					</svg>
					Restore
				</button>
				<button class="btn" onclick={() => handleDeleteForever(item)}>
					<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" aria-hidden="true">
						<path
							d="M5 7h14M9 7V5a1 1 0 0 1 1-1h4a1 1 0 0 1 1 1v2M7 7l1 13a1 1 0 0 0 1 1h6a1 1 0 0 0 1-1l1-13"
							stroke-linecap="round"
							stroke-linejoin="round"
						/>
					</svg>
					Delete forever
				</button>
			</div>
		{/each}
	</div>
{/if}
