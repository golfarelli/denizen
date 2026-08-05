<script lang="ts">
	import { page } from '$app/stores';
	import { goto } from '$app/navigation';
	import { api, ApiError, type Item } from '$lib/api';
	import { auth } from '$lib/auth';

	interface Crumb {
		id: string | null;
		name: string;
	}

	let items = $state<Item[]>([]);
	let breadcrumb = $state<Crumb[]>([{ id: null, name: 'Home' }]);
	let loading = $state(true);
	let error = $state('');

	// The current folder lives in the URL (?folder=<id>, absent = root) so a
	// reload or a shared link lands back in the same place.
	let currentFolderId = $derived($page.url.searchParams.get('folder'));

	async function buildBreadcrumb(folderId: string | null): Promise<Crumb[]> {
		if (!folderId) return [{ id: null, name: 'Home' }];
		const chain: Crumb[] = [];
		let current: string | null = folderId;
		while (current) {
			const item = await api.getItem(current);
			chain.unshift({ id: item.id, name: item.name });
			current = item.parent_id;
		}
		return [{ id: null, name: 'Home' }, ...chain];
	}

	async function load(folderId: string | null) {
		loading = true;
		error = '';
		try {
			const [listing, crumbs] = await Promise.all([api.listItems(folderId), buildBreadcrumb(folderId)]);
			items = listing ?? [];
			breadcrumb = crumbs;
		} catch (err) {
			error = err instanceof ApiError ? err.message : 'Could not load this folder.';
		} finally {
			loading = false;
		}
	}

	$effect(() => {
		if ($auth) load(currentFolderId);
	});

	function openFolder(id: string) {
		goto(`/?folder=${encodeURIComponent(id)}`);
	}

	function goToCrumb(id: string | null) {
		goto(id ? `/?folder=${encodeURIComponent(id)}` : '/');
	}

	async function handleNewFolder() {
		const name = prompt('Folder name');
		if (!name) return;
		try {
			await api.createFolder(name, currentFolderId);
			await load(currentFolderId);
		} catch (err) {
			error = err instanceof ApiError ? err.message : 'Could not create the folder.';
		}
	}

	async function handleDelete(item: Item, event: MouseEvent) {
		event.stopPropagation();
		if (!confirm(`Move "${item.name}" to trash?`)) return;
		try {
			await api.deleteItem(item.id);
			await load(currentFolderId);
		} catch (err) {
			error = err instanceof ApiError ? err.message : 'Could not delete this item.';
		}
	}

	// A plain <a href> can't carry the Authorization header a download
	// needs, so the file is fetched as a blob and handed to the browser via
	// a throwaway object URL instead.
	async function handleDownload(item: Item, event: MouseEvent) {
		event.stopPropagation();
		try {
			const blob = await api.downloadContent(item.id);
			const url = URL.createObjectURL(blob);
			const a = document.createElement('a');
			a.href = url;
			a.download = item.name;
			a.click();
			URL.revokeObjectURL(url);
		} catch {
			error = 'Could not download this file.';
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
</script>

<svelte:head>
	<title>Denizen</title>
</svelte:head>

<nav class="breadcrumb">
	{#each breadcrumb as crumb, i (crumb.id ?? 'root')}
		{#if i > 0}<span>/</span>{/if}
		<button onclick={() => goToCrumb(crumb.id)} disabled={i === breadcrumb.length - 1}>
			{crumb.name}
		</button>
	{/each}
</nav>

<div class="toolbar">
	<h1 style="margin:0">Files</h1>
	<button class="btn btn-primary" onclick={handleNewFolder}>+ New folder</button>
</div>

{#if error}
	<p class="error-text">{error}</p>
{/if}

{#if loading}
	<p>Loading…</p>
{:else if items.length === 0}
	<div class="empty-state">This folder is empty.</div>
{:else}
	<div class="item-list">
		{#each items as item (item.id)}
			<div class="item-row">
				<span class="item-icon">{item.type === 'folder' ? '📁' : '📄'}</span>
				<button
					class="item-name"
					onclick={() => (item.type === 'folder' ? openFolder(item.id) : undefined)}
					disabled={item.type === 'file'}
				>
					{item.name}
				</button>
				<span class="item-size">{formatSize(item.size_bytes)}</span>
				{#if item.type === 'file'}
					<button class="btn" onclick={(e) => handleDownload(item, e)}>Download</button>
				{/if}
				<button class="btn" onclick={(e) => handleDelete(item, e)}>Delete</button>
			</div>
		{/each}
	</div>
{/if}
