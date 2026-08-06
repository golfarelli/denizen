<script lang="ts">
	import { onMount, onDestroy } from 'svelte';
	import { page } from '$app/stores';
	import { api, ApiError, type Item } from '$lib/api';
	import PdfViewer from '$lib/PdfViewer.svelte';

	// Extensions treated as text even when the server's mime_type guess
	// (Go's mime.TypeByExtension, or whatever the browser reported as
	// File.type at upload time — see lib/upload.ts) is empty or generic:
	// Go's own mime type database is OS-registration-based and doesn't
	// reliably know about things like .md or .log.
	const TEXT_EXTENSIONS = new Set([
		'txt', 'md', 'markdown', 'json', 'csv', 'log', 'yaml', 'yml', 'xml',
		'html', 'htm', 'css', 'js', 'ts', 'ini', 'conf', 'toml', 'sh', 'go', 'py'
	]);

	type PreviewKind = 'image' | 'pdf' | 'text' | 'unsupported';

	let item = $state<Item | null>(null);
	let loading = $state(true);
	let error = $state('');
	let kind = $state<PreviewKind>('unsupported');
	let objectUrl = $state(''); // image previews only — see PdfViewer for why pdf doesn't use one
	let pdfBlob = $state<Blob | null>(null);
	let textContent = $state('');

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
		if (mime.startsWith('text/') || TEXT_EXTENSIONS.has(ext)) return 'text';
		return 'unsupported';
	}

	onMount(async () => {
		// Always present at runtime for a matched /file/[id] route — the type
		// only allows undefined because SvelteKit's params type is shared
		// with routes that don't have this segment at all.
		const id = $page.params.id as string;

		try {
			item = await api.getItem(id);
			kind = previewKind(item);

			if (kind === 'unsupported') return; // no bytes to fetch — nothing to preview

			// <img> can't carry the Authorization header content needs (same
			// constraint as download — see api.downloadContent's own
			// comment), so the bytes are fetched here and handed to the
			// viewer as a blob: URL / in-memory text / raw Blob (pdf —
			// PdfViewer does its own arrayBuffer() read) instead of a direct
			// src.
			const blob = await api.downloadContent(id);
			if (kind === 'text') {
				textContent = await blob.text();
			} else if (kind === 'pdf') {
				pdfBlob = blob;
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
	});

	async function handleDownload() {
		if (!item) return;
		try {
			// Reuse whatever's already been fetched for the preview (an
			// object URL for images, a raw Blob for pdf) rather than
			// fetching the same bytes twice; text/unsupported previews
			// never fetched either, so get real bytes here for those.
			let url = objectUrl;
			let revoke = false;
			if (!url) {
				const blob = pdfBlob ?? (await api.downloadContent(item.id));
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

<nav class="breadcrumb">
	<a href={backHref}>← Back</a>
</nav>

{#if loading}
	<p>Loading…</p>
{:else if error}
	<p class="error-text">{error}</p>
{:else if item}
	<div class="toolbar">
		<h1 style="margin:0; word-break: break-word">{item.name}</h1>
		<button class="btn" onclick={handleDownload}>Download</button>
	</div>

	{#if kind === 'image'}
		<div class="preview-frame">
			<img src={objectUrl} alt={item.name} />
		</div>
	{:else if kind === 'pdf'}
		{#if pdfBlob}
			<PdfViewer blob={pdfBlob} />
		{/if}
	{:else if kind === 'text'}
		<pre class="preview-text">{textContent}</pre>
	{:else}
		<div class="empty-state">
			Preview isn't available for this file type yet.<br />
			Use Download above to open it.
		</div>
	{/if}
{/if}
