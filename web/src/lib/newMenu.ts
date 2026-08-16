import { writable } from 'svelte/store';

// The "+ New" trigger itself lives in the sidebar now (routes/
// +layout.svelte, first item — Drive-style, secondbrain session
// 2026-08-16), but everything it actually does (fileInput ref, ScanDialog,
// the upload progress panel, the folder-name prompt) stays owned by
// whichever page supports it — today only routes/+page.svelte, the file
// browser itself. This is the handoff: the page registers its handlers on
// mount and clears them on destroy; the sidebar button calls whatever's
// currently registered, or hides itself when nothing is (e.g. Trash,
// Shares — pages that don't support creating anything in place).
export interface NewMenuActions {
	upload: () => void;
	scan: () => void;
	newFolder: () => void;
	newBlankDocument: (ext: 'docx' | 'xlsx' | 'pptx') => void;
	// Mirrors the page's own currentFolderCanEdit — the sidebar button
	// hides itself when this is false, same as the old toolbar trigger's
	// {#if currentFolderCanEdit} did (browsing a shared read-only folder,
	// where creating anything would just 403 anyway).
	canCreate: boolean;
}

export const newMenuActions = writable<NewMenuActions | null>(null);
