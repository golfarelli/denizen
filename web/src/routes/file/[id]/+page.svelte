<script lang="ts">
	import { onMount, onDestroy } from 'svelte';
	import { page } from '$app/stores';
	import { goto } from '$app/navigation';
	import { api, ApiError, type Item } from '$lib/api';
	import { fullscreen } from '$lib/fullscreen';
	import { copyShareLink } from '$lib/copyShareLink';
	import { previewKind, type PreviewKind } from '$lib/previewKind';
	import MoveDialog from '$lib/MoveDialog.svelte';
	import ShareDialog from '$lib/ShareDialog.svelte';
	import { t } from '$lib/i18n';
	// Dynamically imported below (`{#await import(...)}`), not statically
	// here: pdf.js + docx-preview + xlsx together are a genuinely heavy
	// ~290KB (gzipped) payload, and a static import would bundle all three
	// into this route's chunk regardless of which one file type someone
	// actually opens — wasted download even for something as small as a
	// text file or an image.

	type OnlyOfficeStatus = { enabled: boolean; api_js_url?: string };

	let item = $state<Item | null>(null);
	let loading = $state(true);
	let error = $state('');
	let kind = $state<PreviewKind>('unsupported');
	let objectUrl = $state(''); // image previews only — see PdfViewer for why pdf doesn't use one
	// pdf/docx/xlsx all just want the whole file as a Blob and do their own
	// parsing from there (unlike image/video, none of them can take a
	// direct src) — one shared holder instead of three near-identical ones.
	// Never populated for docx/xlsx when OnlyOffice is handling them instead
	// (see onMount) — it does its own fetching, server-side.
	let officeBlob = $state<Blob | null>(null);
	let textContent = $state('');
	let videoUrl = $state(''); // a real URL (content-token query param), not a blob: one — see getContentToken
	// null until checked; only checked at all for docx/xlsx/pptx/pdf, since
	// nothing else cares — notably not images, which OnlyOffice has never
	// supported (it's a document-editing engine, not an image viewer; see
	// onlyoffice.DocumentType's own comment). See onMount and
	// OnlyOfficeViewer.svelte.
	let onlyOffice = $state<OnlyOfficeStatus | null>(null);

	// Set by whichever list navigated here (routes/+page.svelte, routes/
	// trash/+page.svelte, routes/shares/+page.svelte) so "Back" returns to
	// where the user actually came from — a real folder id, or one of the
	// two fixed non-folder lists that also open files this way now — not
	// always the root. Falls back to root for a direct link/bookmark that
	// never went through any of them.
	let backHref = $derived.by(() => {
		const from = $page.url.searchParams.get('from');
		if (!from) return '/';
		if (from === 'trash') return '/trash';
		if (from === 'shares') return '/shares';
		if (from === 'shared-with-me') return '/shared-with-me';
		return `/?folder=${encodeURIComponent(from)}`;
	});

	onMount(async () => {
		// This page's viewers (OnlyOffice above all, but really any of them
		// on a phone) benefit far more from real screen space than Denizen's
		// own nav chrome being visible while looking at a file does — see
		// lib/fullscreen.ts and routes/+layout.svelte. Set synchronously
		// (before the first await below) so the chrome disappears the moment
		// this page mounts, not after the fetch resolves.
		fullscreen.set(true);

		// Always present at runtime for a matched /file/[id] route — the type
		// only allows undefined because SvelteKit's params type is shared
		// with routes that don't have this segment at all.
		const id = $page.params.id as string;

		try {
			item = await api.getItem(id);
			kind = previewKind(item.name, item.mime_type);

			if (kind === 'unsupported') return; // no bytes to fetch — nothing to preview

			if (kind === 'video') {
				// A real <video src>, not a blob: URL: unlike an image/PDF, a
				// video benefits from actual HTTP Range streaming (starts
				// immediately, seeks without downloading the whole file
				// first) — only a direct element src gets that, so this
				// mints a short-lived token instead of fetching bytes here.
				const minted = await api.getContentToken(id);
				videoUrl = `/api/v1/items/${id}/content?token=${encodeURIComponent(minted.token)}`;
				return;
			}

			if (kind === 'docx' || kind === 'xlsx' || kind === 'pptx' || kind === 'pdf') {
				// Real fidelity + real editing if a Document Server is
				// configured, checked before deciding whether to fetch
				// anything here at all — OnlyOffice does its own fetching,
				// server-side, from the URL its config points at. The Config
				// endpoint (internal/handler/onlyoffice.go) now allows a
				// share recipient in too — real edit mode at 'edit'
				// permission, read-only at 'view' (same server-side gate
				// either way, driven by item.can_edit, not decided here) —
				// unlike the public share landing page, which still skips
				// OnlyOffice entirely since a link has no identity to check
				// a permission against (see routes/s/[token]/+page.svelte).
				onlyOffice = await api.getOnlyOfficeStatus();
				if (onlyOffice.enabled) {
					loading = false;
					return;
				}
				if (kind === 'pptx') {
					// No client-side fallback exists for PowerPoint — this
					// was only ever previewable through OnlyOffice.
					kind = 'unsupported';
					loading = false;
					return;
				}
				// docx/xlsx/pdf fall through to the blob-fetch path below —
				// same client-side viewers as before OnlyOffice existed (pdf.js
				// for pdf specifically).
			}

			// <img> can't carry the Authorization header content needs (same
			// constraint as download — see api.downloadContent's own
			// comment), so the bytes are fetched here and handed to the
			// viewer as a blob: URL / in-memory text / raw Blob (pdf/docx/
			// xlsx — each viewer does its own reading from there) instead of
			// a direct src.
			const blob = await api.downloadContent(id);
			if (kind === 'text') {
				textContent = await blob.text();
			} else if (kind === 'pdf' || kind === 'docx' || kind === 'xlsx') {
				officeBlob = blob;
			} else {
				objectUrl = URL.createObjectURL(blob);
			}
		} catch (err) {
			error = err instanceof ApiError ? err.message : $t('filePreview.errors.couldNotLoad');
		} finally {
			loading = false;
		}
	});

	onDestroy(() => {
		if (objectUrl) URL.revokeObjectURL(objectUrl);
		fullscreen.set(false);
	});

	async function handleDownload() {
		if (!item) return;
		try {
			// Reuse whatever's already been fetched/minted for the preview
			// (an object URL for images, the content-token URL for video, a
			// raw Blob for pdf/docx/xlsx) rather than fetching the same
			// bytes twice or minting a second token; text/unsupported
			// previews never got either, so get real bytes here for those.
			let url = objectUrl || videoUrl;
			let revoke = false;
			if (!url) {
				const blob = officeBlob ?? (await api.downloadContent(item.id));
				url = URL.createObjectURL(blob);
				revoke = true;
			}
			const a = document.createElement('a');
			a.href = url;
			a.download = item.name;
			a.click();
			if (revoke) URL.revokeObjectURL(url);
		} catch {
			error = $t('common.errors.couldNotDownload');
		}
	}

	// This page only ever has one item open at a time, unlike the list's
	// per-row openMenuFor map — a single boolean is enough.
	let menuOpen = $state(false);
	let movingItem = $state<Item | null>(null);
	let sharingItem = $state<Item | null>(null);
	let statusMessage = $state('');
	let statusMessageTimeout: ReturnType<typeof setTimeout> | undefined;

	function showStatus(message: string) {
		statusMessage = message;
		clearTimeout(statusMessageTimeout);
		statusMessageTimeout = setTimeout(() => (statusMessage = ''), 4000);
	}

	function toggleMenu(event: MouseEvent) {
		event.stopPropagation();
		menuOpen = !menuOpen;
	}

	function closeMenu() {
		menuOpen = false;
	}

	$effect(() => {
		if (!menuOpen) return;
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

	async function handleRename() {
		closeMenu();
		if (!item) return;
		const newName = prompt($t('common.newNamePrompt'), item.name);
		if (!newName || newName === item.name) return;
		try {
			await api.move(item.id, newName, item.parent_id);
			item.name = newName;
		} catch (err) {
			error = err instanceof ApiError ? err.message : $t('common.errors.couldNotRename');
		}
	}

	function handleStartMove() {
		closeMenu();
		movingItem = item;
	}

	async function handleCopy() {
		closeMenu();
		if (!item) return;
		try {
			await api.copyItem(item.id, item.parent_id);
			showStatus($t('filePreview.copyCreated', { name: item.name }));
		} catch (err) {
			error = err instanceof ApiError ? err.message : $t('common.errors.couldNotCopy');
		}
	}

	function handleStartShare() {
		closeMenu();
		sharingItem = item;
	}

	async function handleCopyLink() {
		closeMenu();
		if (!item) return;
		try {
			const { url, copied } = await copyShareLink(item.id);
			showStatus(copied ? $t('filePreview.linkCopied') : $t('common.linkCreatedManualCopy', { url }));
		} catch (err) {
			error = err instanceof ApiError ? err.message : $t('common.errors.couldNotCreateShareLink');
		}
	}

	async function handleDelete() {
		closeMenu();
		if (!item) return;
		if (!confirm($t('common.confirmTrash', { name: item.name }))) return;
		try {
			await api.deleteItem(item.id);
			goto(backHref);
		} catch (err) {
			error = err instanceof ApiError ? err.message : $t('common.errors.couldNotDelete');
		}
	}
</script>

<svelte:head>
	<title>{item?.name ?? $t('filePreview.fallbackTitle')} · Denizen</title>
</svelte:head>

<div class="preview-page">
	<!-- Deliberately outside the loading/error/item branches below and kept
	     to one slim row: this is the only nav left once fullscreen hides the
	     app's own topbar (see onMount above), so Back needs to stay reachable
	     even while the file is still loading or failed to load, not just once
	     item is populated. -->
	<header class="preview-header">
		<a href={backHref} class="preview-back" aria-label={$t('filePreview.back')}>←</a>
		<span class="preview-title">{item?.name ?? $t('common.loading')}</span>
		{#if item}
			<div class="row-menu">
				<button
					class="btn icon-btn"
					aria-label={$t('common.actionsFor', { name: item.name })}
					aria-haspopup="true"
					aria-expanded={menuOpen}
					onclick={toggleMenu}
				>
					<svg width="18" height="18" viewBox="0 0 24 24" aria-hidden="true">
						<circle cx="12" cy="5" r="1.6" fill="currentColor" />
						<circle cx="12" cy="12" r="1.6" fill="currentColor" />
						<circle cx="12" cy="19" r="1.6" fill="currentColor" />
					</svg>
				</button>
				{#if menuOpen}
					<!-- svelte-ignore a11y_no_static_element_interactions -->
					<!-- svelte-ignore a11y_click_events_have_key_events -->
					<div class="menu-backdrop" onclick={closeMenu}></div>
					<!-- svelte-ignore a11y_no_static_element_interactions -->
					<!-- svelte-ignore a11y_interactive_supports_focus -->
					<!-- svelte-ignore a11y_click_events_have_key_events -->
					<div class="dropdown-menu" onclick={(e) => e.stopPropagation()} role="menu">
						<button role="menuitem" onclick={handleDownload}>
							<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" aria-hidden="true">
								<path d="M12 4v11M8 11l4 4 4-4M5 19h14" stroke-linecap="round" stroke-linejoin="round" />
							</svg>
							{$t('common.download')}
						</button>
						{#if item.can_edit}
							<button role="menuitem" onclick={handleRename}>
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
							<button role="menuitem" onclick={handleStartMove}>
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
						<button role="menuitem" onclick={handleCopy}>
							<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" aria-hidden="true">
								<rect x="8" y="8" width="12" height="12" rx="2" stroke-linecap="round" stroke-linejoin="round" />
								<path d="M16 8V5a1 1 0 0 0-1-1H5a1 1 0 0 0-1 1v10a1 1 0 0 0 1 1h3" stroke-linecap="round" stroke-linejoin="round" />
							</svg>
							{$t('common.makeACopy')}
						</button>
						{#if item.owned}
							<button role="menuitem" onclick={handleStartShare}>
								<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" aria-hidden="true">
									<circle cx="6" cy="12" r="2.2" />
									<circle cx="17" cy="6" r="2.2" />
									<circle cx="17" cy="18" r="2.2" />
									<path d="M8 10.8 15 7M8 13.2 15 17" stroke-linecap="round" />
								</svg>
								{$t('common.share')}
							</button>
							<button role="menuitem" onclick={handleCopyLink}>
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
							<button role="menuitem" class="danger" onclick={handleDelete}>
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
		{/if}
	</header>
	{#if statusMessage}
		<p class="status-text preview-status">{statusMessage}</p>
	{/if}

	<div class="preview-content">
		{#if loading}
			<p>{$t('common.loading')}</p>
		{:else if error}
			<p class="error-text">{error}</p>
		{:else if item}
			{#if kind === 'image'}
				<div class="preview-frame">
					<img src={objectUrl} alt={item.name} />
				</div>
			{:else if kind === 'docx' || kind === 'xlsx' || kind === 'pptx' || kind === 'pdf'}
				{#if onlyOffice?.enabled}
					{#await import('$lib/OnlyOfficeViewer.svelte') then { default: OnlyOfficeViewer }}
						<OnlyOfficeViewer itemId={item.id} apiJsUrl={onlyOffice.api_js_url ?? ''} />
					{/await}
				{:else if kind === 'docx' && officeBlob}
					{#await import('$lib/DocxViewer.svelte') then { default: DocxViewer }}
						<DocxViewer blob={officeBlob} />
					{/await}
				{:else if kind === 'xlsx' && officeBlob}
					{#await import('$lib/XlsxViewer.svelte') then { default: XlsxViewer }}
						<XlsxViewer blob={officeBlob} />
					{/await}
				{:else if kind === 'pdf' && officeBlob}
					{#await import('$lib/PdfViewer.svelte') then { default: PdfViewer }}
						<PdfViewer blob={officeBlob} />
					{/await}
				{/if}
			{:else if kind === 'text'}
				<pre class="preview-text">{textContent}</pre>
			{:else if kind === 'video'}
				<div class="preview-frame">
					<!-- svelte-ignore a11y_media_has_caption -->
					<video src={videoUrl} controls></video>
				</div>
			{:else}
				<div class="empty-state">
					{$t('filePreview.noPreview1')}<br />
					{$t('filePreview.noPreview2')}
				</div>
			{/if}
		{/if}
	</div>
</div>

<MoveDialog bind:item={movingItem} onMoved={() => showStatus($t('filePreview.moved'))} />
<ShareDialog bind:item={sharingItem} />
