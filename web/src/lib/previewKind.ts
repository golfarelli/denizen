// Shared by both preview pages — the private one (routes/file/[id]/
// +page.svelte) and the public share landing page (routes/s/[token]/
// +page.svelte), which need to agree on exactly the same "what can this
// app render inline" decision so a shared file previews exactly like it
// does when you're looking at your own copy of it.

// Extensions treated as text even when the server's mime_type guess
// (Go's mime.TypeByExtension, or whatever the browser reported as
// File.type at upload time — see lib/upload.ts) is empty or generic:
// Go's own mime type database is OS-registration-based and doesn't
// reliably know about things like .md or .log.
const TEXT_EXTENSIONS = new Set([
	'txt',
	'md',
	'markdown',
	'json',
	'csv',
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

export type PreviewKind = 'image' | 'pdf' | 'docx' | 'xlsx' | 'pptx' | 'text' | 'video' | 'unsupported';

// Keyed by extension, not mime_type, for docx/xlsx/pptx specifically: all
// three are OOXML zip-based formats with mime types Go's own guesser
// (mime.TypeByExtension) doesn't reliably know, and the legacy binary
// predecessors (.doc, .xls) need telling apart from these anyway since
// only one of the two libraries this app has (xlsx, via SheetJS's bundled
// legacy parser) can actually read its legacy sibling — .doc has no
// viewer here, only .xls does.
export function previewKind(name: string, mimeType?: string): PreviewKind {
	const mime = mimeType ?? '';
	const ext = name.split('.').pop()?.toLowerCase() ?? '';
	if (mime.startsWith('image/')) return 'image';
	if (mime === 'application/pdf') return 'pdf';
	if (mime.startsWith('video/')) return 'video';
	if (ext === 'docx') return 'docx';
	if (ext === 'xlsx' || ext === 'xls') return 'xlsx';
	if (ext === 'pptx') return 'pptx'; // only ever previewable via OnlyOffice — no client-side viewer for it
	if (mime.startsWith('text/') || TEXT_EXTENSIONS.has(ext)) return 'text';
	return 'unsupported';
}
