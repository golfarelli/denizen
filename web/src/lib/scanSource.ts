// Loading a captured photo for the crop step.

/** Longest side of the working copy of a photo. A 12MP phone photo is ~48MB
 * as raw pixels and the crop step reads it all back for the warp — capping
 * it here keeps that bounded, while staying well above the final page size
 * (see CROP_MAX_DIMENSION_PX in scanWarp.ts) so cropping loses no quality
 * that would have survived to the PDF anyway. */
const MAX_SOURCE_DIMENSION_PX = 3200;

/** Decodes a photo into a canvas, EXIF orientation applied and size capped.
 * Every coordinate in the crop step (detected corners, handle positions,
 * the warp) is in this canvas's pixel space. */
export async function loadScanCanvas(blob: Blob): Promise<HTMLCanvasElement> {
	const bitmap = await createImageBitmap(blob, { imageOrientation: 'from-image' });
	const scale = Math.min(1, MAX_SOURCE_DIMENSION_PX / Math.max(bitmap.width, bitmap.height));
	const canvas = document.createElement('canvas');
	canvas.width = Math.max(1, Math.round(bitmap.width * scale));
	canvas.height = Math.max(1, Math.round(bitmap.height * scale));
	const ctx = canvas.getContext('2d');
	if (!ctx) throw new Error('canvas 2D context unavailable');
	ctx.drawImage(bitmap, 0, 0, canvas.width, canvas.height);
	bitmap.close();
	return canvas;
}
