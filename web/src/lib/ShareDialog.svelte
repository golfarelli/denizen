<script lang="ts">
	import { api, ApiError, type DirectoryUser, type GrantedShare, type Item, type Share } from '$lib/api';
	import { t } from '$lib/i18n';

	// null = closed. Bound from the parent so opening/closing is just
	// setting a variable, no separate open/close event plumbing needed.
	let { item = $bindable(null) }: { item: Item | null } = $props();

	let dialogEl: HTMLDialogElement;
	let requiresAuth = $state(false);
	let expiresInDays = $state<number | null>(null);
	let result = $state<Share | null>(null);
	let error = $state('');
	let loading = $state(false);
	let copied = $state(false);

	// --- direct, per-person sharing (view-only, files only — see
	// GrantedShare's own comment) ------------------------------------------
	let directory = $state<DirectoryUser[]>([]);
	let grants = $state<GrantedShare[]>([]);
	let selectedUserId = $state('');
	let peopleError = $state('');
	let addingPerson = $state(false);
	// GrantedShare carries the recipient's username, not their id (see its
	// own comment) — matching against the directory by username instead of
	// id to filter out people already granted access. Usernames are unique
	// and this app has no account-deletion feature, so that's a stable
	// enough key for this.
	let availablePeople = $derived(directory.filter((u) => !grants.some((g) => g.shared_with_username === u.username)));

	async function loadPeopleSection(itemId: string) {
		peopleError = '';
		try {
			const [dir, list] = await Promise.all([api.listUserDirectory(), api.listUserSharesForItem(itemId)]);
			directory = dir;
			grants = list;
		} catch (err) {
			peopleError = err instanceof ApiError ? err.message : $t('dialogs.share.errors.couldNotLoadPeople');
		}
	}

	async function handleAddPerson() {
		if (!item || !selectedUserId) return;
		addingPerson = true;
		peopleError = '';
		try {
			const grant = await api.createUserShare(item.id, selectedUserId);
			grants = [...grants, grant];
			selectedUserId = '';
		} catch (err) {
			peopleError = err instanceof ApiError ? err.message : $t('dialogs.share.errors.couldNotSharePerson');
		} finally {
			addingPerson = false;
		}
	}

	async function handleRemovePerson(grantId: string) {
		peopleError = '';
		try {
			await api.revokeUserShare(grantId);
			grants = grants.filter((g) => g.id !== grantId);
		} catch (err) {
			peopleError = err instanceof ApiError ? err.message : $t('dialogs.share.errors.couldNotRemovePerson');
		}
	}

	$effect(() => {
		if (item) {
			requiresAuth = false;
			expiresInDays = null;
			result = null;
			error = '';
			copied = false;
			directory = [];
			grants = [];
			selectedUserId = '';
			peopleError = '';
			if (item.type === 'file') loadPeopleSection(item.id);
			dialogEl?.showModal();
		} else {
			dialogEl?.close();
		}
	});

	function close() {
		item = null;
	}

	async function handleCreate() {
		if (!item) return;
		loading = true;
		error = '';
		try {
			const expiresAt = expiresInDays ? Math.floor(Date.now() / 1000) + expiresInDays * 86400 : null;
			result = await api.createShare(item.id, requiresAuth, expiresAt);
		} catch (err) {
			error = err instanceof ApiError ? err.message : $t('dialogs.share.errors.couldNotCreate');
		} finally {
			loading = false;
		}
	}

	function absoluteUrl(path: string): string {
		return `${location.origin}${path}`;
	}

	async function copyLink() {
		if (!result?.url) return;
		try {
			await navigator.clipboard.writeText(absoluteUrl(result.url));
			copied = true;
		} catch {
			// The Clipboard API needs a secure context (https or localhost) —
			// quite possibly unavailable for a home server reached over
			// plain http on the LAN. The link stays visible and selectable
			// in the field below regardless, so it can still be copied by
			// hand either way.
		}
	}
</script>

<dialog bind:this={dialogEl} onclose={close} class="card">
	{#if item}
		<h2>{$t('dialogs.share.heading', { name: item.name })}</h2>

		{#if item.type === 'file'}
			<div class="share-people">
				<h3>{$t('dialogs.share.peopleHeading')}</h3>
				{#if peopleError}<p class="error-text">{peopleError}</p>{/if}
				{#if grants.length > 0}
					<ul class="share-people-list">
						{#each grants as grant (grant.id)}
							<li>
								<span>{grant.shared_with_username}</span>
								<button
									class="btn icon-btn"
									aria-label={$t('dialogs.share.removePerson', { name: grant.shared_with_username })}
									onclick={() => handleRemovePerson(grant.id)}
								>
									✕
								</button>
							</li>
						{/each}
					</ul>
				{/if}
				{#if availablePeople.length > 0}
					<div class="share-people-add">
						<select bind:value={selectedUserId} aria-label={$t('dialogs.share.choosePerson')}>
							<option value="">{$t('dialogs.share.choosePerson')}</option>
							{#each availablePeople as person (person.id)}
								<option value={person.id}>{person.username}</option>
							{/each}
						</select>
						<button class="btn" onclick={handleAddPerson} disabled={!selectedUserId || addingPerson}>
							{addingPerson ? $t('dialogs.share.sharing') : $t('dialogs.share.sharePerson')}
						</button>
					</div>
				{:else if directory.length === 0}
					<p class="hint">{$t('dialogs.share.noOtherUsers')}</p>
				{/if}
			</div>
			<hr class="share-divider" />
			<h3>{$t('dialogs.share.linkHeading')}</h3>
		{/if}

		{#if !result}
			<label class="field-inline">
				<input type="checkbox" bind:checked={requiresAuth} />
				{$t('dialogs.share.requireAuth')}
			</label>
			<div class="field">
				<label for="expires">{$t('dialogs.share.expiresLabel')}</label>
				<input id="expires" type="number" min="1" bind:value={expiresInDays} />
			</div>
			{#if error}<p class="error-text">{error}</p>{/if}
			<div class="dialog-actions">
				<button class="btn" onclick={close}>{$t('common.cancel')}</button>
				<button class="btn btn-primary" onclick={handleCreate} disabled={loading}>
					{loading ? $t('dialogs.share.creating') : $t('dialogs.share.createLink')}
				</button>
			</div>
		{:else}
			<p>{$t('dialogs.share.saveNowNotice')}</p>
			<div class="field">
				<!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
				<input
					readonly
					value={absoluteUrl(result.url ?? '')}
					onclick={(e) => (e.target as HTMLInputElement).select()}
				/>
			</div>
			<div class="dialog-actions">
				<button class="btn" onclick={copyLink}>{copied ? $t('common.copied') : $t('common.copyLink')}</button>
				<button class="btn btn-primary" onclick={close}>{$t('common.done')}</button>
			</div>
		{/if}
	{/if}
</dialog>
