<script lang="ts">
	// A shimmering placeholder shaped like the real list, instead of a bare
	// "Loading…" line — reuses .item-row/.item-icon/.item-name/... (see
	// app.css) for identical grid alignment, so the layout doesn't visibly
	// jump once the real rows replace this. Slightly varied bar widths
	// (cycled by index, not random — a fixed pattern avoids the skeleton
	// visibly re-shuffling on every re-render) read less mechanically than
	// every row being an identical block.
	let { rows = 6 }: { rows?: number } = $props();

	const NAME_WIDTHS = ['55%', '70%', '40%', '62%'];
</script>

<div class="item-list" aria-hidden="true">
	{#each { length: rows } as _, i (i)}
		<div class="item-row">
			<span class="item-icon"><span class="skeleton-block skeleton-circle"></span></span>
			<span class="item-name"><span class="skeleton-block" style:width={NAME_WIDTHS[i % NAME_WIDTHS.length]}></span></span>
			<span class="item-modified"><span class="skeleton-block" style:width="65%"></span></span>
			<span class="item-size"><span class="skeleton-block" style:width="45%"></span></span>
		</div>
	{/each}
</div>
