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
				<button class="btn" onclick={() => handleRestore(item)}>Restore</button>
				<button class="btn" onclick={() => handleDeleteForever(item)}>Delete forever</button>
			</div>
		{/each}
	</div>
{/if}
