<script lang="ts">
	import { onMount } from 'svelte';
	import { api, ApiError, type Me } from '$lib/api';
	import { me } from '$lib/me';

	const GIB = 1024 ** 3;

	let users = $state<Me[]>([]);
	let loading = $state(true);
	let error = $state('');

	// user.id -> the quota (in GB, as text while being edited) for whichever
	// row currently has its quota field open; absent = not being edited.
	let editingQuotaGB = $state<Record<string, string>>({});

	let inviteQuotaGB = $state<number | null>(null);
	let inviteResult = $state<{ code: string; expires_at: number } | null>(null);
	let inviteError = $state('');
	let inviteLoading = $state(false);
	let inviteCopied = $state(false);

	async function loadUsers() {
		loading = true;
		error = '';
		try {
			users = await api.listUsers();
		} catch (err) {
			error = err instanceof ApiError ? err.message : 'Could not load users.';
		} finally {
			loading = false;
		}
	}

	onMount(loadUsers);

	function inviteUrl(code: string): string {
		return `${location.origin}/register?code=${encodeURIComponent(code)}`;
	}

	async function handleCreateInvite() {
		inviteLoading = true;
		inviteError = '';
		inviteResult = null;
		inviteCopied = false;
		try {
			const quotaBytes = inviteQuotaGB ? Math.round(inviteQuotaGB * GIB) : null;
			inviteResult = await api.createInvite(quotaBytes);
		} catch (err) {
			inviteError = err instanceof ApiError ? err.message : 'Could not create the invite.';
		} finally {
			inviteLoading = false;
		}
	}

	async function copyInviteLink() {
		if (!inviteResult) return;
		try {
			await navigator.clipboard.writeText(inviteUrl(inviteResult.code));
			inviteCopied = true;
		} catch {
			// Same Clipboard API caveat as ShareDialog — the field stays
			// visible and selectable regardless, so it's still copyable by
			// hand on a plain-http home server.
		}
	}

	async function toggleDisabled(user: Me) {
		error = '';
		try {
			await api.updateUser(user.id, { disabled: !user.disabled });
			await loadUsers();
		} catch (err) {
			error = err instanceof ApiError ? err.message : 'Could not update this user.';
		}
	}

	function startEditQuota(user: Me) {
		editingQuotaGB[user.id] = (user.quota_bytes / GIB).toString();
	}

	async function saveQuota(user: Me) {
		const gb = parseFloat(editingQuotaGB[user.id]);
		if (Number.isNaN(gb) || gb < 0) return;
		error = '';
		try {
			await api.updateUser(user.id, { quota_bytes: Math.round(gb * GIB) });
			delete editingQuotaGB[user.id];
			await loadUsers();
		} catch (err) {
			error = err instanceof ApiError ? err.message : 'Could not update this user.';
		}
	}

	function formatGB(bytes: number): string {
		return `${(bytes / GIB).toFixed(1)} GB`;
	}
</script>

<svelte:head>
	<title>Admin · Denizen</title>
</svelte:head>

<h1>Admin</h1>

{#if $me && !$me.is_admin}
	<p class="error-text">You don't have access to this page.</p>
{:else}
	<section class="card" style="margin-bottom: var(--space-6)">
		<h2>Invite someone</h2>
		<div class="field">
			<label for="invite-quota">Storage quota, in GB (blank = server default)</label>
			<input id="invite-quota" type="number" min="0" step="0.5" bind:value={inviteQuotaGB} />
		</div>
		{#if inviteError}<p class="error-text">{inviteError}</p>{/if}
		<button class="btn btn-primary" onclick={handleCreateInvite} disabled={inviteLoading}>
			{inviteLoading ? 'Creating…' : 'Create invite'}
		</button>

		{#if inviteResult}
			<div class="field" style="margin-top: var(--space-4)">
				<label for="invite-url">Share this link (shown only once)</label>
				<!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
				<input
					id="invite-url"
					readonly
					value={inviteUrl(inviteResult.code)}
					onclick={(e) => (e.target as HTMLInputElement).select()}
				/>
			</div>
			<button class="btn" onclick={copyInviteLink}>{inviteCopied ? 'Copied!' : 'Copy link'}</button>
		{/if}
	</section>

	<h2>Users</h2>
	{#if error}<p class="error-text">{error}</p>{/if}

	{#if loading}
		<p>Loading…</p>
	{:else}
		<div class="item-list">
			{#each users as user (user.id)}
				<div class="item-row item-row-flex">
					<span class="item-name" style="cursor:default; flex:1">
						{user.username}{user.is_admin ? ' 👑' : ''}{user.disabled ? ' (disabled)' : ''}
					</span>

					{#if editingQuotaGB[user.id] !== undefined}
						<input
							type="number"
							min="0"
							step="0.5"
							style="width:6rem"
							bind:value={editingQuotaGB[user.id]}
						/>
						<button class="btn" onclick={() => saveQuota(user)}>Save</button>
					{:else}
						<span class="item-size">{formatGB(user.storage_used_bytes)} / {formatGB(user.quota_bytes)}</span>
						<button class="btn" onclick={() => startEditQuota(user)}>Edit quota</button>
					{/if}

					<button class="btn" onclick={() => toggleDisabled(user)}>
						{user.disabled ? 'Enable' : 'Disable'}
					</button>
				</div>
			{/each}
		</div>
	{/if}
{/if}
