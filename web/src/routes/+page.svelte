<script lang="ts">
	import { page } from '$app/stores';
	import { goto } from '$app/navigation';
	import { api, ApiError, type Item } from '$lib/api';
	import { auth } from '$lib/auth';
	import { startUpload } from '$lib/upload';
	import ShareDialog from '$lib/ShareDialog.svelte';

	interface Crumb {
		id: string | null;
		name: string;
	}

	interface UploadEntry {
		id: string;
		name: string;
		progress: number;
		status: 'uploading' | 'done' | 'error';
		error?: string;
	}

	let items = $state<Item[]>([]);
	let breadcrumb = $state<Crumb[]>([{ id: null, name: 'Home' }]);
	let loading = $state(true);
	let error = $state('');
	let uploads = $state<UploadEntry[]>([]);
	let dragging = $state(false);
	let fileInput: HTMLInputElement;
	let sharingItem = $state<Item | null>(null);

	// A local tracking key for the upload progress panel below — doesn't
	// need to be globally unique or unguessable, just distinct within this
	// tab's own `uploads` array, so crypto.randomUUID() would be overkill
	// even where it works. It also *doesn't* work everywhere: it's a
	// secure-context-only API (HTTPS or localhost), so on a home server
	// reached over plain http via its LAN IP — a real deployment shape for
	// this app, not a hypothetical one — it throws, silently aborting the
	// whole upload before startUpload() is ever called. Caught by exactly
	// that: a real user on the real deployment clicking "+ Upload" and
	// seeing nothing happen.
	let nextUploadId = 0;
	function newUploadId(): string {
		nextUploadId += 1;
		return `upload-${Date.now()}-${nextUploadId}`;
	}

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

	// api.move (PATCH /items/{id}) is a full replacement of name + location
	// (see CONTRIBUTING.md on why that's items' convention, unlike users'
	// partial-update PATCH) — a rename is just that call with the same
	// folder it's already in, only the name actually changing.
	async function handleRename(item: Item, event: MouseEvent) {
		event.stopPropagation();
		const newName = prompt('New name', item.name);
		if (!newName || newName === item.name) return;
		try {
			await api.move(item.id, newName, currentFolderId);
			await load(currentFolderId);
		} catch (err) {
			error = err instanceof ApiError ? err.message : 'Could not rename this item.';
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

	function uploadFiles(fileList: FileList | File[]) {
		for (const file of fileList) {
			const entryId = newUploadId();
			uploads.push({ id: entryId, name: file.name, progress: 0, status: 'uploading' });

			startUpload(file, currentFolderId, {
				onProgress: (percent) => {
					const entry = uploads.find((u) => u.id === entryId);
					if (entry) entry.progress = percent;
				},
				onSuccess: () => {
					const entry = uploads.find((u) => u.id === entryId);
					if (entry) {
						entry.status = 'done';
						entry.progress = 100;
					}
					// Only the current folder's own listing needs refreshing —
					// an upload started elsewhere and finishing later shouldn't
					// yank the view out from under whatever the user is looking
					// at now, so this deliberately doesn't force-navigate.
					load(currentFolderId);
				},
				onError: (message) => {
					const entry = uploads.find((u) => u.id === entryId);
					if (entry) {
						entry.status = 'error';
						entry.error = message;
					}
				}
			});
		}
	}

	function handleFileInputChange(event: Event) {
		const input = event.target as HTMLInputElement;
		if (input.files?.length) uploadFiles(input.files);
		input.value = ''; // allow re-selecting the same file later
	}

	function handleDrop(event: DragEvent) {
		event.preventDefault();
		dragging = false;
		if (event.dataTransfer?.files.length) uploadFiles(event.dataTransfer.files);
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
	<div style="display:flex; gap: var(--space-2)">
		<button class="btn" onclick={() => fileInput.click()}>+ Upload</button>
		<button class="btn btn-primary" onclick={handleNewFolder}>+ New folder</button>
	</div>
</div>

<input
	bind:this={fileInput}
	type="file"
	multiple
	hidden
	onchange={handleFileInputChange}
/>

{#if uploads.length > 0}
	<div class="card" style="margin-bottom: var(--space-4)">
		{#each uploads as entry (entry.id)}
			<div class="upload-entry">
				<span class="upload-name">{entry.name}</span>
				{#if entry.status === 'uploading'}
					<div class="upload-bar"><div class="upload-bar-fill" style="width: {entry.progress}%"></div></div>
					<span class="upload-percent">{entry.progress}%</span>
				{:else if entry.status === 'done'}
					<span class="upload-status-done">Done</span>
				{:else}
					<span class="error-text">{entry.error ?? 'Failed'}</span>
				{/if}
			</div>
		{/each}
	</div>
{/if}

{#if error}
	<p class="error-text">{error}</p>
{/if}

<!-- svelte-ignore a11y_no_static_element_interactions -->
<div
	ondragover={(e) => {
		e.preventDefault();
		dragging = true;
	}}
	ondragleave={() => (dragging = false)}
	ondrop={handleDrop}
	class:dropzone-active={dragging}
>
	{#if loading}
		<p>Loading…</p>
	{:else if items.length === 0}
		<div class="empty-state">This folder is empty. Drop files here, or use "+ Upload".</div>
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
				<button class="btn" onclick={(e) => handleRename(item, e)}>Rename</button>
				<button
					class="btn"
					onclick={(e) => {
						e.stopPropagation();
						sharingItem = item;
					}}
				>
					Share
				</button>
				<button class="btn" onclick={(e) => handleDelete(item, e)}>Delete</button>
			</div>
			{/each}
		</div>
	{/if}
</div>

<ShareDialog bind:item={sharingItem} />
