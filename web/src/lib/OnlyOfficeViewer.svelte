<script lang="ts">
	import { onMount, onDestroy } from 'svelte';
	import { api } from '$lib/api';
	import { t } from '$lib/i18n';

	// Phase 1 only: view. No editorConfig.callbackUrl/save-back handling
	// exists server-side yet — see internal/onlyoffice's own doc comment —
	// so nothing here needs to know the difference; the config this
	// fetches is already permissions.edit: false.
	let { itemId, apiJsUrl }: { itemId: string; apiJsUrl: string } = $props();

	let containerId = $derived(`onlyoffice-${itemId}`);
	let loading = $state(true);
	let error = $state('');

	// The editor instance DocsAPI.DocEditor hands back — not app state
	// Svelte needs to react to, just something to clean up on destroy.
	let editor: unknown = null;

	onMount(async () => {
		try {
			// OnlyOffice's "desktop" type (its own default) is the full
			// ribbon UI — real toolbar buttons sized for a mouse — which
			// renders tiny and cramped squeezed into a phone-width viewport
			// (confirmed live, not theoretical). "mobile" is its own
			// purpose-built alternative for exactly this. 768px is a common
			// phone/tablet breakpoint, not tied to any particular device.
			const editorType = window.innerWidth < 768 ? 'mobile' : 'desktop';
			const config = await api.getOnlyOfficeConfig(itemId, editorType);
			await loadScript(apiJsUrl);
			// DocsAPI is a global the script above attaches to window — not
			// a module this app imports, since it has to match whatever
			// Document Server version is actually configured (see
			// onlyoffice.Client.APIJSURL's own comment).
			const DocsAPI = (window as unknown as { DocsAPI: { DocEditor: new (id: string, config: unknown) => unknown } })
				.DocsAPI;
			editor = new DocsAPI.DocEditor(containerId, config);
		} catch (err) {
			error = err instanceof Error ? err.message : $t('viewers.errors.couldNotLoadEditor');
		} finally {
			loading = false;
		}
	});

	onDestroy(() => {
		(editor as { destroyEditor?: () => void } | null)?.destroyEditor?.();
	});

	function loadScript(src: string): Promise<void> {
		return new Promise((resolve, reject) => {
			if (document.querySelector(`script[src="${src}"]`)) {
				resolve();
				return;
			}
			const script = document.createElement('script');
			script.src = src;
			script.onload = () => resolve();
			script.onerror = () => reject(new Error($t('viewers.errors.couldNotLoadEditorScript')));
			document.head.appendChild(script);
		});
	}
</script>

{#if loading}
	<p>{$t('viewers.loadingEditor')}</p>
{/if}
{#if error}
	<p class="error-text">{error}</p>
{/if}
<!-- DocsAPI.DocEditor mounts its own iframe into this element by id,
     imperatively — same reasoning as PdfViewer/DocxViewer's own containers,
     kept empty of Svelte-managed content. -->
<div id={containerId} class="onlyoffice-container"></div>
