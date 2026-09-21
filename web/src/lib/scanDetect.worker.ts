// Runs OpenCV.js off the main thread, for scanDetect.ts.
//
// Loading OpenCV means executing a ~10MB script and compiling a WebAssembly
// module — seconds of work that, on the page's own thread, froze the whole
// UI (no dragging a corner, nothing) right when the crop step opens. In a
// worker it costs nothing but time: the page stays fully responsive and the
// detected corners simply arrive when they're ready.

import cvModule from '@techstark/opencv-js';
import { isConvexQuad, orderCorners, polygonArea, type Point } from './scanGeometry';

// A page smaller than this share of the photo is more likely a coaster, a
// window or a shadow than the document being scanned.
const MIN_AREA_FRACTION = 0.15;
// ...and one that's essentially the whole frame is the photo's own border
// (the thresholding strategy can produce one), not a page in it.
const MAX_AREA_FRACTION = 0.97;

interface DetectRequest {
	id: number;
	width: number;
	height: number;
	/** RGBA pixels of the (already downscaled) photo. */
	pixels: ArrayBuffer;
}

// eslint-disable-next-line @typescript-eslint/no-explicit-any
type CV = any;

let ready: Promise<{ cv: CV }> | null = null;

// Wrapped in an object: some Emscripten builds expose `cv` itself as a
// thenable, and returning that straight from an async function would make
// the promise machinery try to unwrap it.
function loadCv(): Promise<{ cv: CV }> {
	if (!ready) {
		ready = new Promise((resolve, reject) => {
			const cv = cvModule as CV;
			if (cv.Mat) return resolve({ cv });
			const timeout = setTimeout(() => reject(new Error('OpenCV did not initialise')), 60_000);
			cv.onRuntimeInitialized = () => {
				clearTimeout(timeout);
				resolve({ cv });
			};
		});
	}
	return ready;
}

/** The four corners of the page in the image, or null. */
function findQuad(cv: CV, width: number, height: number, pixels: ArrayBuffer): Point[] | null {
	const frameArea = width * height;
	const src = cv.matFromImageData(new ImageData(new Uint8ClampedArray(pixels), width, height));
	const gray = new cv.Mat();
	const blurred = new cv.Mat();
	try {
		cv.cvtColor(src, gray, cv.COLOR_RGBA2GRAY);
		cv.GaussianBlur(gray, blurred, new cv.Size(5, 5), 0);

		// Two strategies, in order: edge-based (a page against a contrasting
		// surface) and, if that finds nothing, a global threshold (a bright
		// page against a darker one even when its edges are soft).
		for (const strategy of ['edges', 'threshold'] as const) {
			const binary = new cv.Mat();
			try {
				if (strategy === 'edges') {
					cv.Canny(blurred, binary, 50, 150);
					// Close small gaps in the outline so it forms one contour.
					const kernel = cv.Mat.ones(3, 3, cv.CV_8U);
					cv.dilate(binary, binary, kernel);
					kernel.delete();
				} else {
					cv.threshold(blurred, binary, 0, 255, cv.THRESH_BINARY + cv.THRESH_OTSU);
				}
				const found = largestQuad(cv, binary, frameArea);
				if (found) return orderCorners(found);
			} finally {
				binary.delete();
			}
		}
		return null;
	} finally {
		src.delete();
		gray.delete();
		blurred.delete();
	}
}

/** The largest convex 4-sided contour in a binary image that's a plausible
 * page (see MIN/MAX_AREA_FRACTION), or null. */
function largestQuad(cv: CV, binary: CV, frameArea: number): Point[] | null {
	const contours = new cv.MatVector();
	const hierarchy = new cv.Mat();
	let best: Point[] | null = null;
	let bestArea = frameArea * MIN_AREA_FRACTION;
	try {
		cv.findContours(binary, contours, hierarchy, cv.RETR_LIST, cv.CHAIN_APPROX_SIMPLE);
		for (let i = 0; i < contours.size(); i++) {
			const contour = contours.get(i);
			const approx = new cv.Mat();
			try {
				// Simplify the outline; 2% of its perimeter is the usual
				// tolerance for "a page with slightly wobbly edges is still 4-sided".
				cv.approxPolyDP(contour, approx, 0.02 * cv.arcLength(contour, true), true);
				if (approx.rows !== 4) continue;
				const points: Point[] = [];
				for (let j = 0; j < 4; j++) points.push({ x: approx.data32S[j * 2], y: approx.data32S[j * 2 + 1] });
				const area = polygonArea(points);
				if (area <= bestArea || area >= frameArea * MAX_AREA_FRACTION) continue;
				if (!isConvexQuad(orderCorners(points))) continue;
				best = points;
				bestArea = area;
			} finally {
				contour.delete();
				approx.delete();
			}
		}
	} finally {
		contours.delete();
		hierarchy.delete();
	}
	return best;
}

const scope = self as unknown as {
	onmessage: ((e: MessageEvent<DetectRequest>) => void) | null;
	postMessage: (message: unknown) => void;
};

scope.onmessage = async (e) => {
	const { id, width, height, pixels } = e.data;
	try {
		const { cv } = await loadCv();
		scope.postMessage({ id, quad: findQuad(cv, width, height, pixels) });
	} catch {
		scope.postMessage({ id, quad: null });
	}
};
