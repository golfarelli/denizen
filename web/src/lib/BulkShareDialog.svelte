<script lang="ts">
	import { api, ApiError, type DirectoryUser, type Item, type SharePermission } from '$lib/api';
	import { t } from '$lib/i18n';

	// null/empty = closed. Deliberately simpler than ShareDialog: no
	// "who already has access" listing (would mean N different lists, one
	// per item, awkward to show meaningfully at once) and no link
	// creation (a link is inherently a single-item thing) — just "share
	// all of these with one person", the one bulk action Drive itself
	// actually offers on a multi-selection too.
	let { items = $bindable(null) }: { items: Item[] | null } = $props();

	let dialogEl: HTMLDialogElement;
	let directory = $state<DirectoryUser[]>([]);
	let selectedUserId = $state('');
	let permission = $state<SharePermission>('view');
	let loading = $state(false);
	let error = $state('');
	let done = $state<{ succeeded: number; failed: number } | null>(null);

	$effect(() => {
		if (items && items.length > 0) {
			selectedUserId = '';
			permission = 'view';
			error = '';
			done = null;
			loadDirectory();
			dialogEl?.showModal();
		} else {
			dialogEl?.close();
		}
	});

	async function loadDirectory() {
		try {
			directory = await api.listUserDirectory();
		} catch (err) {
			error = err instanceof ApiError ? err.message : $t('dialogs.share.errors.couldNotLoadPeople');
		}
	}

	function close() {
		items = null;
	}

	async function handleShare() {
		if (!items || !selectedUserId) return;
		loading = true;
		error = '';
		// One item already shared with this person (a 409 conflict) or
		// otherwise failing shouldn't stop the rest of the selection —
		// same "don't let one bad apple block the others" spirit as the
		// bulk move/delete handlers in routes/+page.svelte.
		const results = await Promise.allSettled(
			items.map((item) => api.createUserShare(item.id, selectedUserId, permission))
		);
		const failed = results.filter((r) => r.status === 'rejected').length;
		loading = false;
		done = { succeeded: items.length - failed, failed };
	}
</script>

<dialog bind:this={dialogEl} onclose={close} class="card">
	{#if items && items.length > 0}
		<h2>{$t('dialogs.bulkShare.heading', { count: items.length })}</h2>

		{#if error}<p class="error-text">{error}</p>{/if}

		{#if done}
			<p>
				{done.failed === 0
					? $t('dialogs.bulkShare.allSucceeded', { count: done.succeeded })
					: $t('dialogs.bulkShare.someFailed', { succeeded: done.succeeded, failed: done.failed })}
			</p>
			<div class="dialog-actions">
				<button class="btn btn-primary" onclick={close}>{$t('common.done')}</button>
			</div>
		{:else if directory.length === 0}
			<p class="hint">{$t('dialogs.share.noOtherUsers')}</p>
			<div class="dialog-actions">
				<button class="btn" onclick={close}>{$t('common.cancel')}</button>
			</div>
		{:else}
			<div class="field">
				<label for="bulk-share-person">{$t('dialogs.share.choosePerson')}</label>
				<select id="bulk-share-person" bind:value={selectedUserId}>
					<option value="">{$t('dialogs.share.choosePerson')}</option>
					{#each directory as person (person.id)}
						<option value={person.id}>{person.username}</option>
					{/each}
				</select>
			</div>
			<div class="field">
				<label for="bulk-share-permission">{$t('dialogs.share.permissionLabel', { name: '' })}</label>
				<select id="bulk-share-permission" bind:value={permission}>
					<option value="view">{$t('dialogs.share.permissionView')}</option>
					<option value="edit">{$t('dialogs.share.permissionEdit')}</option>
				</select>
			</div>
			<div class="dialog-actions">
				<button class="btn" onclick={close}>{$t('common.cancel')}</button>
				<button class="btn btn-primary" onclick={handleShare} disabled={!selectedUserId || loading}>
					{loading ? $t('dialogs.share.sharing') : $t('dialogs.share.sharePerson')}
				</button>
			</div>
		{/if}
	{/if}
</dialog>
