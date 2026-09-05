<script lang="ts">
	// Renders whatever lib/dialog.ts's dialogRequest store currently holds —
	// one instance, mounted once in routes/+layout.svelte, standing in for
	// every window.prompt()/confirm() call anywhere in the app. Native
	// <dialog> (see MoveDialog.svelte/ShareDialog.svelte's own use of the
	// same element+.card styling) gives Escape-to-cancel and backdrop focus
	// trapping for free — this doesn't reimplement either.
	import { dialogRequest, type DialogRequest } from './dialog';
	import { t } from './i18n';

	let dialogEl: HTMLDialogElement;
	let inputEl: HTMLInputElement = $state()!;
	let inputValue = $state('');

	$effect(() => {
		const req = $dialogRequest;
		if (req) {
			if (req.kind === 'prompt') inputValue = req.defaultValue;
			dialogEl?.showModal();
			// select(), not just focus(): a rename prompt pre-fills the
			// current name (window.prompt's own default-value behavior),
			// selected so typing straight away replaces it — same as a
			// real prompt().
			if (req.kind === 'prompt') queueMicrotask(() => inputEl?.select());
		} else {
			dialogEl?.close();
		}
	});

	// The only path that resolves a *pending* request — every trigger below
	// (a button, submitting the form, Escape/backdrop via onclose) funnels
	// through this, and the guard on `req` makes it safe to call more than
	// once for the same request (button click closes the dialog, which
	// fires the native onclose too — see the template below).
	function settle(req: DialogRequest, value: string | boolean | null) {
		dialogRequest.set(null);
		if (req.kind === 'prompt') req.resolve(value as string | null);
		else req.resolve(value as boolean);
	}

	function handleClose() {
		const req = $dialogRequest;
		if (!req) return; // already settled by a button below
		settle(req, req.kind === 'prompt' ? null : false);
	}

	function submitPrompt(e: SubmitEvent) {
		e.preventDefault();
		const req = $dialogRequest;
		if (req?.kind !== 'prompt') return;
		const trimmed = inputValue.trim();
		settle(req, trimmed || null); // blank counts as cancelled, same as a real prompt()
	}
</script>

<dialog bind:this={dialogEl} onclose={handleClose} class="card">
	{#if $dialogRequest?.kind === 'prompt'}
		{@const req = $dialogRequest}
		<form onsubmit={submitPrompt}>
			<p>{req.message}</p>
			<input bind:this={inputEl} type="text" bind:value={inputValue} />
			<div class="dialog-actions">
				<button type="button" class="btn" onclick={() => settle(req, null)}>{$t('common.cancel')}</button>
				<button type="submit" class="btn btn-primary">{req.confirmLabel}</button>
			</div>
		</form>
	{:else if $dialogRequest?.kind === 'confirm'}
		{@const req = $dialogRequest}
		<p>{req.message}</p>
		<div class="dialog-actions">
			<button class="btn" onclick={() => settle(req, false)}>{$t('common.cancel')}</button>
			<button class="btn" class:btn-primary={!req.danger} class:btn-danger={req.danger} onclick={() => settle(req, true)}>
				{req.confirmLabel}
			</button>
		</div>
	{/if}
</dialog>
