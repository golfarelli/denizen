<script lang="ts">
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { api, ApiError, type ReceivedShare, type Item } from '$lib/api';
	import FileIcon from '$lib/FileIcon.svelte';
	import { t } from '$lib/i18n';

	interface EnrichedReceivedShare extends ReceivedShare {
		item?: Item;
		// The owner trashed or permanently deleted it, or revoked the grant
		// out from under an already-loaded page — GetIncludingTrashed
		// (internal/service/item.go) 404s a grant recipient for a trashed
		// item on purpose (no trash-preview window for someone who isn't
		// the owner), so from here that looks identical to "gone for good"
		// either way — there's nothing more specific to tell the recipient.
		itemMissing?: boolean;
	}

	let shares = $state<EnrichedReceivedShare[]>([]);
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
			const list = await api.listSharedWithMe();
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
			error = err instanceof ApiError ? err.message : $t('sharedWithMe.errors.couldNotLoad');
		} finally {
			loading = false;
		}
	}

	onMount(load);

	function openItem(share: EnrichedReceivedShare) {
		if (!share.item || share.item.type !== 'file') return;
		goto(`/file/${share.item.id}?from=shared-with-me`);
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
	<title>{$t('nav.sharedWithMe')} · Denizen</title>
</svelte:head>

<h1>{$t('nav.sharedWithMe')}</h1>
<p class="hint">{$t('sharedWithMe.hint')}</p>

{#if error}
	<p class="error-text">{error}</p>
{/if}

{#if loading}
	<p>{$t('common.loading')}</p>
{:else if shares.length === 0}
	<div class="empty-state">{$t('sharedWithMe.empty')}</div>
{:else}
	<div class="item-list-header">
		<span class="item-icon"></span>
		<span class="item-name-header">{$t('common.name')}</span>
		<span class="item-modified">{$t('sharedWithMe.sharedBy')}</span>
		<span class="item-size">{$t('sharedWithMe.sharedOn')}</span>
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
				<button class="item-name" disabled={!share.item} onclick={() => openItem(share)}>
					{#if share.itemMissing}
						{$t('sharedWithMe.itemMissing')}
					{:else}
						{share.item?.name}
					{/if}
				</button>
				<span class="item-modified">{share.owner_username}</span>
				<span class="item-size">{formatDate(share.created_at)}</span>
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
							{#if share.item}
								<button role="menuitem" onclick={() => openItem(share)}>
									<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" aria-hidden="true">
										<path d="M2 12s3.5-7 10-7 10 7 10 7-3.5 7-10 7-10-7-10-7Z" stroke-linejoin="round" />
										<circle cx="12" cy="12" r="3" />
									</svg>
									{$t('common.open')}
								</button>
							{/if}
							<button class="dropdown-menu-cancel" onclick={closeMenu}>{$t('common.cancel')}</button>
						</div>
					{/if}
				</div>
			</div>
		{/each}
	</div>
{/if}
