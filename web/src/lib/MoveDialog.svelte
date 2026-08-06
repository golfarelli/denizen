<script lang="ts">
	import { api, ApiError, type Item } from '$lib/api';
	import FileIcon from '$lib/FileIcon.svelte';

	interface Crumb {
		id: string | null;
		name: string;
	}

	// null = closed. onMoved lets the caller refresh its own listing after a
	// successful move — unlike ShareDialog, this actually changes the
	// current folder's contents.
	let { item = $bindable(null), onMoved }: { item: Item | null; onMoved: () => void } = $props();

	let dialogEl: HTMLDialogElement;
	let currentFolderId = $state<string | null>(null);
	let breadcrumb = $state<Crumb[]>([{ id: null, name: 'Home' }]);
	let folders = $state<Item[]>([]);
	let loading = $state(false);
	let error = $state('');
	let moving = $state(false);

	$effect(() => {
		if (item) {
			currentFolderId = null;
			breadcrumb = [{ id: null, name: 'Home' }];
			error = '';
			loadFolders(null);
			dialogEl?.showModal();
		} else {
			dialogEl?.close();
		}
	});

	async function loadFolders(folderId: string | null) {
		loading = true;
		try {
			const all = await api.listItems(folderId);
			// Only folders are valid destinations, and the item being moved
			// can't be moved into itself — the backend also rejects moving it
			// into one of its own descendants, surfaced as an error if
			// someone navigates there and hits "Move here" anyway rather than
			// pre-computed here.
			folders = all.filter((i) => i.type === 'folder' && i.id !== item?.id);
		} catch (err) {
			error = err instanceof ApiError ? err.message : 'Could not load folders.';
		} finally {
			loading = false;
		}
	}

	async function openFolder(folder: Item) {
		currentFolderId = folder.id;
		breadcrumb = [...breadcrumb, { id: folder.id, name: folder.name }];
		await loadFolders(folder.id);
	}

	async function goToCrumb(index: number) {
		const crumb = breadcrumb[index];
		currentFolderId = crumb.id;
		breadcrumb = breadcrumb.slice(0, index + 1);
		await loadFolders(crumb.id);
	}

	function close() {
		item = null;
	}

	async function handleMoveHere() {
		if (!item) return;
		moving = true;
		error = '';
		try {
			await api.move(item.id, item.name, currentFolderId);
			onMoved();
			close();
		} catch (err) {
			error = err instanceof ApiError ? err.message : 'Could not move this item.';
		} finally {
			moving = false;
		}
	}
</script>

<dialog bind:this={dialogEl} onclose={close} class="card">
	{#if item}
		<h2>Move "{item.name}"</h2>

		<nav class="breadcrumb">
			{#each breadcrumb as crumb, i (crumb.id ?? 'root')}
				{#if i > 0}<span>/</span>{/if}
				<button onclick={() => goToCrumb(i)} disabled={i === breadcrumb.length - 1}>
					{crumb.name}
				</button>
			{/each}
		</nav>

		{#if error}<p class="error-text">{error}</p>{/if}

		{#if loading}
			<p>Loading…</p>
		{:else if folders.length === 0}
			<p class="hint">No subfolders here.</p>
		{:else}
			<div class="item-list">
				{#each folders as folder (folder.id)}
					<div class="item-row item-row-flex">
						<span class="item-icon"><FileIcon type="folder" name={folder.name} /></span>
						<button class="item-name" onclick={() => openFolder(folder)}>{folder.name}</button>
					</div>
				{/each}
			</div>
		{/if}

		<div class="dialog-actions">
			<button class="btn" onclick={close}>Cancel</button>
			<button class="btn btn-primary" onclick={handleMoveHere} disabled={moving}>
				{moving ? 'Moving…' : 'Move here'}
			</button>
		</div>
	{/if}
</dialog>
