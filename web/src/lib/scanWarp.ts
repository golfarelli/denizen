// Perspective correction for the document scanner: takes the photo and the
// four corners the user confirmed, and produces the flattened, cropped
// page as a JPEG.
//
// Written by hand rather than pulling in OpenCV for it (see
// CONTRIBUTING.md's "Minimal dependencies"): flattening a quad is a small,
// bounded piece of math — solve a homography, sample every output pixel
// once — unlike *finding* the document in the photo, which is real
// computer vision and lives in scanDetect.ts behind a lazily loaded
// library. Keeping the warp independent of that library also means a
// manual crop still works if the library can't load.

import { quadOutputSize, solveHomography, type Quad } from './scanGeometry';

/** Longest side of a cropped page — matches lib/scan.ts's own cap, so the
 * later PDF step doesn't have to resample it again. */
export const CROP_MAX_DIMENSION_PX = 2000;
const CROP_JPEG_QUALITY = 0.92;

/** Flattens the region of `source` bounded by `quad` (in the source
 * canvas's own pixel coordinates) into a new canvas. */
export function warpQuad(source: HTMLCanvasElement, quad: Quad, maxLongSide = CROP_MAX_DIMENSION_PX): HTMLCanvasElement {
	const { width: outW, height: outH } = quadOutputSize(quad, maxLongSide);

	const srcCtx = source.getContext('2d');
	if (!srcCtx) throw new Error('canvas 2D context unavailable');
	const src = srcCtx.getImageData(0, 0, source.width, source.height);
	const sw = src.width;
	const sh = src.height;
	const sd = src.data;

	// Output pixel (u, v) → source position, via the homography that maps
	// the output rectangle's corners onto the quad's.
	const dstRect: Quad = [
		{ x: 0, y: 0 },
		{ x: outW, y: 0 },
		{ x: outW, y: outH },
		{ x: 0, y: outH }
	];
	const [h11, h12, h13, h21, h22, h23, h31, h32] = solveHomography(dstRect, quad);

	const out = new ImageData(outW, outH);
	const od = out.data;
	let o = 0;
	for (let v = 0; v < outH; v++) {
		for (let u = 0; u < outW; u++) {
			// Sample at the pixel's centre, not its corner.
			const pu = u + 0.5;
			const pv = v + 0.5;
			const w = h31 * pu + h32 * pv + 1;
			const x = (h11 * pu + h12 * pv + h13) / w - 0.5;
			const y = (h21 * pu + h22 * pv + h23) / w - 0.5;

			// Bilinear interpolation, edge-clamped.
			const x0 = Math.min(sw - 1, Math.max(0, Math.floor(x)));
			const y0 = Math.min(sh - 1, Math.max(0, Math.floor(y)));
			const x1 = Math.min(sw - 1, x0 + 1);
			const y1 = Math.min(sh - 1, y0 + 1);
			const fx = Math.min(1, Math.max(0, x - x0));
			const fy = Math.min(1, Math.max(0, y - y0));
			const i00 = (y0 * sw + x0) * 4;
			const i10 = (y0 * sw + x1) * 4;
			const i01 = (y1 * sw + x0) * 4;
			const i11 = (y1 * sw + x1) * 4;
			const w00 = (1 - fx) * (1 - fy);
			const w10 = fx * (1 - fy);
			const w01 = (1 - fx) * fy;
			const w11 = fx * fy;
			od[o++] = sd[i00] * w00 + sd[i10] * w10 + sd[i01] * w01 + sd[i11] * w11;
			od[o++] = sd[i00 + 1] * w00 + sd[i10 + 1] * w10 + sd[i01 + 1] * w01 + sd[i11 + 1] * w11;
			od[o++] = sd[i00 + 2] * w00 + sd[i10 + 2] * w10 + sd[i01 + 2] * w01 + sd[i11 + 2] * w11;
			od[o++] = 255;
		}
	}

	const canvas = document.createElement('canvas');
	canvas.width = outW;
	canvas.height = outH;
	const ctx = canvas.getContext('2d');
	if (!ctx) throw new Error('canvas 2D context unavailable');
	ctx.putImageData(out, 0, 0);
	return canvas;
}

/** warpQuad, encoded as the JPEG that becomes one page of the scan. */
export async function cropToJpeg(source: HTMLCanvasElement, quad: Quad): Promise<Blob> {
	const canvas = warpQuad(source, quad);
	const blob = await new Promise<Blob | null>((resolve) => canvas.toBlob(resolve, 'image/jpeg', CROP_JPEG_QUALITY));
	if (!blob) throw new Error('could not encode the cropped page as JPEG');
	return blob;
}
