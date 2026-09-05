<script lang="ts">
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { page } from '$app/stores';
	import { startUpload } from '$lib/upload';
	import { t } from '$lib/i18n';

	interface SharedFile {
		name: string;
		blob: Blob;
		status: 'pending' | 'uploading' | 'done' | 'error';
		error?: string;
	}

	// sw.js's fetch handler intercepts the OS share sheet's POST here and
	// stashes each shared file in the Cache Storage API (private per-origin
	// storage, unrelated to this app's own file storage) before redirecting
	// to this same page with ?share_id= — see its own doc comment for why
	// the POST itself can't just be authenticated and uploaded directly.
	// This page's job is exactly the second half: read those bytes back out
	// as real File objects and hand them to the same authenticated
	// startUpload() every other upload path in this app already uses.
	let files = $state<SharedFile[]>([]);
	let loading = $state(true);
	let loadError = $state('');

	onMount(async () => {
		const shareId = $page.url.searchParams.get('share_id');
		if (!shareId) {
			loadError = $t('shareTarget.errors.noFiles');
			loading = false;
			return;
		}
		if (!('caches' in window)) {
			loadError = $t('shareTarget.errors.unsupported');
			loading = false;
			return;
		}

		try {
			const cache = await caches.open('denizen-shared-files');
			const countRes = await cache.match(`/__shared/${shareId}/count`);
			const count = countRes ? Number(await countRes.text()) : 0;

			const loaded: SharedFile[] = [];
			for (let i = 0; i < count; i++) {
				const key = `/__shared/${shareId}/${i}`;
				const res = await cache.match(key);
				if (!res) continue;
				const name = decodeURIComponent(res.headers.get('X-Denizen-Filename') ?? `shared-file-${i}`);
				loaded.push({ name, blob: await res.blob(), status: 'pending' });
				await cache.delete(key); // one-shot: this share_id is only ever read once
			}
			await cache.delete(`/__shared/${shareId}/count`);

			files = loaded;
			if (loaded.length === 0) loadError = $t('shareTarget.errors.notFound');
		} catch {
			loadError = $t('shareTarget.errors.couldNotRead');
		} finally {
			loading = false;
		}
	});

	function handleUploadAll() {
		for (const entry of files) {
			if (entry.status !== 'pending') continue;
			entry.status = 'uploading';
			const file = new File([entry.blob], entry.name, { type: entry.blob.type });
			startUpload(file, null, {
				onProgress: () => {},
				onSuccess: () => {
					entry.status = 'done';
					if (files.every((f) => f.status === 'done')) {
						goto('/');
					}
				},
				onError: (message) => {
					entry.status = 'error';
					entry.error = message;
				}
			});
		}
	}
</script>

<svelte:head>
	<title>{$t('shareTarget.title')}</title>
</svelte:head>

<h1>{$t('shareTarget.title')}</h1>

{#if loading}
	<p>{$t('shareTarget.reading')}</p>
{:else if loadError}
	<p class="error-text">{loadError}</p>
	<a class="btn" href="/">{$t('shareTarget.back')}</a>
{:else}
	<p class="hint">{$t('shareTarget.uploadsToHome')}</p>
	<div class="item-list" style="margin-bottom: var(--space-4)">
		{#each files as entry (entry.name)}
			<div class="item-row">
				<span class="item-icon">📄</span>
				<span class="item-name" style="cursor: default">{entry.name}</span>
				{#if entry.status === 'uploading'}
					<span class="upload-percent">{$t('shareTarget.uploading')}</span>
				{:else if entry.status === 'done'}
					<span class="upload-status-done">{$t('common.done')}</span>
				{:else if entry.status === 'error'}
					<span class="error-text">{entry.error ?? $t('common.failed')}</span>
				{/if}
			</div>
		{/each}
	</div>
	<button class="btn btn-primary" onclick={handleUploadAll} disabled={files.every((f) => f.status !== 'pending')}>
		{$t('fileBrowser.uploadPlain')}
	</button>
{/if}
