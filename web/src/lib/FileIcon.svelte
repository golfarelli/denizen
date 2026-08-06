<script lang="ts">
	// A small set of colored, generic file/folder glyphs — deliberately not
	// an attempt to reproduce any real application's actual logo (leaving
	// trademark concerns aside, hand-drawing a dozen brand marks is far more
	// effort than a graphic touch-up calls for). Each category is a plain
	// document silhouette in a distinct color, with either a short 2-4
	// letter label baked into the SVG (word/excel/powerpoint/pdf/text/
	// archive, where a label is genuinely the clearest cue) or a small glyph
	// instead (image/video, where a picture/play icon reads faster than
	// text would).
	let { type, name, mimeType }: { type: 'file' | 'folder'; name: string; mimeType?: string } = $props();

	const IMAGE_EXT = new Set(['jpg', 'jpeg', 'png', 'gif', 'webp', 'svg', 'bmp', 'heic', 'avif']);
	const VIDEO_EXT = new Set(['mp4', 'webm', 'mov', 'mkv', 'avi', 'm4v']);
	const AUDIO_EXT = new Set(['mp3', 'wav', 'ogg', 'flac', 'm4a', 'aac']);
	const ARCHIVE_EXT = new Set(['zip', 'rar', '7z', 'tar', 'gz', 'bz2']);
	// Mirrors file/[id]/+page.svelte's own TEXT_EXTENSIONS — kept as a
	// separate copy rather than a shared import since the two lists serve
	// different purposes (this one only ever picks a color/label, that one
	// decides which viewer component loads) and drifting slightly apart
	// (e.g. this one not needing 'csv', which is folded into 'excel'
	// instead) is fine.
	const TEXT_EXT = new Set([
		'txt',
		'md',
		'markdown',
		'json',
		'log',
		'yaml',
		'yml',
		'xml',
		'html',
		'htm',
		'css',
		'js',
		'ts',
		'ini',
		'conf',
		'toml',
		'sh',
		'go',
		'py'
	]);

	type Kind =
		| 'folder'
		| 'word'
		| 'excel'
		| 'powerpoint'
		| 'pdf'
		| 'image'
		| 'video'
		| 'audio'
		| 'archive'
		| 'text'
		| 'generic';

	function kindOf(): Kind {
		if (type === 'folder') return 'folder';
		const mime = mimeType ?? '';
		const ext = name.split('.').pop()?.toLowerCase() ?? '';
		if (ext === 'docx' || ext === 'doc') return 'word';
		if (ext === 'xlsx' || ext === 'xls' || ext === 'csv') return 'excel';
		if (ext === 'pptx' || ext === 'ppt') return 'powerpoint';
		if (ext === 'pdf' || mime === 'application/pdf') return 'pdf';
		if (mime.startsWith('image/') || IMAGE_EXT.has(ext)) return 'image';
		if (mime.startsWith('video/') || VIDEO_EXT.has(ext)) return 'video';
		if (mime.startsWith('audio/') || AUDIO_EXT.has(ext)) return 'audio';
		if (ARCHIVE_EXT.has(ext)) return 'archive';
		if (mime.startsWith('text/') || TEXT_EXT.has(ext)) return 'text';
		return 'generic';
	}

	const COLORS: Record<Kind, string> = {
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

	const LABELS: Partial<Record<Kind, string>> = {
		word: 'DOC',
		excel: 'XLS',
		powerpoint: 'PPT',
		pdf: 'PDF',
		archive: 'ZIP',
		text: 'TXT'
	};

	let kind = $derived(kindOf());
	let color = $derived(COLORS[kind]);
	let label = $derived(LABELS[kind]);
</script>

{#if kind === 'folder'}
	<svg viewBox="0 0 24 24" class="file-icon" aria-hidden="true">
		<path fill={color} d="M4 6a2 2 0 0 1 2-2h4l2 2h6a2 2 0 0 1 2 2v9a2 2 0 0 1-2 2H6a2 2 0 0 1-2-2V6Z" />
	</svg>
{:else}
	<svg viewBox="0 0 24 24" class="file-icon" aria-hidden="true">
		<!-- Page silhouette with a corner cut to read as "a folded page"
		     rather than a plain rectangle. -->
		<path fill={color} d="M6 2a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8l-6-6H6Z" />
		<path fill="#fff" fill-opacity="0.35" d="M14 2v5a1 1 0 0 0 1 1h5Z" />
		{#if kind === 'image'}
			<circle cx="9" cy="10.5" r="1.3" fill="#fff" />
			<path fill="#fff" d="M5 18.5 8.2 14l2.6 2.8L14.5 12l4.5 6.5H5Z" />
		{:else if kind === 'video'}
			<path fill="#fff" d="M10 8.6v6.8a.6.6 0 0 0 .93.5l5.4-3.4a.6.6 0 0 0 0-1.02l-5.4-3.4a.6.6 0 0 0-.93.52Z" />
		{:else if label}
			<text
				x="12"
				y="18"
				text-anchor="middle"
				font-size="6.5"
				font-weight="700"
				fill="#fff"
				font-family="system-ui, sans-serif">{label}</text
			>
		{/if}
	</svg>
{/if}

<style>
	.file-icon {
		width: 1.35rem;
		height: 1.35rem;
		flex-shrink: 0;
		display: block;
	}
</style>
