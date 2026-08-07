<script lang="ts">
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { api, ApiError, type Share, type Item } from '$lib/api';
	import FileIcon from '$lib/FileIcon.svelte';
	import { t } from '$lib/i18n';

	interface EnrichedShare extends Share {
		item?: Item;
		// Distinct from "item is in the trash" (item still resolves, just
		// with deleted_at set — GetIncludingTrashed server-side, see
		// internal/handler/item.go) — this is the actually-gone case: the
		// share outlived its target being permanently deleted.
		itemMissing?: boolean;
	}

	let shares = $state<EnrichedShare[]>([]);
	let loading = $state(true);
	let error = $state('');

	let openMenuFor = $state<string | null>(null);

	function toggleMenu(id: string, event: MouseEvent) {
		event.stopPropagation();
		openMenuFor = openMenuFor === id ? null : id;
	}

	function closeMenu() {
		openMenuFor = null;
	}

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

	async function load() {
		loading = true;
		error = '';
		try {
			const list = await api.listShares();
			// The listing itself only carries item_id (see api.ts's Share
			// type) — resolve each target's full item to show something a
			// person recognizes (name, icon, size) and to know whether it's
			// still openable. A share can outlive its target being
			// permanently deleted, so a lookup failing here is an expected
			// case, not an error to surface: that share just shows as
			// pointing at a gone item.
			shares = await Promise.all(
				list.map(async (share) => {
					try {
						const item = await api.getItem(share.item_id);
						return { ...share, item };
					} catch {
						return { ...share, itemMissing: true };
					}
				})
			);
		} catch (err) {
			error = err instanceof ApiError ? err.message : $t('shares.errors.couldNotLoad');
		} finally {
			loading = false;
		}
	}

	onMount(load);

	function openItem(share: EnrichedShare) {
		if (!share.item || share.item.type !== 'file') return;
		goto(`/file/${share.item.id}?from=shares`);
	}

	async function handleRevoke(id: string, event: MouseEvent) {
		event.stopPropagation();
		closeMenu();
		if (!confirm($t('shares.confirmRevoke'))) return;
		try {
			await api.revokeShare(id);
			await load();
		} catch (err) {
			error = err instanceof ApiError ? err.message : $t('shares.errors.couldNotRevoke');
		}
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
	<title>{$t('nav.myShares')} · Denizen</title>
</svelte:head>

<h1>{$t('nav.myShares')}</h1>
<p class="hint">{$t('shares.hint')}</p>

{#if error}
	<p class="error-text">{error}</p>
{/if}

{#if loading}
	<p>{$t('common.loading')}</p>
{:else if shares.length === 0}
	<div class="empty-state">{$t('shares.empty')}</div>
{:else}
	<div class="item-list-header">
		<span class="item-icon"></span>
		<span class="item-name-header">{$t('common.name')}</span>
		<span class="item-modified">{$t('shares.status')}</span>
		<span class="item-size">{$t('shares.expires')}</span>
		<span class="row-menu"></span>
	</div>
	<div class="item-list">
		{#each shares as share (share.id)}
			<div class="item-row">
				<span class="item-icon">
					{#if share.item}
						<FileIcon type={share.item.type} name={share.item.name} mimeType={share.item.mime_type} />
					{/if}
				</span>
				<button
					class="item-name"
					disabled={!share.item || share.item.type !== 'file'}
					onclick={() => openItem(share)}
				>
					{#if share.itemMissing}
						{$t('shares.itemMissing')}
					{:else if share.item?.deleted_at}
						{$t('shares.itemInTrash', { name: share.item.name })}
					{:else}
						{share.item?.name}
					{/if}
				</button>
				<span class="item-modified">{share.requires_auth ? $t('shares.loginRequired') : $t('shares.public')}</span>
				<span class="item-size">{share.expires_at ? formatDate(share.expires_at) : $t('shares.never')}</span>
				<div class="row-menu">
					<button
						class="btn icon-btn"
						aria-label={$t('common.actionsFor', { name: share.item?.name ?? $t('shares.genericItemName') })}
						aria-haspopup="true"
						aria-expanded={openMenuFor === share.id}
						onclick={(e) => toggleMenu(share.id, e)}
					>
						<svg width="18" height="18" viewBox="0 0 24 24" aria-hidden="true">
							<circle cx="12" cy="5" r="1.6" fill="currentColor" />
							<circle cx="12" cy="12" r="1.6" fill="currentColor" />
							<circle cx="12" cy="19" r="1.6" fill="currentColor" />
						</svg>
					</button>
					{#if openMenuFor === share.id}
						<!-- svelte-ignore a11y_no_static_element_interactions -->
						<!-- svelte-ignore a11y_click_events_have_key_events -->
						<div class="menu-backdrop" onclick={closeMenu}></div>
						<!-- svelte-ignore a11y_no_static_element_interactions -->
						<!-- svelte-ignore a11y_interactive_supports_focus -->
						<!-- svelte-ignore a11y_click_events_have_key_events -->
						<div class="dropdown-menu" onclick={(e) => e.stopPropagation()} role="menu">
							{#if share.item && share.item.type === 'file'}
								<button role="menuitem" onclick={() => openItem(share)}>
									<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" aria-hidden="true">
										<path d="M2 12s3.5-7 10-7 10 7 10 7-3.5 7-10 7-10-7-10-7Z" stroke-linejoin="round" />
										<circle cx="12" cy="12" r="3" />
									</svg>
									{$t('common.open')}
								</button>
							{/if}
							<button role="menuitem" onclick={(e) => handleRevoke(share.id, e)}>
								<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" aria-hidden="true">
									<path
										d="M18 6 6 18M6 6l12 12"
										stroke-linecap="round"
										stroke-linejoin="round"
									/>
								</svg>
								{$t('shares.revoke')}
							</button>
							<button class="dropdown-menu-cancel" onclick={closeMenu}>{$t('common.cancel')}</button>
						</div>
					{/if}
				</div>
			</div>
		{/each}
	</div>
{/if}
