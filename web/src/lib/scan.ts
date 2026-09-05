import { buildPdf, type PdfPage } from './pdf';

const POINTS_PER_INCH = 72;
// A scanned page isn't screen pixels — this only needs to produce a sane
// physical page size for the PDF's MediaBox, not match a real scanner's
// exact DPI.
const ASSUMED_DPI = 150;
// Camera photos can be tens of megabytes at full sensor resolution —
// unreasonable for what's meant to be a multi-page *document* scan, not a
// photo album. This caps each page's longest side before it ever reaches
// the PDF.
const MAX_DIMENSION_PX = 2000;
const JPEG_QUALITY = 0.82;

/** Normalizes one captured photo (whatever format/orientation the camera
 * gave us) into a page ready for buildPdf() — always re-encoded as JPEG
 * through a canvas, never the raw camera bytes passed straight through.
 * The canvas round-trip is what applies EXIF orientation correctly and
 * caps the size; both are needed for every source, not an edge case. */
export async function photoToPdfPage(blob: Blob): Promise<PdfPage> {
	const bitmap = await createImageBitmap(blob, { imageOrientation: 'from-image' });
	const scale = Math.min(1, MAX_DIMENSION_PX / Math.max(bitmap.width, bitmap.height));
	const width = Math.round(bitmap.width * scale);
	const height = Math.round(bitmap.height * scale);

	const canvas = document.createElement('canvas');
	canvas.width = width;
	canvas.height = height;
	const ctx = canvas.getContext('2d');
	if (!ctx) throw new Error('canvas 2D context unavailable');
	ctx.drawImage(bitmap, 0, 0, width, height);
	bitmap.close();

	const jpegBlob = await new Promise<Blob | null>((resolve) => canvas.toBlob(resolve, 'image/jpeg', JPEG_QUALITY));
	if (!jpegBlob) throw new Error('could not encode the captured photo as JPEG');
	const jpegBytes = new Uint8Array(await jpegBlob.arrayBuffer());

	return {
		jpegBytes,
		pixelWidth: width,
		pixelHeight: height,
		pageWidthPt: (width / ASSUMED_DPI) * POINTS_PER_INCH,
		pageHeightPt: (height / ASSUMED_DPI) * POINTS_PER_INCH
	};
}

/** Converts a set of captured photos, in order, into a single multi-page
 * PDF — the client-side counterpart to Google Drive's own "scan to PDF"
 * camera flow. */
export async function photosToPdf(blobs: Blob[]): Promise<Blob> {
	const pages = await Promise.all(blobs.map(photoToPdfPage));
	return buildPdf(pages);
}
