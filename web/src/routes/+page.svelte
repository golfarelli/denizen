<script lang="ts">
	import { page } from '$app/stores';
	import { goto } from '$app/navigation';
	import { api, ApiError, type Item } from '$lib/api';
	import { auth } from '$lib/auth';
	import { startUpload } from '$lib/upload';
	import ShareDialog from '$lib/ShareDialog.svelte';
	import BulkShareDialog from '$lib/BulkShareDialog.svelte';
	import MoveDialog from '$lib/MoveDialog.svelte';
	import ScanDialog from '$lib/ScanDialog.svelte';
	import FileIcon from '$lib/FileIcon.svelte';
	import { copyShareLink } from '$lib/copyShareLink';
	import { sortItems, type SortField, type SortDirection } from '$lib/sortItems';
	import { viewMode } from '$lib/viewMode';
	import SortArrow from '$lib/SortArrow.svelte';
	import SortMenu from '$lib/SortMenu.svelte';
	import { t } from '$lib/i18n';

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
	// The root crumb's label is snapshotted at whatever locale was active
	// when it was (re)built (on every folder navigation, see buildBreadcrumb
	// below) — switching language mid-browse without navigating leaves it
	// stale until the next folder change. Not worth extra reactive
	// plumbing for a single-user app where that's a rare, harmless edge.
	let breadcrumb = $state<Crumb[]>([{ id: null, name: $t('common.home') }]);
	// Whether the *current folder itself* may be written to — always true
	// at root (the caller's own), and for an owned folder anywhere, false
	// when browsing into a folder shared with the caller at view-only
	// permission. Gates Upload/Scan/New folder (see load, which sets this
	// from the same GET the breadcrumb chain already does — no extra
	// request).
	let currentFolderCanEdit = $state(true);
	let loading = $state(true);
	let error = $state('');
	let uploads = $state<UploadEntry[]>([]);
	let dragging = $state(false);
	let fileInput: HTMLInputElement;
	let sharingItem = $state<Item | null>(null);
	let movingItems = $state<Item[] | null>(null);
	let sharingItems = $state<Item[] | null>(null);
	let scanOpen = $state(false);
	// Multi-select for bulk actions (Share/Move/Download/Delete) — ids
	// rather than Items so it survives a `load()` refresh (a fresh array
	// of Item objects each time) without losing track of what's selected.
	let selectedIds = $state<Set<string>>(new Set());
	// A real, whole-drive search (GET /api/v1/search — name and indexed
	// file content, see internal/service/item.go's Search), not a filter
	// over whatever's already loaded for the current folder. null =
	// showing the current folder's own `items` as normal; non-null =
	// showing these search results instead (see the debounced $effect
	// below, and searchResultsFor's own comment on the staleness guard).
	let searchQuery = $state('');
	let searchResults = $state<Item[] | null>(null);
	let searching = $state(false);

	// Not persisted (unlike $viewMode, see lib/viewMode.ts) — resets to
	// name/ascending each visit, same as most desktop file managers do
	// rather than remembering a sort that might not make sense next time.
	let sortField = $state<SortField>('name');
	let sortDirection = $state<SortDirection>('asc');

	function toggleSort(field: SortField) {
		if (sortField === field) {
			sortDirection = sortDirection === 'asc' ? 'desc' : 'asc';
		} else {
			sortField = field;
			sortDirection = 'asc';
		}
	}

	let visibleItems = $derived(sortItems(searchResults ?? items, sortField, sortDirection));
	let selectedItems = $derived(visibleItems.filter((item) => selectedIds.has(item.id)));

	function toggleSelect(id: string, event?: Event) {
		event?.stopPropagation();
		const next = new Set(selectedIds);
		if (next.has(id)) next.delete(id);
		else next.add(id);
		selectedIds = next;
	}

	function clearSelection() {
		selectedIds = new Set();
	}

	function toggleSelectAll() {
		selectedIds =
			selectedIds.size === visibleItems.length
				? new Set()
				: new Set(visibleItems.map((item) => item.id));
	}

	// Long-press to select on mobile, instead of a checkbox sitting on
	// every row all the time — see .row-checkbox's own CSS comment for
	// why it's hidden below 640px until a selection is already active.
	// Pointer events (not touch-specific ones) so this works identically
	// with a mouse's own "press and hold", not just a finger.
	//
	// The buttons below also cancel the browser's own contextmenu event
	// (fired on a real touch-and-hold, separately from anything here) —
	// without that, Android in particular leaves the gesture half-eaten
	// by its native long-press handling even after our own timer-based
	// selection has already fired, and every *subsequent* tap on another
	// row stops registering at all. Confirmed this was the actual gap:
	// e2e (mouse-only, via Playwright's click({ delay })) never exercises
	// a real contextmenu event, so it looked fine in tests despite this.
	const LONG_PRESS_MS = 500;
	let longPressTimer: ReturnType<typeof setTimeout> | undefined;
	let longPressTriggered = false;

	function startLongPress(id: string) {
		longPressTriggered = false;
		clearTimeout(longPressTimer);
		longPressTimer = setTimeout(() => {
			longPressTriggered = true;
			toggleSelect(id);
		}, LONG_PRESS_MS);
	}

	function cancelLongPress() {
		clearTimeout(longPressTimer);
	}

	// The shared click handler for both the list row's .item-name button
	// and the grid tile's .item-tile-main button: a long-press just
	// finished selecting this item (longPressTriggered, reset here so it
	// doesn't leak into the next click), or a selection is already active
	// (any further tap keeps selecting instead of navigating away from
	// it) — either way, don't open. A plain tap with nothing selected
	// opens normally.
	function handleItemActivate(item: Item) {
		if (longPressTriggered) {
			longPressTriggered = false;
			return;
		}
		if (selectedIds.size > 0) {
			toggleSelect(item.id);
			return;
		}
		if (item.type === 'folder') openFolder(item.id);
		else openFile(item.id);
	}

	// Debounced so every keystroke doesn't fire a request — 300ms is short
	// enough to feel responsive, long enough that typing a whole word
	// only actually searches once. The `current === searchQuery.trim()`
	// checks below guard against a slower, earlier request's response
	// landing after a faster, later one's and clobbering it — no
	// AbortController: a personal-scale SQLite query is fast enough that
	// this is a defensive nicety, not something that actually races often.
	$effect(() => {
		const current = searchQuery.trim();
		if (!current) {
			searchResults = null;
			searching = false;
			return;
		}
		searching = true;
		const timer = setTimeout(async () => {
			try {
				const results = await api.search(current);
				if (searchQuery.trim() === current) searchResults = results;
			} catch (err) {
				if (searchQuery.trim() === current) {
					error = err instanceof ApiError ? err.message : $t('fileBrowser.errors.couldNotSearch');
				}
			} finally {
				if (searchQuery.trim() === current) searching = false;
			}
		}, 300);
		return () => clearTimeout(timer);
	});

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

	// Mobile-only floating action button (see app.css's .fab) — replaces
	// the always-visible Upload/Scan/New folder row on a phone-width
	// screen. A separate open/close state and effect from the row action
	// menu above (openMenuFor) since it's not per-item and the two can be
	// open independently of each other, though in practice a click
	// anywhere closes whichever one is open.
	let fabMenuOpen = $state(false);

	$effect(() => {
		if (!fabMenuOpen) return;
		function handlePointerDown() {
			fabMenuOpen = false;
		}
		function handleKeydown(e: KeyboardEvent) {
			if (e.key === 'Escape') fabMenuOpen = false;
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

	// Also reports the target folder's own can_edit (from the same GET that
	// already fetches it while walking the ancestor chain) — load below
	// uses it for currentFolderCanEdit, no separate request needed.
	async function buildBreadcrumb(folderId: string | null): Promise<{ crumbs: Crumb[]; canEdit: boolean }> {
		if (!folderId) return { crumbs: [{ id: null, name: $t('common.home') }], canEdit: true };
		const chain: Crumb[] = [];
		let current: string | null = folderId;
		let canEdit = true;
		while (current) {
			let item: Item;
			try {
				item = await api.getItem(current);
			} catch {
				// An ancestor above the point we were actually granted access
				// to — a shared subfolder nested inside parts of the owner's
				// drive we can't see the rest of. Stop climbing here instead
				// of failing the whole page; Home (prepended below) still
				// safely takes us back to our own root either way.
				break;
			}
			if (current === folderId) canEdit = item.can_edit;
			chain.unshift({ id: item.id, name: item.name });
			current = item.parent_id;
		}
		return { crumbs: [{ id: null, name: $t('common.home') }, ...chain], canEdit };
	}

	async function load(folderId: string | null) {
		loading = true;
		error = '';
		try {
			const [listing, folder] = await Promise.all([api.listItems(folderId), buildBreadcrumb(folderId)]);
			items = listing ?? [];
			breadcrumb = folder.crumbs;
			currentFolderCanEdit = folder.canEdit;
		} catch (err) {
			error = err instanceof ApiError ? err.message : $t('fileBrowser.errors.couldNotLoadFolder');
		} finally {
			loading = false;
		}
	}

	$effect(() => {
		if ($auth) load(currentFolderId);
		searchQuery = ''; // a filter scoped to the folder you were just in shouldn't silently apply to the one you navigate to next
		clearSelection(); // a selection from the folder you were just in shouldn't silently apply to the one you navigate to next either
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
		const name = prompt($t('fileBrowser.folderNamePrompt'));
		if (!name) return;
		try {
			await api.createFolder(name, currentFolderId);
			await load(currentFolderId);
		} catch (err) {
			error = err instanceof ApiError ? err.message : $t('fileBrowser.errors.couldNotCreateFolder');
		}
	}

	async function handleDelete(item: Item, event: MouseEvent) {
		event.stopPropagation();
		closeMenu();
		if (!confirm($t('common.confirmTrash', { name: item.name }))) return;
		try {
			await api.deleteItem(item.id);
			await load(currentFolderId);
		} catch (err) {
			error = err instanceof ApiError ? err.message : $t('common.errors.couldNotDelete');
		}
	}

	// api.move (PATCH /items/{id}) is a full replacement of name + location
	// (see CONTRIBUTING.md on why that's items' convention, unlike users'
	// partial-update PATCH) — a rename is just that call with the same
	// folder it's already in, only the name actually changing.
	async function handleRename(item: Item, event: MouseEvent) {
		event.stopPropagation();
		closeMenu();
		const newName = prompt($t('common.newNamePrompt'), item.name);
		if (!newName || newName === item.name) return;
		try {
			await api.move(item.id, newName, currentFolderId);
			await load(currentFolderId);
		} catch (err) {
			error = err instanceof ApiError ? err.message : $t('common.errors.couldNotRename');
		}
	}

	function handleStartMove(item: Item, event: MouseEvent) {
		event.stopPropagation();
		closeMenu();
		movingItems = [item];
	}

	function handleStartShare(item: Item, event: MouseEvent) {
		event.stopPropagation();
		closeMenu();
		sharingItem = item;
	}

	let statusMessage = $state('');
	let statusMessageTimeout: ReturnType<typeof setTimeout> | undefined;

	function showStatus(message: string) {
		statusMessage = message;
		clearTimeout(statusMessageTimeout);
		statusMessageTimeout = setTimeout(() => (statusMessage = ''), 4000);
	}

	async function handleCopyLink(item: Item, event: MouseEvent) {
		event.stopPropagation();
		closeMenu();
		try {
			const { url, copied } = await copyShareLink(item.id);
			showStatus(
				copied
					? $t('fileBrowser.linkCopied', { name: item.name })
					: $t('common.linkCreatedManualCopy', { url })
			);
		} catch (err) {
			error = err instanceof ApiError ? err.message : $t('common.errors.couldNotCreateShareLink');
		}
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
			error = err instanceof ApiError ? err.message : $t('common.errors.couldNotCopy');
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
			error = $t('common.errors.couldNotDownload');
		}
	}

	// --- bulk actions (selection toolbar) --------------------------------------

	async function handleBulkDelete() {
		const targets = selectedItems;
		if (targets.length === 0) return;
		if (!confirm($t('common.confirmTrashMultiple', { count: targets.length }))) return;
		const results = await Promise.allSettled(targets.map((item) => api.deleteItem(item.id)));
		const failed = results.filter((r) => r.status === 'rejected').length;
		clearSelection();
		await load(currentFolderId);
		if (failed > 0) error = $t('common.errors.couldNotDeleteSome', { count: failed });
	}

	// Each file triggers its own real browser download (same blob+<a>
	// trick as the single-file handleDownload above) — there's no backend
	// support for bundling several files into one zip, so a browser may
	// still show its own "this site is downloading multiple files"
	// permission prompt after the first couple. Folders are skipped
	// outright (downloading a whole folder isn't supported anywhere else
	// in this app either — see the file-preview page's own comment on the
	// public share landing page).
	async function handleBulkDownload() {
		for (const item of selectedItems) {
			if (item.type !== 'file') continue;
			try {
				const blob = await api.downloadContent(item.id);
				const url = URL.createObjectURL(blob);
				const a = document.createElement('a');
				a.href = url;
				a.download = item.name;
				a.click();
				URL.revokeObjectURL(url);
			} catch {
				error = $t('common.errors.couldNotDownload');
			}
		}
	}

	function handleBulkMove() {
		if (selectedItems.length > 0) movingItems = selectedItems;
	}

	function handleBulkShare() {
		if (selectedItems.length > 0) sharingItems = selectedItems;
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

	// No forced locale: the browser's own locale (whatever Fabio's device
	// is set to) decides the actual date format, the same way Drive/
	// Nextcloud's own "Last modified" column does — not hardcoded to any
	// one language.
	function formatDate(unixSeconds: number): string {
		return new Date(unixSeconds * 1000).toLocaleDateString(undefined, {
			year: 'numeric',
			month: 'short',
			day: 'numeric'
		});
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
	<h1 style="margin:0">{$t('fileBrowser.title')}</h1>
	<div class="search-box">
		<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true">
			<circle cx="11" cy="11" r="6" />
			<path d="m20 20-3.5-3.5" stroke-linecap="round" />
		</svg>
		<input
			type="search"
			placeholder={$t('fileBrowser.searchPlaceholder')}
			bind:value={searchQuery}
			aria-label={$t('fileBrowser.searchAriaLabel')}
		/>
	</div>
	<SortMenu
		fields={[
			{ key: 'name', label: $t('common.name') },
			{ key: 'modified', label: $t('common.modified') },
			{ key: 'size', label: $t('common.size') }
		]}
		bind:sortField
		bind:sortDirection
	/>
	<div class="view-toggle" role="group" aria-label={$t('fileBrowser.viewGroupLabel')}>
		<button
			class="view-toggle-btn"
			class:active={$viewMode === 'list'}
			aria-label={$t('fileBrowser.listView')}
			aria-pressed={$viewMode === 'list'}
			onclick={() => viewMode.set('list')}
		>
			<svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" aria-hidden="true">
				<path d="M4 6h16M4 12h16M4 18h16" stroke-linecap="round" />
			</svg>
		</button>
		<button
			class="view-toggle-btn"
			class:active={$viewMode === 'grid'}
			aria-label={$t('fileBrowser.gridView')}
			aria-pressed={$viewMode === 'grid'}
			onclick={() => viewMode.set('grid')}
		>
			<svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" aria-hidden="true">
				<rect x="4" y="4" width="7" height="7" rx="1.5" />
				<rect x="13" y="4" width="7" height="7" rx="1.5" />
				<rect x="4" y="13" width="7" height="7" rx="1.5" />
				<rect x="13" y="13" width="7" height="7" rx="1.5" />
			</svg>
		</button>
	</div>
	{#if currentFolderCanEdit}
		<div class="toolbar-actions">
			<button class="btn" onclick={() => fileInput.click()}>{$t('fileBrowser.upload')}</button>
			<button class="btn" onclick={() => (scanOpen = true)}>{$t('fileBrowser.scan')}</button>
			<button class="btn btn-primary" onclick={handleNewFolder}>{$t('fileBrowser.newFolder')}</button>
		</div>
	{/if}
</div>

{#if searchResults !== null}
	<p class="hint">{$t('fileBrowser.searchResultsHint', { query: searchQuery.trim(), count: searchResults.length })}</p>
{/if}

{#if selectedIds.size > 0}
	<div class="selection-toolbar">
		<button class="btn icon-btn" aria-label={$t('common.cancel')} onclick={clearSelection}>✕</button>
		<span>{$t('fileBrowser.selection.count', { count: selectedIds.size })}</span>
		<button class="btn" onclick={toggleSelectAll}>
			{selectedIds.size === visibleItems.length
				? $t('fileBrowser.selection.deselectAll')
				: $t('fileBrowser.selection.selectAll')}
		</button>
		<div class="selection-toolbar-actions">
			<button class="btn" onclick={handleBulkDownload} disabled={!selectedItems.some((i) => i.type === 'file')}>
				{$t('common.download')}
			</button>
			<button class="btn" onclick={handleBulkMove} disabled={!selectedItems.every((i) => i.can_edit)}>
				{$t('common.move')}
			</button>
			<button class="btn" onclick={handleBulkShare} disabled={!selectedItems.every((i) => i.owned)}>
				{$t('common.share')}
			</button>
			<button
				class="btn danger"
				onclick={handleBulkDelete}
				disabled={!selectedItems.every((i) => i.can_edit)}
			>
				{$t('common.delete')}
			</button>
		</div>
	</div>
{/if}

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
					<span class="upload-status-done">{$t('common.done')}</span>
					<button class="btn icon-btn" aria-label={$t('fileBrowser.dismiss')} onclick={() => dismissUpload(entry.id)}>✕</button>
				{:else}
					<span class="error-text">{entry.error ?? $t('common.failed')}</span>
					<button class="btn icon-btn" aria-label={$t('fileBrowser.dismiss')} onclick={() => dismissUpload(entry.id)}>✕</button>
				{/if}
			</div>
		{/each}
	</div>
{/if}

{#if error}
	<p class="error-text">{error}</p>
{/if}
{#if statusMessage}
	<p class="status-text">{statusMessage}</p>
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
	{#if loading || (searching && searchResults === null)}
		<p>{$t('common.loading')}</p>
	{:else if visibleItems.length === 0}
		<div class="empty-state">
			{searchQuery.trim() ? $t('fileBrowser.noSearchMatches', { query: searchQuery }) : $t('fileBrowser.emptyFolder')}
		</div>
	{:else}
		{#snippet rowMenu(item: Item)}
			<div class="row-menu">
				<!-- "Who has access" at a glance — only ever set on an owned
				     item (see Item.shared_with's own comment); hover for the
				     full list, click to jump straight into the dialog that
				     manages it. -->
				{#if item.shared_with && item.shared_with.length > 0}
					<button
						class="shared-badge"
						title={item.shared_with.join(', ')}
						aria-label={$t('dialogs.share.sharedWithBadge', { names: item.shared_with.join(', ') })}
						onclick={(e) => handleStartShare(item, e)}
					>
						<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true">
							<circle cx="6" cy="12" r="2.2" />
							<circle cx="17" cy="6" r="2.2" />
							<circle cx="17" cy="18" r="2.2" />
							<path d="M8 10.8 15 7M8 13.2 15 17" stroke-linecap="round" />
						</svg>
					</button>
				{/if}
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
						{#if item.type === 'file'}
							<button role="menuitem" onclick={(e) => handleDownload(item, e)}>
								<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" aria-hidden="true">
									<path d="M12 4v11M8 11l4 4 4-4M5 19h14" stroke-linecap="round" stroke-linejoin="round" />
								</svg>
								{$t('common.download')}
							</button>
						{/if}
						{#if item.can_edit}
							<button role="menuitem" onclick={(e) => handleRename(item, e)}>
								<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" aria-hidden="true">
									<path
										d="M4 20h4L18.5 9.5a1.5 1.5 0 0 0 0-2.1l-1.9-1.9a1.5 1.5 0 0 0-2.1 0L4 16v4Z"
										stroke-linecap="round"
										stroke-linejoin="round"
									/>
									<path d="M13 6.5l4 4" stroke-linecap="round" />
								</svg>
								{$t('common.rename')}
							</button>
							<button role="menuitem" onclick={(e) => handleStartMove(item, e)}>
								<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" aria-hidden="true">
									<path
										d="M3 7a1 1 0 0 1 1-1h4l1.5 1.5H20a1 1 0 0 1 1 1V17a1 1 0 0 1-1 1H4a1 1 0 0 1-1-1V7Z"
										stroke-linecap="round"
										stroke-linejoin="round"
									/>
									<path d="M9 13h6M12 10l3 3-3 3" stroke-linecap="round" stroke-linejoin="round" />
								</svg>
								{$t('common.move')}
							</button>
						{/if}
						<button role="menuitem" onclick={(e) => handleCopy(item, e)}>
							<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" aria-hidden="true">
								<rect x="8" y="8" width="12" height="12" rx="2" stroke-linecap="round" stroke-linejoin="round" />
								<path d="M16 8V5a1 1 0 0 0-1-1H5a1 1 0 0 0-1 1v10a1 1 0 0 0 1 1h3" stroke-linecap="round" stroke-linejoin="round" />
							</svg>
							{$t('common.makeACopy')}
						</button>
						{#if item.owned}
							<button role="menuitem" onclick={(e) => handleStartShare(item, e)}>
								<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" aria-hidden="true">
									<circle cx="6" cy="12" r="2.2" />
									<circle cx="17" cy="6" r="2.2" />
									<circle cx="17" cy="18" r="2.2" />
									<path d="M8 10.8 15 7M8 13.2 15 17" stroke-linecap="round" />
								</svg>
								{$t('common.share')}
							</button>
							<button role="menuitem" onclick={(e) => handleCopyLink(item, e)}>
								<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" aria-hidden="true">
									<path
										d="M9.5 14.5 14.5 9.5M8 12.5l-2 2a3 3 0 0 0 4.24 4.24l2-2M16 11.5l2-2a3 3 0 0 0-4.24-4.24l-2 2"
										stroke-linecap="round"
										stroke-linejoin="round"
									/>
								</svg>
								{$t('common.copyLink')}
							</button>
						{/if}
						{#if item.can_edit}
							<button role="menuitem" class="danger" onclick={(e) => handleDelete(item, e)}>
								<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" aria-hidden="true">
									<path
										d="M5 7h14M9 7V5a1 1 0 0 1 1-1h4a1 1 0 0 1 1 1v2M7 7l1 13a1 1 0 0 0 1 1h6a1 1 0 0 0 1-1l1-13"
										stroke-linecap="round"
										stroke-linejoin="round"
									/>
								</svg>
								{$t('common.delete')}
							</button>
						{/if}
						<button class="dropdown-menu-cancel" onclick={closeMenu}>{$t('common.cancel')}</button>
					</div>
				{/if}
			</div>
		{/snippet}

		{#if $viewMode === 'list'}
			<div class="item-list-header">
				<span class="item-icon">
					<input
						type="checkbox"
						checked={visibleItems.length > 0 && selectedIds.size === visibleItems.length}
						aria-label={selectedIds.size === visibleItems.length
							? $t('fileBrowser.selection.deselectAll')
							: $t('fileBrowser.selection.selectAll')}
						onchange={toggleSelectAll}
					/>
				</span>
				<button class="sort-header item-name-header" onclick={() => toggleSort('name')}>
					{$t('common.name')}
					{#if sortField === 'name'}<SortArrow direction={sortDirection} />{/if}
				</button>
				<button class="sort-header item-modified" onclick={() => toggleSort('modified')}>
					{$t('common.modified')}
					{#if sortField === 'modified'}<SortArrow direction={sortDirection} />{/if}
				</button>
				<button class="sort-header item-size" onclick={() => toggleSort('size')}>
					{$t('common.size')}
					{#if sortField === 'size'}<SortArrow direction={sortDirection} />{/if}
				</button>
				<span class="row-menu"></span>
			</div>
			<div class="item-list" class:has-selection={selectedIds.size > 0}>
				{#each visibleItems as item (item.id)}
				<div class="item-row" class:item-row-selected={selectedIds.has(item.id)}>
					<span class="item-icon">
						<input
							type="checkbox"
							class="row-checkbox"
							checked={selectedIds.has(item.id)}
							aria-label={$t('fileBrowser.selection.selectAriaLabel', { name: item.name })}
							onclick={(e) => toggleSelect(item.id, e)}
						/>
						<FileIcon type={item.type} name={item.name} mimeType={item.mime_type} />
					</span>
					<button
						class="item-name"
						onpointerdown={() => startLongPress(item.id)}
						onpointerup={cancelLongPress}
						onpointerleave={cancelLongPress}
						onpointercancel={cancelLongPress}
						onpointermove={cancelLongPress}
						oncontextmenu={(e) => e.preventDefault()}
						onclick={() => handleItemActivate(item)}
					>
						{item.name}
					</button>
					<span class="item-modified">{formatDate(item.updated_at)}</span>
					<span class="item-size">{formatSize(item.size_bytes)}</span>
					{@render rowMenu(item)}
				</div>
				{/each}
			</div>
		{:else}
			<div class="item-grid" class:has-selection={selectedIds.size > 0}>
				{#each visibleItems as item (item.id)}
					<div class="item-tile" class:item-row-selected={selectedIds.has(item.id)}>
						<input
							type="checkbox"
							class="row-checkbox tile-checkbox"
							checked={selectedIds.has(item.id)}
							aria-label={$t('fileBrowser.selection.selectAriaLabel', { name: item.name })}
							onclick={(e) => toggleSelect(item.id, e)}
						/>
						{@render rowMenu(item)}
						<button
							class="item-tile-main"
							onpointerdown={() => startLongPress(item.id)}
							onpointerup={cancelLongPress}
							onpointerleave={cancelLongPress}
							onpointercancel={cancelLongPress}
							onpointermove={cancelLongPress}
							oncontextmenu={(e) => e.preventDefault()}
							onclick={() => handleItemActivate(item)}
						>
							<FileIcon type={item.type} name={item.name} mimeType={item.mime_type} size="2.75rem" />
							<span class="item-tile-name">{item.name}</span>
							{#if item.type === 'file'}
								<span class="item-tile-size">{formatSize(item.size_bytes)}</span>
							{/if}
						</button>
					</div>
				{/each}
			</div>
		{/if}
	{/if}
</div>

{#if currentFolderCanEdit && selectedIds.size === 0}
	<button
		class="fab"
		aria-label={$t('fileBrowser.addAriaLabel')}
		aria-haspopup="true"
		aria-expanded={fabMenuOpen}
		onclick={(e) => {
			e.stopPropagation();
			fabMenuOpen = !fabMenuOpen;
		}}
	>
		<svg width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" aria-hidden="true">
			<path d="M12 5v14M5 12h14" stroke-linecap="round" />
		</svg>
	</button>
{/if}
{#if fabMenuOpen}
	<!-- svelte-ignore a11y_no_static_element_interactions -->
	<!-- svelte-ignore a11y_click_events_have_key_events -->
	<div class="menu-backdrop" onclick={() => (fabMenuOpen = false)}></div>
	<!-- svelte-ignore a11y_no_static_element_interactions -->
	<!-- svelte-ignore a11y_interactive_supports_focus -->
	<!-- svelte-ignore a11y_click_events_have_key_events -->
	<div class="dropdown-menu fab-menu" onclick={(e) => e.stopPropagation()} role="menu">
		<button
			role="menuitem"
			onclick={() => {
				fabMenuOpen = false;
				fileInput.click();
			}}
		>
			<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" aria-hidden="true">
				<path d="M12 15V4M8 8l4-4 4 4M5 20h14" stroke-linecap="round" stroke-linejoin="round" />
			</svg>
			{$t('fileBrowser.uploadPlain')}
		</button>
		<button
			role="menuitem"
			onclick={() => {
				fabMenuOpen = false;
				scanOpen = true;
			}}
		>
			<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" aria-hidden="true">
				<path d="M8 7l1.2-2h5.6L16 7h3a2 2 0 0 1 2 2v9a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V9a2 2 0 0 1 2-2h3Z" stroke-linejoin="round" />
				<circle cx="12" cy="13.5" r="3.2" />
			</svg>
			{$t('fileBrowser.scanPlain')}
		</button>
		<button
			role="menuitem"
			onclick={() => {
				fabMenuOpen = false;
				handleNewFolder();
			}}
		>
			<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" aria-hidden="true">
				<path
					d="M4 6a2 2 0 0 1 2-2h4l2 2h6a2 2 0 0 1 2 2v9a2 2 0 0 1-2 2H6a2 2 0 0 1-2-2V6Z"
					stroke-linejoin="round"
				/>
				<path d="M12 11v4M10 13h4" stroke-linecap="round" />
			</svg>
			{$t('fileBrowser.newFolderPlain')}
		</button>
		<button class="dropdown-menu-cancel" onclick={() => (fabMenuOpen = false)}>{$t('common.cancel')}</button>
	</div>
{/if}

<ShareDialog bind:item={sharingItem} />
<BulkShareDialog bind:items={sharingItems} />
<MoveDialog
	bind:items={movingItems}
	onMoved={() => {
		clearSelection();
		load(currentFolderId);
	}}
/>
<ScanDialog bind:open={scanOpen} onScanned={handleScanned} />
