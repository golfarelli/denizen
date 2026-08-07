<script lang="ts">
	import { onMount } from 'svelte';
	import { renderAsync } from 'docx-preview';
	import { t } from '$lib/i18n';

	let { blob }: { blob: Blob } = $props();

	let container = $state<HTMLDivElement>();
	let rendering = $state(true);
	let error = $state('');

	onMount(async () => {
		if (!container) return;
		try {
			// Same container for both the rendered body and the styles
			// docx-preview generates for it — keeps everything scoped inside
			// this component instead of leaking a <style> tag into the rest
			// of the page.
			await renderAsync(blob, container, container, {
				ignoreHeight: true, // flow to natural height instead of a fixed simulated page size
				className: 'docx-page'
			});
		} catch (err) {
			error = err instanceof Error ? err.message : $t('viewers.errors.couldNotRenderDocument');
		} finally {
			rendering = false;
		}
	});
</script>

{#if rendering}
	<p>{$t('viewers.renderingDocument')}</p>
{/if}
{#if error}
	<p class="error-text">{error}</p>
{/if}
<!-- Deliberately empty of Svelte-managed content, same reasoning as
     PdfViewer's own container: docx-preview renders into this node
     imperatively. -->
<div class="docx-container" bind:this={container}></div>
