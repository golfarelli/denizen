<script lang="ts">
	import { onMount, onDestroy } from 'svelte';
	import { page } from '$app/stores';
	import { api, ApiError, type PublicShareItem } from '$lib/api';
	import { fullscreen } from '$lib/fullscreen';
	import FileIcon from '$lib/FileIcon.svelte';

	// Extensions FileIcon would happily categorize but this page can't
	// actually preview inline — kept small and specific to "worth fetching
	// the whole file just to show a picture", the same bar the private
	// preview page (routes/file/[id]/+page.svelte) uses for its own image
	// case, not an attempt to cover every viewable type here too. A public
	// share landing page is meant to be light — pdf.js/docx-preview/xlsx
	// are the same "genuinely heavy" dependencies noted in
	// docs/ARCHITECTURE.md, not something this page pulls in just for a
	// nicer preview of a link that's mostly going to be "here's a photo".
	const IMAGE_EXT = new Set(['jpg', 'jpeg', 'png', 'gif', 'webp', 'svg', 'bmp', 'heic', 'avif']);

	let item = $state<PublicShareItem | null>(null);
	let loading = $state(true);
	let error = $state('');
	// Distinct from `error`: a requires_auth share the visitor hasn't
	// authenticated for yet isn't broken, it's just asking for a login —
	// a different UI (a "Log in" link, not an error message).
	let needsLogin = $state(false);
	let downloading = $state(false);
	// Populated only for images (see IMAGE_EXT above) — kept as a real
	// Blob, not just its object URL, so handleDownload can reuse the same
	// bytes instead of fetching the file twice.
	let contentBlob = $state<Blob | null>(null);
	let objectUrl = $state('');

	function isImage(name: string): boolean {
		const ext = name.split('.').pop()?.toLowerCase() ?? '';
		return IMAGE_EXT.has(ext);
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
			if (item.type === 'file' && isImage(item.name)) {
				contentBlob = await api.downloadSharedContent(token);
				objectUrl = URL.createObjectURL(contentBlob);
			}
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

<div class="share-page">
	<header class="share-page-header">
		<a class="brand" href="/">
			<svg width="22" height="22" viewBox="0 0 24 24" aria-hidden="true">
				<rect width="24" height="24" rx="6" fill="var(--color-accent)" />
				<path
					fill="var(--color-accent-contrast)"
					d="M6.5 7a1 1 0 0 1 1-1h3.2l1.3 1.3H17a1 1 0 0 1 1 1v7.2a1 1 0 0 1-1 1H7.5a1 1 0 0 1-1-1V7Z"
				/>
			</svg>
			Denizen
		</a>
	</header>

	<div class="share-page-content">
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
		{:else if item}
			<div class="share-card">
				<div class="share-card-icon"><FileIcon type={item.type} name={item.name} size="3.5rem" /></div>
				<h1>{item.name}</h1>
				<p class="hint">{item.type === 'folder' ? 'Folder' : formatSize(item.size_bytes)}</p>

				{#if objectUrl}
					<img class="share-preview" src={objectUrl} alt={item.name} />
				{/if}

				{#if item.type === 'file'}
					<button class="btn btn-primary" onclick={handleDownload} disabled={downloading}>
						{downloading ? 'Downloading…' : 'Download'}
					</button>
				{:else}
					<p class="hint">
						Shared folders can't be downloaded as a whole yet — ask whoever shared this with you
						for the individual files instead.
					</p>
				{/if}
			</div>
		{/if}
	</div>
</div>
