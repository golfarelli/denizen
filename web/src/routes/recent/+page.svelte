<script lang="ts">
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { api, ApiError, type Item } from '$lib/api';
	import FileIcon from '$lib/FileIcon.svelte';
	import SkeletonList from '$lib/SkeletonList.svelte';
	import { t } from '$lib/i18n';

	// A flat, read-only jump list — every row here is one of the caller's
	// own files (see ItemService.ListRecent: folders and anything reached
	// via a share are both out of scope), so unlike routes/+page.svelte
	// there's no folder navigation, selection, or row menu to build here.
	// Clicking a row just opens that file, same as double-clicking it in
	// the main file browser.

	let items = $state<Item[]>([]);
	let loading = $state(true);
	let error = $state('');

	async function load() {
		loading = true;
		error = '';
		try {
			items = await api.listRecent();
		} catch (err) {
			error = err instanceof ApiError ? err.message : $t('recent.errors.couldNotLoad');
		} finally {
			loading = false;
		}
	}

	onMount(load);

	function openItem(item: Item) {
		goto(`/file/${item.id}?from=recent`);
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

	function formatDate(unixSeconds: number): string {
		return new Date(unixSeconds * 1000).toLocaleDateString(undefined, {
			year: 'numeric',
			month: 'short',
			day: 'numeric'
		});
	}
</script>

<svelte:head>
	<title>{$t('nav.recent')} · Denizen</title>
</svelte:head>

<div class="toolbar">
	<h1 style="margin:0">{$t('nav.recent')}</h1>
</div>
<p class="hint">{$t('recent.hint')}</p>

{#if error}
	<p class="error-text">{error}</p>
{/if}

{#if loading}
	<SkeletonList />
{:else if items.length === 0}
	<div class="empty-state">{$t('recent.empty')}</div>
{:else}
	<div class="item-list-header">
		<span class="item-icon"></span>
		<span class="item-name-header">{$t('common.name')}</span>
		<span class="item-modified">{$t('common.modified')}</span>
		<span class="item-size">{$t('common.size')}</span>
		<span class="row-menu"></span>
	</div>
	<div class="item-list">
		{#each items as item (item.id)}
			<div class="item-row">
				<span class="item-icon">
					<FileIcon type={item.type} name={item.name} mimeType={item.mime_type} />
				</span>
				<button class="item-name" onclick={() => openItem(item)}>
					{item.name}
				</button>
				<span class="item-modified">{formatDate(item.updated_at)}</span>
				<span class="item-size">{formatSize(item.size_bytes)}</span>
				<span class="row-menu"></span>
			</div>
		{/each}
	</div>
{/if}
