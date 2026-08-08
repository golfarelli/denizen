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
	let movingItem = $state<Item | null>(null);
	let scanOpen = $state(false);
	// Filters the *current folder's own* listing only — a real search
	// across the whole tree would need a backend endpoint (and a decision
	// about how deep/fast that should be) this app doesn't have yet, so
	// this stays a client-side, current-folder-only filter rather than
	// pretending to be more than that.
	let searchQuery = $state('');

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

	let visibleItems = $derived(
		sortItems(
			searchQuery.trim()
				? items.filter((item) => item.name.toLowerCase().includes(searchQuery.trim().toLowerCase()))
				: items,
			sortField,
			sortDirection
		)
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
		movingItem = item;
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
	{#if loading}
		<p>{$t('common.loading')}</p>
	{:else if items.length === 0}
		<div class="empty-state">{$t('fileBrowser.emptyFolder')}</div>
	{:else if visibleItems.length === 0}
		<div class="empty-state">{$t('fileBrowser.noSearchMatches', { query: searchQuery })}</div>
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
				<span class="item-icon"></span>
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
					<span class="item-modified">{formatDate(item.updated_at)}</span>
					<span class="item-size">{formatSize(item.size_bytes)}</span>
					{@render rowMenu(item)}
				</div>
				{/each}
			</div>
		{:else}
			<div class="item-grid">
				{#each visibleItems as item (item.id)}
					<div class="item-tile">
						{@render rowMenu(item)}
						<button
							class="item-tile-main"
							onclick={() => (item.type === 'folder' ? openFolder(item.id) : openFile(item.id))}
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

{#if currentFolderCanEdit}
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
<MoveDialog bind:item={movingItem} onMoved={() => load(currentFolderId)} />
<ScanDialog bind:open={scanOpen} onScanned={handleScanned} />
