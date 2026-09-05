<script lang="ts">
	import { onMount } from 'svelte';
	import * as XLSX from 'xlsx';
	import { t } from '$lib/i18n';

	let { blob }: { blob: Blob } = $props();

	let loading = $state(true);
	let error = $state('');
	let sheetNames = $state<string[]>([]);
	let activeSheet = $state('');
	let rows = $state<unknown[][]>([]);

	// Kept out of $state — a parsed SheetJS workbook isn't plain data Svelte
	// needs to react to, only sheet switching (selectSheet) needs to trigger
	// updates, and that's driven by reassigning `rows` directly.
	let workbook: XLSX.WorkBook | null = null;

	onMount(async () => {
		try {
			const bytes = new Uint8Array(await blob.arrayBuffer());
			workbook = XLSX.read(bytes, { type: 'array' });
			sheetNames = workbook.SheetNames;
			if (sheetNames.length > 0) selectSheet(sheetNames[0]);
		} catch (err) {
			error = err instanceof Error ? err.message : $t('viewers.errors.couldNotReadSpreadsheet');
		} finally {
			loading = false;
		}
	});

	function selectSheet(name: string) {
		if (!workbook) return;
		activeSheet = name;
		// header: 1 = an array of rows of raw cell values (never HTML —
		// unlike XLSX.utils.sheet_to_html, this can't be a foothold for a
		// malicious spreadsheet to inject markup into the page, since
		// Svelte's own {cell} interpolation below escapes it like any other
		// text, the same as an untrusted string from anywhere else).
		rows = XLSX.utils.sheet_to_json<unknown[]>(workbook.Sheets[name], { header: 1, blankrows: false });
	}
</script>

{#if loading}
	<p>{$t('viewers.readingSpreadsheet')}</p>
{:else if error}
	<p class="error-text">{error}</p>
{:else}
	{#if sheetNames.length > 1}
		<div class="xlsx-tabs">
			{#each sheetNames as name (name)}
				<button class="btn" class:btn-primary={name === activeSheet} onclick={() => selectSheet(name)}>
					{name}
				</button>
			{/each}
		</div>
	{/if}
	<div class="xlsx-table-wrap">
		<table class="xlsx-table">
			<tbody>
				{#each rows as row, i (i)}
					<tr>
						{#each row as cell, j (j)}
							<td>{cell ?? ''}</td>
						{/each}
					</tr>
				{/each}
			</tbody>
		</table>
	</div>
{/if}
