<script lang="ts">
	import { api, ApiError, type Item, type Share } from '$lib/api';

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

	$effect(() => {
		if (item) {
			requiresAuth = false;
			expiresInDays = null;
			result = null;
			error = '';
			copied = false;
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
			error = err instanceof ApiError ? err.message : 'Could not create the link.';
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
		<h2>Share "{item.name}"</h2>

		{#if !result}
			<label class="field-inline">
				<input type="checkbox" bind:checked={requiresAuth} />
				Require the visitor to be logged in
			</label>
			<div class="field">
				<label for="expires">Expires after (days, blank = never)</label>
				<input id="expires" type="number" min="1" bind:value={expiresInDays} />
			</div>
			{#if error}<p class="error-text">{error}</p>{/if}
			<div class="dialog-actions">
				<button class="btn" onclick={close}>Cancel</button>
				<button class="btn btn-primary" onclick={handleCreate} disabled={loading}>
					{loading ? 'Creating…' : 'Create link'}
				</button>
			</div>
		{:else}
			<p>Save this link now — it won't be shown again.</p>
			<div class="field">
				<!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
				<input
					readonly
					value={absoluteUrl(result.url ?? '')}
					onclick={(e) => (e.target as HTMLInputElement).select()}
				/>
			</div>
			<div class="dialog-actions">
				<button class="btn" onclick={copyLink}>{copied ? 'Copied!' : 'Copy link'}</button>
				<button class="btn btn-primary" onclick={close}>Done</button>
			</div>
		{/if}
	{/if}
</dialog>
