<script lang="ts">
	import { photosToPdf } from '$lib/scan';
	import { t } from '$lib/i18n';
	import ScanCropper from '$lib/ScanCropper.svelte';
	import { loadScanCanvas } from '$lib/scanSource';
	import { detectDocumentQuad, disposeDocumentDetector } from '$lib/scanDetect';
	import { cropToJpeg } from '$lib/scanWarp';
	import { defaultQuad, isConvexQuad, polygonArea, type Quad } from '$lib/scanGeometry';

	// No "item" concept here (unlike ShareDialog/MoveDialog) — this is a
	// toolbar-level action, not a per-row one, so a plain open flag is all
	// the caller needs to control.
	let { open = $bindable(false), onScanned }: { open: boolean; onScanned: (file: File) => void } = $props();

	interface CapturedPage {
		id: string;
		blob: Blob;
		previewUrl: string;
	}

	// A photo just taken, waiting on the crop step before it becomes a page:
	// the four corners start at whatever automatic detection finds (or a
	// default inset, until it finishes or if it finds nothing) and the user
	// can drag them onto the real corners.
	interface PendingCrop {
		previewUrl: string;
		width: number;
		height: number;
		quad: Quad;
		detected: Quad | null;
		detecting: boolean;
		// The user moved a corner themselves — a detection result landing
		// afterwards must not yank it back.
		touched: boolean;
	}

	let dialogEl: HTMLDialogElement;
	let fileInput = $state<HTMLInputElement>();
	let pages = $state<CapturedPage[]>([]);
	let pending = $state<PendingCrop | null>(null);
	// Not reactive: a canvas is not something to deep-proxy, and nothing renders from it.
	let pendingCanvas: HTMLCanvasElement | null = null;
	let captureSeq = 0; // lets a slow detection for a discarded photo be ignored
	let building = $state(false);
	let error = $state('');
	let nextPageId = 0;

	let cropValid = $derived(
		!!pending && isConvexQuad(pending.quad) && polygonArea(pending.quad) > pending.width * pending.height * 0.02
	);

	$effect(() => {
		if (open) {
			dialogEl?.showModal();
		} else {
			dialogEl?.close();
		}
	});

	function discardPending() {
		captureSeq++;
		if (pending) URL.revokeObjectURL(pending.previewUrl);
		pending = null;
		pendingCanvas = null;
	}

	function close() {
		discardPending();
		// End of the scan session: free OpenCV's memory (the next scan reloads it).
		disposeDocumentDetector();
		for (const page of pages) URL.revokeObjectURL(page.previewUrl);
		pages = [];
		error = '';
		open = false;
	}

	function canvasToJpeg(canvas: HTMLCanvasElement, quality: number): Promise<Blob> {
		return new Promise((resolve, reject) =>
			canvas.toBlob((b) => (b ? resolve(b) : reject(new Error('could not encode the photo'))), 'image/jpeg', quality)
		);
	}

	// The `capture` attribute opens the camera app fresh for one photo per
	// invocation (not a multi-shot picker) — "Add page" re-triggers this
	// same hidden input for each page, one camera round-trip at a time,
	// same as Google Drive's own scan flow works a page at a time. The photo
	// then goes through the crop step before it counts as a page.
	async function handleCapture(event: Event) {
		const input = event.target as HTMLInputElement;
		const file = input.files?.[0];
		input.value = ''; // allow capturing into the same input again for the next page
		if (!file) return;
		error = '';
		discardPending();
		const seq = captureSeq;
		try {
			const canvas = await loadScanCanvas(file);
			// A canvas-derived preview, so what's shown is exactly the pixels
			// (orientation included) the corners are measured in.
			const previewUrl = URL.createObjectURL(await canvasToJpeg(canvas, 0.85));
			if (seq !== captureSeq) {
				URL.revokeObjectURL(previewUrl);
				return; // retaken or closed while decoding
			}
			pendingCanvas = canvas;
			pending = {
				previewUrl,
				width: canvas.width,
				height: canvas.height,
				quad: defaultQuad(canvas.width, canvas.height),
				detected: null,
				detecting: true,
				touched: false
			};
			// Not awaited: the crop step is usable straight away, the
			// detected corners land when they're ready.
			void detectDocumentQuad(canvas).then((found) => {
				if (seq !== captureSeq || !pending) return;
				pending.detecting = false;
				pending.detected = found;
				if (found && !pending.touched) pending.quad = found;
			});
		} catch {
			error = $t('dialogs.scan.errors.couldNotLoadPhoto');
		}
	}

	function resetCorners() {
		if (!pending) return;
		pending.quad = pending.detected ?? defaultQuad(pending.width, pending.height);
		pending.touched = false;
	}

	function retake() {
		discardPending();
		fileInput?.click();
	}

	async function confirmCrop() {
		if (!pending || !pendingCanvas || !cropValid) return;
		building = true;
		error = '';
		try {
			const blob = await cropToJpeg(pendingCanvas, pending.quad);
			nextPageId += 1;
			pages.push({ id: `page-${nextPageId}`, blob, previewUrl: URL.createObjectURL(blob) });
			discardPending();
		} catch {
			error = $t('dialogs.scan.errors.couldNotCrop');
		} finally {
			building = false;
		}
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

<dialog bind:this={dialogEl} onclose={close} class="card" class:scan-crop-open={!!pending}>
	{#if open}
		<h2>{$t('dialogs.scan.heading')}</h2>

		{#if error}<p class="error-text">{error}</p>{/if}

		{#if pending}
			<p class="hint" data-detecting={pending.detecting} data-detected={pending.detected !== null}>
				{pending.detecting
					? $t('dialogs.scan.detecting')
					: pending.detected
						? $t('dialogs.scan.adjustHint')
						: $t('dialogs.scan.adjustHintManual')}
			</p>
			<ScanCropper
				src={pending.previewUrl}
				width={pending.width}
				height={pending.height}
				bind:quad={pending.quad}
				onadjust={() => {
					if (pending) pending.touched = true;
				}}
			/>
		{:else if pages.length === 0}
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

		{#if pending}
			<div class="dialog-actions" style="justify-content: space-between">
				<button class="btn" onclick={retake}>{$t('dialogs.scan.retake')}</button>
				<div style="display:flex; gap: var(--space-2)">
					<button class="btn" onclick={resetCorners}>{$t('dialogs.scan.resetCorners')}</button>
					<button class="btn btn-primary" onclick={confirmCrop} disabled={!cropValid || building}>
						{$t('dialogs.scan.usePage')}
					</button>
				</div>
			</div>
		{:else}
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
	{/if}
</dialog>
