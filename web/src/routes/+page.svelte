<script lang="ts">
	import { page } from '$app/stores';
	import { goto } from '$app/navigation';
	import { api, ApiError, type Item } from '$lib/api';
	import { auth } from '$lib/auth';
	import { startUpload } from '$lib/upload';
	import ShareDialog from '$lib/ShareDialog.svelte';
	import MoveDialog from '$lib/MoveDialog.svelte';
	import ScanDialog from '$lib/ScanDialog.svelte';
	import FileIcon from '$lib/FileIcon.svelte';

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
	let movingItem = $state<Item | null>(null);
	let scanOpen = $state(false);
	// Filters the *current folder's own* listing only — a real search
	// across the whole tree would need a backend endpoint (and a decision
	// about how deep/fast that should be) this app doesn't have yet, so
	// this stays a client-side, current-folder-only filter rather than
	// pretending to be more than that.
	let searchQuery = $state('');
	let visibleItems = $derived(
		searchQuery.trim()
			? items.filter((item) => item.name.toLowerCase().includes(searchQuery.trim().toLowerCase()))
			: items
	);

	// Which row's action menu is open, by item id — null means none. Only
	// one at a time, mirroring how a real menu behaves (opening another
	// row's menu closes whichever one was already open, see toggleMenu).
	let openMenuFor = $state<string | null>(null);

	function toggleMenu(id: string, event: MouseEvent) {
		event.stopPropagation();
		openMenuFor = openMenuFor === id ? null : id;
	}

	function closeMenu() {
		openMenuFor = null;
	}

	// A plain hand-rolled outside-click/Escape close, not the native Popover
	// API: Popover's own light-dismiss would solve this for free, but
	// positioning a popover next to its trigger button relies on CSS anchor
	// positioning, which isn't reliably supported across mobile browsers yet
	// — and a mispositioned action menu on the one platform this feature is
	// explicitly for (mobile) would be worse than not using the new API.
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
		searchQuery = ''; // a filter scoped to the folder you were just in shouldn't silently apply to the one you navigate to next
	});

	function openFolder(id: string) {
		goto(`/?folder=${encodeURIComponent(id)}`);
	}

	// The "from" param is how /file/[id] knows which folder's "← Back" link
	// to build — it can't just always mean root, and browser-history back()
	// isn't reliable for that either (a bookmark or a page reload before
	// this navigation both have no history entry to go back to).
	function openFile(id: string) {
		const from = currentFolderId ? `?from=${encodeURIComponent(currentFolderId)}` : '';
		goto(`/file/${id}${from}`);
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
		closeMenu();
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
		closeMenu();
		const newName = prompt('New name', item.name);
		if (!newName || newName === item.name) return;
		try {
			await api.move(item.id, newName, currentFolderId);
			await load(currentFolderId);
		} catch (err) {
			error = err instanceof ApiError ? err.message : 'Could not rename this item.';
		}
	}

	function handleStartMove(item: Item, event: MouseEvent) {
		event.stopPropagation();
		closeMenu();
		movingItem = item;
	}

	function handleStartShare(item: Item, event: MouseEvent) {
		event.stopPropagation();
		closeMenu();
		sharingItem = item;
	}

	// Mirrors Google Drive's own "Make a copy": duplicates into the same
	// folder the original is currently sitting in (which, for any row
	// visible here, is exactly currentFolderId), auto-suffixed by the
	// backend since the name is guaranteed to collide with the original.
	async function handleCopy(item: Item, event: MouseEvent) {
		event.stopPropagation();
		closeMenu();
		try {
			await api.copyItem(item.id, currentFolderId);
			await load(currentFolderId);
		} catch (err) {
			error = err instanceof ApiError ? err.message : 'Could not copy this item.';
		}
	}

	// A plain <a href> can't carry the Authorization header a download
	// needs, so the file is fetched as a blob and handed to the browser via
	// a throwaway object URL instead.
	async function handleDownload(item: Item, event: MouseEvent) {
		event.stopPropagation();
		closeMenu();
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

	// ScanDialog hands back one already-built PDF File — from here it's a
	// normal upload into the current folder, same as anything picked via
	// "+ Upload" or dropped in, reusing the exact same progress-tracked path.
	function handleScanned(file: File) {
		uploadFiles([file]);
	}

	// The upload panel's "Done"/error rows never went away on their own —
	// there was no button that actually removed a settled entry, just a
	// static status label that happened to be sitting where a dismiss
	// button would go, easy to mistake for one. An entry can only be
	// dismissed once it's settled (done or error): dismissing mid-upload
	// wouldn't stop the transfer, just hide it, so an error partway through
	// would silently vanish instead of surfacing.
	function dismissUpload(id: string) {
		uploads = uploads.filter((u) => u.id !== id);
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
	<div class="search-box">
		<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true">
			<circle cx="11" cy="11" r="6" />
			<path d="m20 20-3.5-3.5" stroke-linecap="round" />
		</svg>
		<input type="search" placeholder="Cerca in questa cartella…" bind:value={searchQuery} aria-label="Cerca file" />
	</div>
	<div style="display:flex; gap: var(--space-2)">
		<button class="btn" onclick={() => fileInput.click()}>+ Upload</button>
		<button class="btn" onclick={() => (scanOpen = true)}>📷 Scan</button>
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
					<button class="btn icon-btn" aria-label="Dismiss" onclick={() => dismissUpload(entry.id)}>✕</button>
				{:else}
					<span class="error-text">{entry.error ?? 'Failed'}</span>
					<button class="btn icon-btn" aria-label="Dismiss" onclick={() => dismissUpload(entry.id)}>✕</button>
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
	{:else if visibleItems.length === 0}
		<div class="empty-state">No files match "{searchQuery}" in this folder.</div>
	{:else}
		<div class="item-list">
			{#each visibleItems as item (item.id)}
			<div class="item-row">
				<span class="item-icon">
					<FileIcon type={item.type} name={item.name} mimeType={item.mime_type} />
				</span>
				<button
					class="item-name"
					onclick={() => (item.type === 'folder' ? openFolder(item.id) : openFile(item.id))}
				>
					{item.name}
				</button>
				<span class="item-size">{formatSize(item.size_bytes)}</span>
				<div class="row-menu">
					<button
						class="btn icon-btn"
						aria-label="Actions for {item.name}"
						aria-haspopup="true"
						aria-expanded={openMenuFor === item.id}
						onclick={(e) => toggleMenu(item.id, e)}
					>
						⋮
					</button>
					{#if openMenuFor === item.id}
						<!-- svelte-ignore a11y_no_static_element_interactions -->
						<!-- svelte-ignore a11y_interactive_supports_focus -->
						<!-- svelte-ignore a11y_click_events_have_key_events -->
						<div class="dropdown-menu" onclick={(e) => e.stopPropagation()} role="menu">
							{#if item.type === 'file'}
								<button role="menuitem" onclick={(e) => handleDownload(item, e)}>⬇ Download</button>
							{/if}
							<button role="menuitem" onclick={(e) => handleRename(item, e)}>✎ Rename</button>
							<button role="menuitem" onclick={(e) => handleStartMove(item, e)}>➜ Move</button>
							<button role="menuitem" onclick={(e) => handleCopy(item, e)}>⧉ Make a copy</button>
							<button role="menuitem" onclick={(e) => handleStartShare(item, e)}>🔗 Share</button>
							<button role="menuitem" class="danger" onclick={(e) => handleDelete(item, e)}>🗑 Delete</button>
						</div>
					{/if}
				</div>
			</div>
			{/each}
		</div>
	{/if}
</div>

<ShareDialog bind:item={sharingItem} />
<MoveDialog bind:item={movingItem} onMoved={() => load(currentFolderId)} />
<ScanDialog bind:open={scanOpen} onScanned={handleScanned} />
