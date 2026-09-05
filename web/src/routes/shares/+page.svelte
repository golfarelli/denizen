<script lang="ts">
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { api, ApiError, type Share, type GrantedShare, type Item, type SharePermission } from '$lib/api';
	import FileIcon from '$lib/FileIcon.svelte';
	import SkeletonList from '$lib/SkeletonList.svelte';
	import { t } from '$lib/i18n';
	import { confirmDialog } from '$lib/dialog';
	import { loadPersisted, savePersisted } from '$lib/persistedState';
	import SortArrow from '$lib/SortArrow.svelte';
	import SortMenu from '$lib/SortMenu.svelte';

	// Two very different kinds of "share" — a token-based link anyone
	// holding it can use (Share), and a direct grant to one specific
	// person (GrantedShare) — merged into one list/sort/revoke flow since
	// from the owner's own point of view ("everything I've shared") they
	// belong on the same page. kind is what the template and handleRevoke
	// below branch on; everything else about a row is read off whichever
	// of the two link/person-only field groups actually applies.
	interface EnrichedShare {
		id: string;
		item_id: string;
		item?: Item;
		// Distinct from "item is in the trash" (item still resolves, just
		// with deleted_at set — GetIncludingTrashed server-side, see
		// internal/handler/item.go) — this is the actually-gone case: the
		// share outlived its target being permanently deleted.
		itemMissing?: boolean;
		created_at: number;
		kind: 'link' | 'person';
		requires_auth?: boolean; // link only
		expires_at?: number; // link only
		shared_with_username?: string; // person only
		permission?: SharePermission; // person only
	}

	function fromLink(share: Share): EnrichedShare {
		return {
			id: share.id,
			item_id: share.item_id,
			created_at: share.created_at,
			kind: 'link',
			requires_auth: share.requires_auth,
			expires_at: share.expires_at
		};
	}

	function fromGrant(grant: GrantedShare): EnrichedShare {
		return {
			id: grant.id,
			item_id: grant.item_id,
			created_at: grant.created_at,
			kind: 'person',
			shared_with_username: grant.shared_with_username,
			permission: grant.permission
		};
	}

	let shares = $state<EnrichedShare[]>([]);
	let loading = $state(true);
	let error = $state('');

	// Not the shared lib/sortItems.ts (its Name/Modified/Size shape doesn't
	// match this page's own Name/Status/Expires columns — EnrichedShare
	// isn't an Item, it wraps one) — a small local comparator instead.
	type SortField = 'name' | 'status' | 'expires';
	type SortDirection = 'asc' | 'desc';
	const storedSort = loadPersisted<{ field: SortField; direction: SortDirection }>('denizen.sort.shares', {
		field: 'name',
		direction: 'asc'
	});
	let sortField = $state<SortField>(storedSort.field);
	let sortDirection = $state<SortDirection>(storedSort.direction);

	$effect(() => savePersisted('denizen.sort.shares', { field: sortField, direction: sortDirection }));

	function toggleSort(field: SortField) {
		if (sortField === field) {
			sortDirection = sortDirection === 'asc' ? 'desc' : 'asc';
		} else {
			sortField = field;
			sortDirection = 'asc';
		}
	}

	// A single sortable string for the Status column across both kinds —
	// groups public links, then login-required links, then person shares
	// (alphabetical by recipient), which reads sensibly without needing a
	// more elaborate multi-key comparator for what's a personal-scale list.
	function statusSortKey(share: EnrichedShare): string {
		if (share.kind === 'person') return `2-${share.shared_with_username}`;
		return share.requires_auth ? '1' : '0';
	}

	let sortedShares = $derived.by(() => {
		const sign = sortDirection === 'asc' ? 1 : -1;
		return [...shares].sort((a, b) => {
			if (sortField === 'name') {
				return sign * (a.item?.name ?? '').localeCompare(b.item?.name ?? '', undefined, { numeric: true, sensitivity: 'base' });
			}
			if (sortField === 'status') {
				return sign * statusSortKey(a).localeCompare(statusSortKey(b));
			}
			// "Never" (expires_at === undefined — always true for a person
			// share, which never expires) always sorts last, regardless of
			// direction — not just "largest" (which would flip to first on
			// a descending sort, reading oddly next to real dates that keep
			// behaving as expected).
			if (a.expires_at == null && b.expires_at == null) return 0;
			if (a.expires_at == null) return 1;
			if (b.expires_at == null) return -1;
			return sign * (a.expires_at - b.expires_at);
		});
	});

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
			const [links, grants] = await Promise.all([api.listShares(), api.listMyUserShares()]);
			const merged = [...links.map(fromLink), ...grants.map(fromGrant)];
			// Neither listing carries more than item_id (see api.ts's Share/
			// GrantedShare types) — resolve each target's full item to show
			// something a person recognizes (name, icon) and to know
			// whether it's still openable. A share can outlive its target
			// being permanently deleted, so a lookup failing here is an
			// expected case, not an error to surface: that row just shows
			// as pointing at a gone item. Several rows can point at the
			// same item_id (a link and a person share, or two people), so
			// this resolves each item once and reuses it, not once per row.
			const itemCache = new Map<string, Promise<Item>>();
			function resolveItem(itemId: string): Promise<Item> {
				let promise = itemCache.get(itemId);
				if (!promise) {
					promise = api.getItem(itemId);
					itemCache.set(itemId, promise);
				}
				return promise;
			}
			shares = await Promise.all(
				merged.map(async (share) => {
					try {
						const item = await resolveItem(share.item_id);
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
		if (!share.item) return;
		// Every row here is one of the caller's own items (they're the one
		// who shared it) — unlike shared-with-me/+page.svelte, there's no
		// access question, just "file preview or browse into the folder".
		if (share.item.type === 'folder') {
			goto(`/?folder=${encodeURIComponent(share.item.id)}`);
			return;
		}
		goto(`/file/${share.item.id}?from=shares`);
	}

	async function handleRevoke(share: EnrichedShare, event: MouseEvent) {
		event.stopPropagation();
		closeMenu();
		const message = share.kind === 'person'
			? $t('shares.confirmRevokePerson', { name: share.shared_with_username ?? '' })
			: $t('shares.confirmRevoke');
		if (!(await confirmDialog(message, { confirmLabel: $t('shares.revoke'), danger: true }))) return;
		try {
			if (share.kind === 'person') {
				await api.revokeUserShare(share.id);
			} else {
				await api.revokeShare(share.id);
			}
			await load();
		} catch (err) {
			error = err instanceof ApiError ? err.message : $t('shares.errors.couldNotRevoke');
		}
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

<div class="toolbar">
	<h1 style="margin:0">{$t('nav.myShares')}</h1>
	<SortMenu
		fields={[
			{ key: 'name', label: $t('common.name') },
			{ key: 'status', label: $t('shares.sharedWithHeader') },
			{ key: 'expires', label: $t('shares.expires') }
		]}
		bind:sortField
		bind:sortDirection
	/>
</div>
<p class="hint">{$t('shares.hint')}</p>

{#if error}
	<p class="error-text">{error}</p>
{/if}

{#if loading}
	<SkeletonList />
{:else if shares.length === 0}
	<div class="empty-state">{$t('shares.empty')}</div>
{:else}
	<div class="item-list-header">
		<span class="item-icon"></span>
		<button class="sort-header item-name-header" onclick={() => toggleSort('name')}>
			{$t('common.name')}
			{#if sortField === 'name'}<SortArrow direction={sortDirection} />{/if}
		</button>
		<button class="sort-header item-modified" onclick={() => toggleSort('status')}>
			{$t('shares.sharedWithHeader')}
			{#if sortField === 'status'}<SortArrow direction={sortDirection} />{/if}
		</button>
		<button class="sort-header item-size" onclick={() => toggleSort('expires')}>
			{$t('shares.expires')}
			{#if sortField === 'expires'}<SortArrow direction={sortDirection} />{/if}
		</button>
		<span class="row-menu"></span>
	</div>
	<div class="item-list">
		{#each sortedShares as share (share.id)}
			<div class="item-row">
				<span class="item-icon">
					{#if share.item}
						<FileIcon type={share.item.type} name={share.item.name} mimeType={share.item.mime_type} />
					{/if}
				</span>
				<button class="item-name" disabled={!share.item} onclick={() => openItem(share)}>
					{#if share.itemMissing}
						{$t('shares.itemMissing')}
					{:else if share.item?.deleted_at}
						{$t('shares.itemInTrash', { name: share.item.name })}
					{:else}
						{share.item?.name}
					{/if}
				</button>
				<span class="item-modified">
					{#if share.kind === 'person'}
						{share.shared_with_username}
						<span class="permission-tag">
							{share.permission === 'edit' ? $t('dialogs.share.permissionEdit') : $t('dialogs.share.permissionView')}
						</span>
					{:else}
						{share.requires_auth ? $t('shares.loginRequired') : $t('shares.public')}
					{/if}
				</span>
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
							{#if share.item}
								<button role="menuitem" onclick={() => openItem(share)}>
									<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" aria-hidden="true">
										<path d="M2 12s3.5-7 10-7 10 7 10 7-3.5 7-10 7-10-7-10-7Z" stroke-linejoin="round" />
										<circle cx="12" cy="12" r="3" />
									</svg>
									{$t('common.open')}
								</button>
							{/if}
							<button role="menuitem" onclick={(e) => handleRevoke(share, e)}>
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

<style>
	.permission-tag {
		margin-left: var(--space-2);
		padding: 0.05em 0.5em;
		border-radius: 1em;
		background: var(--color-border);
		color: var(--color-text-muted);
		font-size: 0.85em;
		vertical-align: middle;
	}
</style>
