<script lang="ts">
	import { t } from '$lib/i18n';
	import type { TypeFilter, DateFilter, OwnerFilter } from '$lib/searchFilters';

	// Doesn't own the query text itself (routes/+page.svelte's existing
	// search box does) — this only ever refines whatever's already been
	// typed there, same as Drive's own "Ricerca avanzata" modal layers
	// filters on top of the search bar instead of replacing it.
	let {
		open = $bindable(false),
		typeFilter = $bindable<TypeFilter>('any'),
		dateFilter = $bindable<DateFilter>('any'),
		ownerFilter = $bindable<OwnerFilter>('any')
	}: {
		open?: boolean;
		typeFilter?: TypeFilter;
		dateFilter?: DateFilter;
		ownerFilter?: OwnerFilter;
	} = $props();

	let dialogEl: HTMLDialogElement;

	$effect(() => {
		if (open) dialogEl?.showModal();
		else dialogEl?.close();
	});

	function close() {
		open = false;
	}

	function reset() {
		typeFilter = 'any';
		dateFilter = 'any';
		ownerFilter = 'any';
	}
</script>

<dialog bind:this={dialogEl} onclose={close} class="card">
	<h2>{$t('dialogs.advancedSearch.heading')}</h2>

	<div class="field">
		<label for="search-filter-type">{$t('dialogs.advancedSearch.type')}</label>
		<select id="search-filter-type" bind:value={typeFilter}>
			<option value="any">{$t('dialogs.advancedSearch.typeAny')}</option>
			<option value="folder">{$t('dialogs.advancedSearch.typeFolder')}</option>
			<option value="image">{$t('dialogs.advancedSearch.typeImage')}</option>
			<option value="video">{$t('dialogs.advancedSearch.typeVideo')}</option>
			<option value="audio">{$t('dialogs.advancedSearch.typeAudio')}</option>
			<option value="word">{$t('dialogs.advancedSearch.typeWord')}</option>
			<option value="excel">{$t('dialogs.advancedSearch.typeExcel')}</option>
			<option value="powerpoint">{$t('dialogs.advancedSearch.typePowerpoint')}</option>
			<option value="pdf">{$t('dialogs.advancedSearch.typePdf')}</option>
			<option value="archive">{$t('dialogs.advancedSearch.typeArchive')}</option>
			<option value="text">{$t('dialogs.advancedSearch.typeText')}</option>
			<option value="generic">{$t('dialogs.advancedSearch.typeGeneric')}</option>
		</select>
	</div>

	<div class="field">
		<label for="search-filter-modified">{$t('dialogs.advancedSearch.modified')}</label>
		<select id="search-filter-modified" bind:value={dateFilter}>
			<option value="any">{$t('dialogs.advancedSearch.modifiedAny')}</option>
			<option value="today">{$t('dialogs.advancedSearch.modifiedToday')}</option>
			<option value="week">{$t('dialogs.advancedSearch.modifiedWeek')}</option>
			<option value="month">{$t('dialogs.advancedSearch.modifiedMonth')}</option>
			<option value="year">{$t('dialogs.advancedSearch.modifiedYear')}</option>
		</select>
	</div>

	<div class="field">
		<label for="search-filter-owner">{$t('dialogs.advancedSearch.owner')}</label>
		<select id="search-filter-owner" bind:value={ownerFilter}>
			<option value="any">{$t('dialogs.advancedSearch.ownerAny')}</option>
			<option value="mine">{$t('dialogs.advancedSearch.ownerMine')}</option>
			<option value="shared">{$t('dialogs.advancedSearch.ownerShared')}</option>
		</select>
	</div>

	<div class="dialog-actions">
		<button class="btn" onclick={reset}>{$t('dialogs.advancedSearch.reset')}</button>
		<button class="btn btn-primary" onclick={close}>{$t('dialogs.advancedSearch.apply')}</button>
	</div>
</dialog>
