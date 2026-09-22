// Drag-and-drop moving of files/folders (desktop): drag rows/tiles in the
// file browser onto a folder — another row/tile in the current listing, or
// an entry in the sidebar's folder tree (including "Home", the root).
//
// The dragged items live in a module-level store rather than only in the
// DataTransfer: the two ends of a drag are different components (routes/
// +page.svelte starts it, +layout.svelte's tree and lib/FolderTreeItem.svelte
// receive it) and DataTransfer can only carry strings. DataTransfer still
// gets a custom type so browsers that require *some* data to start a drag
// (Firefox) do, and so a drop target can tell "moving items around inside
// Denizen" apart from "files dragged in from the OS to upload" — the latter
// is routes/+page.svelte's own .dropzone, which has to ignore the former.
//
// A drop reuses the exact move the "Move to…" dialog performs (api.move,
// name unchanged), so the server's own guard rails apply as-is — e.g. a
// folder can't be moved into itself or one of its own descendants; that
// surfaces here as a failed item, not as anything this file second-guesses.

import { writable, get } from 'svelte/store';
import { api, type Item } from '$lib/api';

export const DRAG_MIME = 'application/x-denizen-items';

/** The items being dragged right now (already narrowed to ones the caller can edit), or null. */
export const dragItems = writable<Item[] | null>(null);

export interface MoveResult {
	/** Increments on every drop — lets a listener tell a new result from one it already handled. */
	seq: number;
	moved: number;
	failed: number;
	/** Name of the first item moved (for a "Moved “x” to y" message). */
	firstName: string;
	folderName: string;
}

/** The outcome of the latest drop-move; routes/+page.svelte reloads and reports on it. */
export const lastMoveResult = writable<MoveResult | null>(null);

/** True when this drag is one of Denizen's own item drags (not files from the OS). */
export function isMoveDrag(e: DragEvent): boolean {
	return e.dataTransfer?.types.includes(DRAG_MIME) ?? false;
}

/** What actually moves when dropping items into folderId (null = root). */
function movable(items: Item[], folderId: string | null): Item[] {
	// Dropping a folder onto itself, or something onto the folder it's
	// already in, changes nothing — leave those out rather than send a
	// pointless request (or, for a folder onto itself, one the server would
	// reject).
	return items.filter((item) => item.id !== folderId && item.parent_id !== folderId);
}

/** Whether dropping items into folderId would move anything. */
export function canDropInto(items: Item[] | null, folderId: string | null): boolean {
	return !!items && movable(items, folderId).length > 0;
}

/**
 * Moves the currently dragged items into folderId, then publishes the
 * outcome on lastMoveResult. One item failing (a name collision, moving a
 * folder into its own descendant, ...) doesn't stop the rest — same as the
 * "Move to…" dialog's bulk move.
 */
export async function dropMoveInto(folderId: string | null, folderName: string): Promise<void> {
	const items = get(dragItems);
	endDrag();
	if (!items) return;
	const toMove = movable(items, folderId);
	if (toMove.length === 0) return;

	const results = await Promise.allSettled(toMove.map((item) => api.move(item.id, item.name, folderId)));
	const failed = results.filter((r) => r.status === 'rejected').length;
	lastMoveResult.set({
		seq: (get(lastMoveResult)?.seq ?? 0) + 1,
		moved: toMove.length - failed,
		failed,
		firstName: toMove[0].name,
		folderName
	});
}

/** Ends a drag: forgets the dragged items and clears any drop-target highlight left behind. */
export function endDrag() {
	dragItems.set(null);
	if (typeof document !== 'undefined') {
		for (const el of document.querySelectorAll('.drop-target')) el.classList.remove('drop-target');
	}
}

interface DropTargetOptions {
	folderId: string | null;
	folderName: string;
	/** False for anything that isn't a valid destination (a file, a shortcut, a folder the caller can't edit). */
	enabled: boolean;
}

/**
 * Svelte action: makes an element a drop target for item drags. While a
 * valid drag is over it the element gets the `drop-target` class
 * (highlighted in app.css); dropping performs the move.
 */
export function dropTarget(node: HTMLElement, initial: DropTargetOptions) {
	let options = initial;

	const accepts = (e: DragEvent) => options.enabled && isMoveDrag(e) && canDropInto(get(dragItems), options.folderId);

	function onOver(e: DragEvent) {
		if (!accepts(e)) return;
		e.preventDefault(); // what marks this element as a valid drop target
		if (e.dataTransfer) e.dataTransfer.dropEffect = 'move';
		node.classList.add('drop-target');
	}

	function onLeave(e: DragEvent) {
		// Moving between this element's own children also fires dragleave.
		if (e.relatedTarget instanceof Node && node.contains(e.relatedTarget)) return;
		node.classList.remove('drop-target');
	}

	function onDrop(e: DragEvent) {
		node.classList.remove('drop-target');
		if (!accepts(e)) return;
		e.preventDefault();
		e.stopPropagation();
		void dropMoveInto(options.folderId, options.folderName);
	}

	node.addEventListener('dragenter', onOver);
	node.addEventListener('dragover', onOver);
	node.addEventListener('dragleave', onLeave);
	node.addEventListener('drop', onDrop);

	return {
		update(next: DropTargetOptions) {
			options = next;
		},
		destroy() {
			node.removeEventListener('dragenter', onOver);
			node.removeEventListener('dragover', onOver);
			node.removeEventListener('dragleave', onLeave);
			node.removeEventListener('drop', onDrop);
		}
	};
}
