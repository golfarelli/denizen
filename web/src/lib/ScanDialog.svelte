<script lang="ts">
	import { photosToPdf } from '$lib/scan';
	import { t } from '$lib/i18n';

	// No "item" concept here (unlike ShareDialog/MoveDialog) — this is a
	// toolbar-level action, not a per-row one, so a plain open flag is all
	// the caller needs to control.
	let { open = $bindable(false), onScanned }: { open: boolean; onScanned: (file: File) => void } = $props();

	interface CapturedPage {
		id: string;
		blob: Blob;
		previewUrl: string;
	}

	let dialogEl: HTMLDialogElement;
	let fileInput = $state<HTMLInputElement>();
	let pages = $state<CapturedPage[]>([]);
	let building = $state(false);
	let error = $state('');
	let nextPageId = 0;

	$effect(() => {
		if (open) {
			dialogEl?.showModal();
		} else {
			dialogEl?.close();
		}
	});

	function close() {
		for (const page of pages) URL.revokeObjectURL(page.previewUrl);
		pages = [];
		error = '';
		open = false;
	}

	// The `capture` attribute opens the camera app fresh for one photo per
	// invocation (not a multi-shot picker) — "Add page" re-triggers this
	// same hidden input for each page, one camera round-trip at a time,
	// same as Google Drive's own scan flow works a page at a time.
	function handleCapture(event: Event) {
		const input = event.target as HTMLInputElement;
		const file = input.files?.[0];
		input.value = ''; // allow capturing into the same input again for the next page
		if (!file) return;
		nextPageId += 1;
		pages.push({ id: `page-${nextPageId}`, blob: file, previewUrl: URL.createObjectURL(file) });
	}

	function removePage(id: string) {
		const page = pages.find((p) => p.id === id);
		if (page) URL.revokeObjectURL(page.previewUrl);
		pages = pages.filter((p) => p.id !== id);
	}

	async function handleFinish() {
		if (pages.length === 0) return;
		building = true;
		error = '';
		try {
			const pdfBlob = await photosToPdf(pages.map((p) => p.blob));
			const stamp = new Date().toISOString().replace(/[:.]/g, '-').slice(0, 19);
			const file = new File([pdfBlob], `Scan ${stamp}.pdf`, { type: 'application/pdf' });
			onScanned(file);
			close();
		} catch (err) {
			error = err instanceof Error ? err.message : $t('dialogs.scan.errors.couldNotBuild');
		} finally {
			building = false;
		}
	}
</script>

<dialog bind:this={dialogEl} onclose={close} class="card">
	{#if open}
		<h2>{$t('dialogs.scan.heading')}</h2>

		{#if error}<p class="error-text">{error}</p>{/if}

		{#if pages.length === 0}
			<p class="hint">{$t('dialogs.scan.hint')}</p>
		{:else}
			<div class="scan-pages">
				{#each pages as page (page.id)}
					<div class="scan-page-thumb">
						<img src={page.previewUrl} alt="" />
						<button
							class="btn icon-btn scan-page-remove"
							aria-label={$t('dialogs.scan.removePage')}
							onclick={() => removePage(page.id)}
						>
							✕
						</button>
					</div>
				{/each}
			</div>
		{/if}

		<input
			bind:this={fileInput}
			type="file"
			accept="image/*"
			capture="environment"
			hidden
			onchange={handleCapture}
		/>

		<div class="dialog-actions" style="justify-content: space-between">
			<button class="btn" onclick={() => fileInput?.click()}>{$t('dialogs.scan.addPage')}</button>
			<div style="display:flex; gap: var(--space-2)">
				<button class="btn" onclick={close}>{$t('common.cancel')}</button>
				<button class="btn btn-primary" onclick={handleFinish} disabled={pages.length === 0 || building}>
					{building ? $t('dialogs.scan.building') : $t('dialogs.scan.saveAsPdf', { count: pages.length })}
				</button>
			</div>
		</div>
	{/if}
</dialog>
