<script lang="ts">
	import { onMount, onDestroy } from 'svelte';
	import * as pdfjsLib from 'pdfjs-dist';
	import type { PDFDocumentLoadingTask, PDFDocumentProxy } from 'pdfjs-dist';
	// Vite's ?url suffix bundles the worker as its own hashed asset and
	// hands back its final URL — pdf.js does its actual parsing off the
	// main thread in this worker, not inline in this module.
	import pdfjsWorkerUrl from 'pdfjs-dist/build/pdf.worker.min.mjs?url';
	import { t } from '$lib/i18n';

	pdfjsLib.GlobalWorkerOptions.workerSrc = pdfjsWorkerUrl;

	// A blob, not a URL: <iframe src="blob:..."> is what this component
	// replaces — Chrome's own inline PDF viewer, which browser-native
	// support does show reliably on desktop, does *not* reliably render a
	// blob: URL inside an iframe on Android Chrome (confirmed: a blank
	// preview pane on a real phone, despite the identical code working in
	// every desktop/E2E check). Rendering every page to a <canvas> with
	// pdf.js sidesteps the platform's own PDF-in-iframe support entirely,
	// so it behaves identically everywhere instead of depending on it.
	let { blob }: { blob: Blob } = $props();

	let container = $state<HTMLDivElement>();
	let rendering = $state(true);
	let error = $state('');
	let loadingTask: PDFDocumentLoadingTask | null = null;
	let pdfDoc: PDFDocumentProxy | null = null;

	onMount(async () => {
		try {
			const bytes = new Uint8Array(await blob.arrayBuffer());
			// destroy() (used below) lives on the loading task itself, not
			// the resolved document — both are kept for that reason.
			loadingTask = pdfjsLib.getDocument({ data: bytes });
			pdfDoc = await loadingTask.promise;
			await renderAllPages();
		} catch (err) {
			error = err instanceof Error ? err.message : $t('viewers.errors.couldNotRenderPdf');
		} finally {
			rendering = false;
		}
	});

	onDestroy(() => {
		loadingTask?.destroy();
	});

	async function renderAllPages() {
		if (!pdfDoc || !container) return;
		// Sized to fill the available width, one continuously-scrollable
		// column of pages — the common lightweight PDF-viewer layout,
		// avoids needing separate pagination controls for a first version.
		const containerWidth = Math.max(container.clientWidth, 280);
		const devicePixelRatioSafe = window.devicePixelRatio || 1;

		for (let pageNum = 1; pageNum <= pdfDoc.numPages; pageNum++) {
			const page = await pdfDoc.getPage(pageNum);
			const baseViewport = page.getViewport({ scale: 1 });
			const viewport = page.getViewport({ scale: containerWidth / baseViewport.width });

			const canvas = document.createElement('canvas');
			canvas.className = 'pdf-page';
			// Backed by more physical pixels than CSS pixels on a
			// high-density (e.g. phone) screen, then scaled back down via
			// CSS width/height — otherwise text renders visibly blurry on
			// exactly the devices this fix is for.
			canvas.width = Math.floor(viewport.width * devicePixelRatioSafe);
			canvas.height = Math.floor(viewport.height * devicePixelRatioSafe);
			canvas.style.width = `${viewport.width}px`;
			canvas.style.height = `${viewport.height}px`;

			const ctx = canvas.getContext('2d');
			if (!ctx) continue;
			ctx.scale(devicePixelRatioSafe, devicePixelRatioSafe);

			await page.render({ canvasContext: ctx, viewport, canvas }).promise;
			container.appendChild(canvas);
		}
	}
</script>

{#if rendering}
	<p>{$t('viewers.renderingPdf')}</p>
{/if}
{#if error}
	<p class="error-text">{error}</p>
{/if}
<!--
	Deliberately empty of any Svelte-managed content: every page canvas
	below is appended imperatively (see renderAllPages) — mixing that with
	Svelte's own declarative children of the same element would fight over
	who owns this node's DOM.
-->
<div class="pdf-pages" bind:this={container}></div>
