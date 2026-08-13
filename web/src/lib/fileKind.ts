// Shared file-type categorization — originally FileIcon.svelte's own local
// kindOf(), extracted so the advanced search "Tipo" filter (routes/
// +page.svelte's SearchFilters) can group results the exact same way the
// icons already do, instead of a second, drifting copy of the same
// extension/mime-type rules.

export type FileKind = 'folder' | 'word' | 'excel' | 'powerpoint' | 'pdf' | 'image' | 'video' | 'audio' | 'archive' | 'text' | 'generic';

const IMAGE_EXT = new Set(['jpg', 'jpeg', 'png', 'gif', 'webp', 'svg', 'bmp', 'heic', 'avif']);
const VIDEO_EXT = new Set(['mp4', 'webm', 'mov', 'mkv', 'avi', 'm4v']);
const AUDIO_EXT = new Set(['mp3', 'wav', 'ogg', 'flac', 'm4a', 'aac']);
const ARCHIVE_EXT = new Set(['zip', 'rar', '7z', 'tar', 'gz', 'bz2']);
// Mirrors file/[id]/+page.svelte's own TEXT_EXTENSIONS — kept as a
// separate copy rather than a shared import since the two lists serve
// different purposes (this one only ever picks a color/label/filter
// bucket, that one decides which viewer component loads) and drifting
// slightly apart (e.g. this one not needing 'csv', which is folded into
// 'excel' instead) is fine.
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

export function fileKind(type: 'file' | 'folder', name: string, mimeType?: string): FileKind {
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
