<script lang="ts">
	import { onMount } from 'svelte';
	import { api, ApiError, type Share } from '$lib/api';

	interface EnrichedShare extends Share {
		itemName?: string;
		itemMissing?: boolean;
	}

	let shares = $state<EnrichedShare[]>([]);
	let loading = $state(true);
	let error = $state('');

	async function load() {
		loading = true;
		error = '';
		try {
			const list = await api.listShares();
			// The listing itself only carries item_id (see api.ts's Share type)
			// — resolve each target's name to show something a person
			// recognizes. A share can outlive its target being trashed, so a
			// lookup failing here is an expected case, not an error to
			// surface: that share just shows as pointing at a gone item.
			shares = await Promise.all(
				list.map(async (share) => {
					try {
						const item = await api.getItem(share.item_id);
						return { ...share, itemName: item.name };
					} catch {
						return { ...share, itemMissing: true };
					}
				})
			);
		} catch (err) {
			error = err instanceof ApiError ? err.message : 'Could not load your shares.';
		} finally {
			loading = false;
		}
	}

	onMount(load);

	async function handleRevoke(id: string) {
		if (!confirm('Revoke this share link? Anyone still holding it will lose access.')) return;
		try {
			await api.revokeShare(id);
			await load();
		} catch (err) {
			error = err instanceof ApiError ? err.message : 'Could not revoke this share.';
		}
	}

	function formatDate(unixSeconds: number): string {
		return new Date(unixSeconds * 1000).toLocaleString();
	}
</script>

<svelte:head>
	<title>My shares · Denizen</title>
</svelte:head>

<h1>My shares</h1>
<p class="hint">
	Links aren't shown again after creation — revoke and create a new one from the file browser if
	you've lost it.
</p>

{#if error}
	<p class="error-text">{error}</p>
{/if}

{#if loading}
	<p>Loading…</p>
{:else if shares.length === 0}
	<div class="empty-state">You haven't shared anything yet.</div>
{:else}
	<div class="item-list">
		{#each shares as share (share.id)}
			<div class="item-row item-row-flex">
				<span class="item-name" style="cursor: default">
					{share.itemMissing ? '(item no longer available)' : share.itemName}
				</span>
				<span class="item-size">{share.requires_auth ? 'Login required' : 'Public'}</span>
				<span class="item-size">
					{share.expires_at ? `Expires ${formatDate(share.expires_at)}` : 'Never expires'}
				</span>
				<button class="btn" onclick={() => handleRevoke(share.id)}>Revoke</button>
			</div>
		{/each}
	</div>
{/if}
