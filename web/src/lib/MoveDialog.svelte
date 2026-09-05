<script lang="ts">
	import { api, ApiError, type Item } from '$lib/api';
	import FileIcon from '$lib/FileIcon.svelte';
	import { t } from '$lib/i18n';

	interface Crumb {
		id: string | null;
		name: string;
	}

	// null/empty = closed. An array, not a single item, so the same dialog
	// covers both "Move" on one row and a bulk-selection "Move" — a rename
	// never happens here either way (see handleConfirm), only relocation.
	// onMoved lets the caller refresh its own listing after a successful
	// move — unlike ShareDialog, this actually changes the current
	// folder's contents.
	//
	// mode 'shortcut' reuses this exact same breadcrumb/folder-browsing UI
	// for "Aggiungi collegamento" instead — same picker, only the confirm
	// action and copy change (see handleConfirm/the heading below). Bulk
	// "add a shortcut for every selected item" comes for free from the
	// same items-array support Move already has.
	let {
		items = $bindable(null),
		mode = 'move',
		onMoved
	}: { items: Item[] | null; mode?: 'move' | 'shortcut'; onMoved: () => void } = $props();

	let dialogEl: HTMLDialogElement;
	let currentFolderId = $state<string | null>(null);
	let breadcrumb = $state<Crumb[]>([{ id: null, name: 'Home' }]);
	let folders = $state<Item[]>([]);
	let loading = $state(false);
	let error = $state('');
	let confirming = $state(false);

	let movingIds = $derived(new Set((items ?? []).map((i) => i.id)));

	$effect(() => {
		if (items && items.length > 0) {
			currentFolderId = null;
			breadcrumb = [{ id: null, name: $t('common.home') }];
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
			// Only folders are valid destinations, and none of the items
			// being moved can be a valid destination for themselves — the
			// backend also rejects moving a folder into one of its own
			// descendants, surfaced as an error if someone navigates there
			// and hits "Move here" anyway rather than pre-computed here.
			folders = all.filter((i) => i.type === 'folder' && !movingIds.has(i.id));
		} catch (err) {
			error = err instanceof ApiError ? err.message : $t('dialogs.move.errors.couldNotLoadFolders');
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
		items = null;
	}

	async function handleConfirm() {
		if (!items || items.length === 0) return;
		confirming = true;
		error = '';
		// Every item keeps its own name — a bulk move never renames, same
		// as dragging a multi-selection onto a folder in Drive/Nextcloud
		// (and a shortcut's own name is always a fresh snapshot of the
		// target's regardless — see createShortcut's own comment). One
		// item's failure (e.g. a name collision the backend rejects)
		// doesn't stop the rest.
		const results = await Promise.allSettled(
			items.map((item) =>
				mode === 'shortcut' ? api.createShortcut(item.id, currentFolderId) : api.move(item.id, item.name, currentFolderId)
			)
		);
		const failed = results.filter((r) => r.status === 'rejected').length;
		confirming = false;
		if (failed > 0) {
			error =
				failed === items.length
					? mode === 'shortcut'
						? $t('common.errors.couldNotAddShortcut')
						: $t('common.errors.couldNotMove')
					: $t(mode === 'shortcut' ? 'dialogs.shortcut.errors.someFailed' : 'dialogs.move.errors.someFailed', {
							count: failed
						});
		}
		onMoved();
		if (failed < items.length) close();
	}
</script>

<dialog bind:this={dialogEl} onclose={close} class="card">
	{#if items && items.length > 0}
		<h2>
			{items.length === 1
				? $t(mode === 'shortcut' ? 'dialogs.shortcut.heading' : 'dialogs.move.heading', { name: items[0].name })
				: $t(mode === 'shortcut' ? 'dialogs.shortcut.headingMultiple' : 'dialogs.move.headingMultiple', {
						count: items.length
					})}
		</h2>

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
			<p>{$t('common.loading')}</p>
		{:else if folders.length === 0}
			<p class="hint">{$t('dialogs.move.noSubfolders')}</p>
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
			<button class="btn" onclick={close}>{$t('common.cancel')}</button>
			<button class="btn btn-primary" onclick={handleConfirm} disabled={confirming}>
				{confirming
					? $t(mode === 'shortcut' ? 'dialogs.shortcut.adding' : 'dialogs.move.moving')
					: $t(mode === 'shortcut' ? 'dialogs.shortcut.addHere' : 'dialogs.move.moveHere')}
			</button>
		</div>
	{/if}
</dialog>
