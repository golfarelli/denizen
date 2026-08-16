<script lang="ts">
	import { tabbarConfig, TAB_POOL, type TabKey } from '$lib/tabbarConfig';
	import { t } from '$lib/i18n';

	const ALL_KEYS = Object.keys(TAB_POOL) as TabKey[];

	// Each slot's own current value stays in its own dropdown (so picking it
	// doesn't make it vanish from itself) but is excluded from the other two,
	// so the same destination can't end up in two slots at once.
	function optionsFor(slot: number): TabKey[] {
		const others = $tabbarConfig.filter((_, i) => i !== slot);
		return ALL_KEYS.filter((key) => !others.includes(key));
	}

	function setSlot(slot: number, value: TabKey) {
		const next = [...$tabbarConfig];
		next[slot] = value;
		tabbarConfig.set(next);
	}
</script>

<svelte:head>
	<title>{$t('settings.title')} · Denizen</title>
</svelte:head>

<div class="toolbar">
	<h1 style="margin:0">{$t('settings.title')}</h1>
</div>

<section class="settings-section">
	<h2>{$t('settings.tabbarHeading')}</h2>
	<p class="hint">{$t('settings.tabbarHint')}</p>

	{#each $tabbarConfig as _, slot}
		<label class="settings-row">
			{$t('settings.tabbarSlot', { n: slot + 2 })}
			<select value={$tabbarConfig[slot]} onchange={(e) => setSlot(slot, e.currentTarget.value as TabKey)}>
				{#each optionsFor(slot) as key (key)}
					<option value={key}>{$t(TAB_POOL[key].labelKey)}</option>
				{/each}
			</select>
		</label>
	{/each}
</section>

<style>
	.settings-section {
		max-width: 32rem;
	}

	.settings-row {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: var(--space-3);
		padding: var(--space-3) 0;
		border-bottom: 1px solid var(--color-border);
	}

	.settings-row select {
		min-width: 12rem;
		border: 1px solid var(--color-border);
		border-radius: var(--radius);
		padding: var(--space-2) var(--space-3);
		background: var(--color-bg);
		color: inherit;
		font: inherit;
	}
</style>
