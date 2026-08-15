<script lang="ts">
	// Grid-tile preview: a real thumbnail for images/PDFs (GET .../thumbnail,
	// see internal/thumbnail and ItemHandler.Thumbnail), falling back to the
	// plain FileIcon glyph for every other kind, or if the fetch fails for
	// any reason (unsupported, not generatable, network hiccup — thumbnails
	// are a nice-to-have, never worth blocking or erroring the grid over).
	// List view intentionally keeps using bare FileIcon everywhere — a real
	// thumbnail only reads as an improvement at grid-tile size, matching
	// how Drive itself only shows them in its own grid view.
	import { onMount, onDestroy } from 'svelte';
	import { api } from '$lib/api';
	import { fileKind } from '$lib/fileKind';
	import FileIcon from '$lib/FileIcon.svelte';

	let {
		id,
		type,
		name,
		mimeType
	}: { id: string; type: 'file' | 'folder'; name: string; mimeType?: string } = $props();

	let kind = $derived(fileKind(type, name, mimeType));
	let thumbnailable = $derived(kind === 'image' || kind === 'pdf');

	// Empty means "show the FileIcon fallback" whether that's because this
	// kind was never thumbnailable, the fetch is still in flight, or it
	// failed — one state covers all three, nothing downstream needs to
	// tell them apart (see the markup below).
	let objectUrl = $state('');

	onMount(() => {
		if (!thumbnailable) return;
		api
			.getThumbnail(id)
			.then((blob) => {
				objectUrl = URL.createObjectURL(blob);
			})
			.catch(() => {
				// Thumbnails are a nice-to-have — the FileIcon fallback below
				// already covers this, nothing else to do.
			});
	});

	onDestroy(() => {
		if (objectUrl) URL.revokeObjectURL(objectUrl);
	});
</script>

{#if thumbnailable && objectUrl}
	<img src={objectUrl} alt="" class="item-tile-thumb" />
{:else}
	<FileIcon {type} {name} {mimeType} size="2.75rem" />
{/if}
