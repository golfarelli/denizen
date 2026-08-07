<script lang="ts">
	import { onMount, onDestroy } from 'svelte';
	import { page } from '$app/stores';
	import { api, ApiError, type PublicShareItem } from '$lib/api';
	import { fullscreen } from '$lib/fullscreen';
	import { previewKind, type PreviewKind } from '$lib/previewKind';
	import FileIcon from '$lib/FileIcon.svelte';
	// Dynamically imported below, same as the private preview page
	// (routes/file/[id]/+page.svelte) and for the same reason — pdf.js/
	// docx-preview/xlsx together are a genuinely heavy ~290KB (gzipped)
	// payload not worth paying for a share that turns out to be a text
	// file or an image. OnlyOffice is deliberately *not* one of the
	// viewers this page can reach for docx/xlsx/pptx/pdf, unlike the
	// private page: its Config endpoint requires an authenticated owner
	// (internal/handler/onlyoffice.go's ownerID(req)), which a visitor
	// here may not have at all — this page always uses the client-side
	// fallback viewers, the same ones the private page falls back to when
	// OnlyOffice isn't configured.

	let item = $state<PublicShareItem | null>(null);
	let loading = $state(true);
	let error = $state('');
	// Distinct from `error`: a requires_auth share the visitor hasn't
	// authenticated for yet isn't broken, it's just asking for a login —
	// a different UI (a "Log in" link, not an error message).
	let needsLogin = $state(false);
	let downloading = $state(false);
	let kind = $state<PreviewKind>('unsupported');
	let objectUrl = $state(''); // image previews
	// pdf/docx/xlsx all just want the whole file as a Blob and do their
	// own parsing from there — one shared holder, same as the private
	// page's own officeBlob. Also what handleDownload reuses instead of
	// fetching the same bytes twice.
	let contentBlob = $state<Blob | null>(null);
	let textContent = $state('');
	// Either a direct, un-authenticated stream URL (public shares — real
	// HTTP Range support, the token in the URL path is all the auth this
	// route needs) or an object: URL from a blob fetch (requires_auth
	// shares — a plain <video src> has no way to attach the Authorization
	// header that needs, so it's fetched the same way images/pdf/docx/
	// xlsx already are here).
	let videoUrl = $state('');
	let videoUrlIsBlob = false;

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

	onMount(async () => {
		// This page's whole point is to work for a visitor who may not
		// have — or be logged into — a Denizen account at all; Denizen's
		// own nav chrome (which assumes a logged-in owner: My shares,
		// Trash, Admin) would be actively wrong to show here. Set
		// regardless of $auth, not just when logged out — a visitor who
		// *does* happen to be logged in still gets the plain landing page,
		// not the full app shell. See lib/fullscreen.ts.
		fullscreen.set(true);

		const token = $page.params.token as string;
		try {
			item = await api.getShareMeta(token);
			if (item.type !== 'file') return; // nothing to preview for a folder
			kind = previewKind(item.name, item.mime_type);
			if (kind === 'unsupported') return;

			if (kind === 'video') {
				if (!item.requires_auth) {
					videoUrl = `/s/${token}/content`;
				} else {
					contentBlob = await api.downloadSharedContent(token);
					videoUrl = URL.createObjectURL(contentBlob);
					videoUrlIsBlob = true;
				}
				return;
			}

			const blob = await api.downloadSharedContent(token);
			contentBlob = blob;
			if (kind === 'image') {
				objectUrl = URL.createObjectURL(blob);
			} else if (kind === 'text') {
				textContent = await blob.text();
			}
			// pdf/docx/xlsx: contentBlob alone is enough, same as the
			// private page's own officeBlob — each viewer reads from it
			// directly.
		} catch (err) {
			if (err instanceof ApiError && err.status === 401) {
				needsLogin = true;
			} else if (err instanceof ApiError && err.status === 404) {
				// Resolve (internal/service/share.go) deliberately makes an
				// unknown/revoked/expired token all look identical from the
				// outside — mirrored here rather than trying to tell them
				// apart in the UI either.
				error = 'This link is no longer available.';
			} else {
				error = err instanceof ApiError ? err.message : 'Could not load this share.';
			}
		} finally {
			loading = false;
		}
	});

	onDestroy(() => {
		fullscreen.set(false);
		if (objectUrl) URL.revokeObjectURL(objectUrl);
		if (videoUrlIsBlob && videoUrl) URL.revokeObjectURL(videoUrl);
	});

	async function handleDownload() {
		if (!item) return;
		downloading = true;
		try {
			const blob = contentBlob ?? (await api.downloadSharedContent($page.params.token as string));
			const url = URL.createObjectURL(blob);
			const a = document.createElement('a');
			a.href = url;
			a.download = item.name;
			a.click();
			URL.revokeObjectURL(url);
		} catch {
			error = 'Could not download this file.';
		} finally {
			downloading = false;
		}
	}
</script>

<svelte:head>
	<title>{item?.name ?? 'Shared file'} · Denizen</title>
</svelte:head>

<div class="preview-page">
	<header class="preview-header">
		<a class="preview-back" href="/" aria-label="Denizen">
			<svg width="22" height="22" viewBox="0 0 24 24" aria-hidden="true">
				<rect width="24" height="24" rx="6" fill="var(--color-accent)" />
				<path
					fill="var(--color-accent-contrast)"
					d="M6.5 7a1 1 0 0 1 1-1h3.2l1.3 1.3H17a1 1 0 0 1 1 1v7.2a1 1 0 0 1-1 1H7.5a1 1 0 0 1-1-1V7Z"
				/>
			</svg>
		</a>
		<span class="preview-title">{item?.name ?? (loading ? 'Loading…' : 'Denizen')}</span>
		{#if item?.type === 'file'}
			<button class="btn btn-primary" onclick={handleDownload} disabled={downloading}>
				{downloading ? 'Downloading…' : 'Download'}
			</button>
		{/if}
	</header>

	<div class="preview-content">
		{#if loading}
			<p>Loading…</p>
		{:else if needsLogin}
			<div class="share-card">
				<p>This link requires you to be logged in to a Denizen account to view it.</p>
				<a class="btn btn-primary" href="/login?then={encodeURIComponent($page.url.pathname)}">Log in</a>
			</div>
		{:else if error}
			<div class="share-card">
				<p class="error-text">{error}</p>
			</div>
		{:else if item?.type === 'folder'}
			<div class="share-card">
				<div class="share-card-icon"><FileIcon type="folder" name={item.name} size="3.5rem" /></div>
				<h1>{item.name}</h1>
				<p class="hint">Folder</p>
				<p class="hint">
					Shared folders can't be downloaded as a whole yet — ask whoever shared this with you
					for the individual files instead.
				</p>
			</div>
		{:else if item}
			{#if kind === 'image'}
				<div class="preview-frame">
					<img src={objectUrl} alt={item.name} />
				</div>
			{:else if kind === 'pdf' && contentBlob}
				{#await import('$lib/PdfViewer.svelte') then { default: PdfViewer }}
					<PdfViewer blob={contentBlob} />
				{/await}
			{:else if kind === 'docx' && contentBlob}
				{#await import('$lib/DocxViewer.svelte') then { default: DocxViewer }}
					<DocxViewer blob={contentBlob} />
				{/await}
			{:else if kind === 'xlsx' && contentBlob}
				{#await import('$lib/XlsxViewer.svelte') then { default: XlsxViewer }}
					<XlsxViewer blob={contentBlob} />
				{/await}
			{:else if kind === 'text'}
				<pre class="preview-text">{textContent}</pre>
			{:else if kind === 'video'}
				<div class="preview-frame">
					<!-- svelte-ignore a11y_media_has_caption -->
					<video src={videoUrl} controls></video>
				</div>
			{:else}
				<div class="share-card">
					<div class="share-card-icon"><FileIcon type={item.type} name={item.name} size="3.5rem" /></div>
					<h1>{item.name}</h1>
					<p class="hint">{formatSize(item.size_bytes)}</p>
					<p class="hint">Preview isn't available for this file type — use Download above.</p>
				</div>
			{/if}
		{/if}
	</div>
</div>
