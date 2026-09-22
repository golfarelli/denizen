// Automatic document-edge detection for the scanner's crop step: finds the
// page in a photo and returns its four corners, to pre-fill the handles the
// user can then adjust (ScanCropper.svelte).
//
// This is the one part of the scanner that's real computer vision, so it
// uses OpenCV.js (see CONTRIBUTING.md's "Minimal dependencies" on where the
// line is) — a large (~10MB) WebAssembly build. It lives in a Web Worker
// (scanDetect.worker.ts), created on first use: loading it takes seconds and
// would otherwise freeze the page. Nothing else in the scanner depends on
// it: if it can't load (offline, blocked, out of memory) detectDocumentQuad
// resolves null and the corners start at their default inset, the manual
// crop working exactly as before.

import { orderCorners, type Quad } from './scanGeometry';

// Detection runs on a downscaled copy: edges are found just as well at this
// size, at a fraction of the time and memory.
const DETECT_MAX_DIMENSION_PX = 640;
// Gives up (resolving null) rather than leaving the crop step "looking for
// the page edges" forever if the worker never answers.
const DETECT_TIMEOUT_MS = 90_000;

let worker: Worker | null = null;
let nextRequestId = 0;
const pending = new Map<number, (quad: { x: number; y: number }[] | null) => void>();

function getWorker(): Worker {
	if (!worker) {
		worker = new Worker(new URL('./scanDetect.worker.ts', import.meta.url), { type: 'module' });
		worker.onmessage = (e: MessageEvent<{ id: number; quad: { x: number; y: number }[] | null }>) => {
			pending.get(e.data.id)?.(e.data.quad);
			pending.delete(e.data.id);
		};
		worker.onerror = () => {
			// A worker that failed to load or crashed: settle everything
			// waiting on it, and start fresh next time.
			for (const settle of pending.values()) settle(null);
			pending.clear();
			disposeDocumentDetector();
		};
	}
	return worker;
}

/** Stops the worker and frees OpenCV's memory — call when a scan session ends. */
export function disposeDocumentDetector(): void {
	worker?.terminate();
	worker = null;
}

/**
 * Finds the page in `source` and returns its corners in `source`'s own pixel
 * coordinates, or null if nothing convincing was found (or OpenCV isn't
 * available).
 */
export function detectDocumentQuad(source: HTMLCanvasElement): Promise<Quad | null> {
	const scale = Math.min(1, DETECT_MAX_DIMENSION_PX / Math.max(source.width, source.height));
	const small = document.createElement('canvas');
	small.width = Math.max(1, Math.round(source.width * scale));
	small.height = Math.max(1, Math.round(source.height * scale));
	const ctx = small.getContext('2d');
	if (!ctx) return Promise.resolve(null);
	ctx.drawImage(source, 0, 0, small.width, small.height);
	const image = ctx.getImageData(0, 0, small.width, small.height);

	return new Promise((resolve) => {
		const id = nextRequestId++;
		const timeout = setTimeout(() => {
			pending.delete(id);
			resolve(null);
		}, DETECT_TIMEOUT_MS);
		pending.set(id, (points) => {
			clearTimeout(timeout);
			// Back from the downscaled copy to the photo's own pixels.
			resolve(points ? orderCorners(points.map((p) => ({ x: p.x / scale, y: p.y / scale }))) : null);
		});
		try {
			getWorker().postMessage({ id, width: image.width, height: image.height, pixels: image.data.buffer }, [image.data.buffer]);
		} catch {
			clearTimeout(timeout);
			pending.delete(id);
			resolve(null);
		}
	});
}
