<script lang="ts">
	// A small set of colored, generic file/folder glyphs — deliberately not
	// an attempt to reproduce any real application's actual logo (leaving
	// trademark concerns aside, hand-drawing a dozen brand marks is far more
	// effort than a graphic touch-up calls for).
	//
	// Files render as a rounded-square color chip (Drive's own mobile app
	// icon shape — see the file-browser row comparison this was modeled
	// after) with either a short 2-4 letter label in the middle
	// (word/excel/powerpoint/pdf/text/archive, where a label is genuinely
	// the clearest cue) or a small glyph instead (image/video, where a
	// picture/play icon reads faster than text would). Folders stay a flat
	// colored silhouette with no chip background, matching how Drive
	// itself draws the two differently — a folder is a container shape,
	// not a document type badge.
	// size defaults to this component's normal list-row size; callers that
	// need it bigger (currently just routes/s/[token]/+page.svelte's share
	// landing card, where it's the one focal element on an otherwise
	// mostly empty page) pass a larger CSS length instead of fighting this
	// component's own scoped styles from outside.
	import { fileKind, type FileKind } from '$lib/fileKind';

	let {
		type,
		name,
		mimeType,
		size = '1.75rem'
	}: { type: 'file' | 'folder'; name: string; mimeType?: string; size?: string } = $props();

	const COLORS: Record<FileKind, string> = {
		folder: '#f2b429',
		word: '#3b6fe0',
		excel: '#1f9d55',
		powerpoint: '#e2711d',
		pdf: '#e5484d',
		image: '#0891b2',
		video: '#c2277a',
		audio: '#8b5cf6',
		archive: '#8a6d3b',
		text: '#64748b',
		generic: '#9095a3'
	};

	const LABELS: Partial<Record<FileKind, string>> = {
		word: 'DOC',
		excel: 'XLS',
		powerpoint: 'PPT',
		pdf: 'PDF',
		archive: 'ZIP',
		text: 'TXT'
	};

	let kind = $derived(fileKind(type, name, mimeType));
	let color = $derived(COLORS[kind]);
	let label = $derived(LABELS[kind]);
</script>

{#if kind === 'folder'}
	<svg viewBox="0 0 24 24" class="file-icon file-icon-folder" style:--file-icon-size={size} aria-hidden="true">
		<path fill={color} d="M4 6a2 2 0 0 1 2-2h4l2 2h6a2 2 0 0 1 2 2v9a2 2 0 0 1-2 2H6a2 2 0 0 1-2-2V6Z" />
	</svg>
{:else}
	<span class="file-icon-chip" style:background={color} style:--file-icon-size={size} aria-hidden="true">
		{#if kind === 'image'}
			<svg viewBox="0 0 24 24" class="file-icon-glyph">
				<circle cx="8.5" cy="9.5" r="1.6" fill="#fff" />
				<path fill="#fff" d="M4 18.5 8 13l3 3.2L15.5 11l4.5 7.5H4Z" />
			</svg>
		{:else if kind === 'video'}
			<svg viewBox="0 0 24 24" class="file-icon-glyph">
				<path fill="#fff" d="M9 8v8a.9.9 0 0 0 1.36.77l6.5-4a.9.9 0 0 0 0-1.54l-6.5-4A.9.9 0 0 0 9 8Z" />
			</svg>
		{:else if label}
			<span class="file-icon-label">{label}</span>
		{:else}
			<svg viewBox="0 0 24 24" class="file-icon-glyph">
				<path
					fill="#fff"
					fill-opacity="0.9"
					d="M6 3a1 1 0 0 0-1 1v16a1 1 0 0 0 1 1h12a1 1 0 0 0 1-1V8l-5-5H6Z"
				/>
			</svg>
		{/if}
	</span>
{/if}

<style>
	.file-icon {
		width: var(--file-icon-size, 1.75rem);
		height: var(--file-icon-size, 1.75rem);
		flex-shrink: 0;
		display: block;
	}

	.file-icon-chip {
		width: var(--file-icon-size, 1.75rem);
		height: var(--file-icon-size, 1.75rem);
		/* Scales with the chip instead of a fixed 6px, which read as
		   barely-rounded once the size prop pushes this well past its
		   normal list-row footprint. */
		border-radius: calc(var(--file-icon-size, 1.75rem) * 0.21);
		flex-shrink: 0;
		display: flex;
		align-items: center;
		justify-content: center;
		/* A thin top highlight + a slight drop shadow — enough to read as a
		   glossy chip instead of a flat color swatch, without changing the
		   per-type color language itself. */
		box-shadow:
			inset 0 1px 0 rgb(255 255 255 / 20%),
			0 1px 2px rgb(0 0 0 / 18%);
	}

	.file-icon-label {
		color: #fff;
		font-size: calc(var(--file-icon-size, 1.75rem) * 0.31);
		font-weight: 700;
		font-family: system-ui, sans-serif;
		letter-spacing: 0.02em;
	}

	.file-icon-glyph {
		width: 65%;
		height: 65%;
	}
</style>
