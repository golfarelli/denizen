<script lang="ts">
	// A single up-pointing triangle, rotated for "desc" — up = ascending
	// for every column (name, modified, size alike), the conventional
	// table-sort convention, rather than flipping the meaning per column.
	let { direction }: { direction: 'asc' | 'desc' } = $props();
</script>

<svg
	width="10"
	height="10"
	viewBox="0 0 24 24"
	class="sort-arrow"
	style:transform={direction === 'desc' ? 'rotate(180deg)' : ''}
	aria-hidden="true"
>
	<path d="M12 5l7 10H5z" fill="currentColor" />
</svg>
