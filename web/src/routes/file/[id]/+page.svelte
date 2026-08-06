<script lang="ts">
	import { onMount, onDestroy } from 'svelte';
	import { page } from '$app/stores';
	import { api, ApiError, type Item } from '$lib/api';
	import { fullscreen } from '$lib/fullscreen';
	// Dynamically imported below (`{#await import(...)}`), not statically
	// here: pdf.js + docx-preview + xlsx together are a genuinely heavy
	// ~290KB (gzipped) payload, and a static import would bundle all three
	// into this route's chunk regardless of which one file type someone
	// actually opens — wasted download even for something as small as a
	// text file or an image.

	// Extensions treated as text even when the server's mime_type guess
	// (Go's mime.TypeByExtension, or whatever the browser reported as
	// File.type at upload time — see lib/upload.ts) is empty or generic:
	// Go's own mime type database is OS-registration-based and doesn't
	// reliably know about things like .md or .log.
	const TEXT_EXTENSIONS = new Set([
		'txt', 'md', 'markdown', 'json', 'csv', 'log', 'yaml', 'yml', 'xml',
		'html', 'htm', 'css', 'js', 'ts', 'ini', 'conf', 'toml', 'sh', 'go', 'py'
	]);

	// Keyed by extension, not mime_type: both are OOXML zip-based formats
	// with mime types Go's own guesser (mime.TypeByExtension) doesn't
	// reliably know, and the legacy binary predecessors (.doc, .xls) need
	// telling apart from these anyway since only one of the two libraries
	// below (xlsx, via SheetJS's bundled legacy parser) can actually read
	// its legacy sibling — .doc has no viewer here, only .xls does.

	type PreviewKind = 'image' | 'pdf' | 'docx' | 'xlsx' | 'pptx' | 'text' | 'video' | 'unsupported';
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
	// null until checked; only checked at all for docx/xlsx/pptx, since
	// nothing else cares. See onMount and OnlyOfficeViewer.svelte.
	let onlyOffice = $state<OnlyOfficeStatus | null>(null);

	// Set by the file list when navigating here (routes/+page.svelte) so
	// "Back" returns to the folder the user actually came from, not always
	// the root — falls back to root for a direct link/bookmark that never
	// went through the list.
	let backHref = $derived(
		$page.url.searchParams.has('from')
			? `/?folder=${encodeURIComponent($page.url.searchParams.get('from')!)}`
			: '/'
	);

	function previewKind(candidate: Item): PreviewKind {
		const mime = candidate.mime_type ?? '';
		const ext = candidate.name.split('.').pop()?.toLowerCase() ?? '';
		if (mime.startsWith('image/')) return 'image';
		if (mime === 'application/pdf') return 'pdf';
		if (mime.startsWith('video/')) return 'video';
		if (ext === 'docx') return 'docx';
		if (ext === 'xlsx' || ext === 'xls') return 'xlsx';
		if (ext === 'pptx') return 'pptx'; // only ever previewable via OnlyOffice — no client-side viewer for it
		if (mime.startsWith('text/') || TEXT_EXTENSIONS.has(ext)) return 'text';
		return 'unsupported';
	}

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
			kind = previewKind(item);

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

			if (kind === 'docx' || kind === 'xlsx' || kind === 'pptx') {
				// Real fidelity + editing (view-only for now — see
				// OnlyOfficeViewer's own comment) if a Document Server is
				// configured, checked before deciding whether to fetch
				// anything here at all — OnlyOffice does its own fetching,
				// server-side, from the URL its config points at.
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
				// docx/xlsx fall through to the blob-fetch path below —
				// same client-side viewers as before OnlyOffice existed.
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
			error = err instanceof ApiError ? err.message : 'Could not load this file.';
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
			error = 'Could not download this file.';
		}
	}
</script>

<svelte:head>
	<title>{item?.name ?? 'File'} · Denizen</title>
</svelte:head>

<div class="preview-page">
	<!-- Deliberately outside the loading/error/item branches below and kept
	     to one slim row: this is the only nav left once fullscreen hides the
	     app's own topbar (see onMount above), so Back needs to stay reachable
	     even while the file is still loading or failed to load, not just once
	     item is populated. -->
	<header class="preview-header">
		<a href={backHref} class="preview-back" aria-label="Back">←</a>
		<span class="preview-title">{item?.name ?? 'Loading…'}</span>
		{#if item}
			<button class="btn" onclick={handleDownload}>Download</button>
		{/if}
	</header>

	<div class="preview-content">
		{#if loading}
			<p>Loading…</p>
		{:else if error}
			<p class="error-text">{error}</p>
		{:else if item}
			{#if kind === 'image'}
				<div class="preview-frame">
					<img src={objectUrl} alt={item.name} />
				</div>
			{:else if kind === 'pdf'}
				{#if officeBlob}
					{#await import('$lib/PdfViewer.svelte') then { default: PdfViewer }}
						<PdfViewer blob={officeBlob} />
					{/await}
				{/if}
			{:else if kind === 'docx' || kind === 'xlsx' || kind === 'pptx'}
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
					Preview isn't available for this file type yet.<br />
					Use Download above to open it.
				</div>
			{/if}
		{/if}
	</div>
</div>
