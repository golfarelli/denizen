// Hand-rolled minimal PDF writer: one full-page JPEG per page, nothing
// else — no fonts, no compression, no text. That's a small, bounded
// subset of the PDF spec (a handful of object types, a linear xref table),
// unlike something genuinely complex like resumable uploads (see
// upload.ts, which does pull in a real client for that reason) — a good
// fit for CONTRIBUTING.md's preference for implementing small, self-
// contained things ourselves instead of a general-purpose PDF library
// dependency for what's really just "put these images in a PDF".
//
// JPEG bytes are embedded as-is (via the DCTDecode filter, which just
// means "this stream is already a JPEG, don't touch it") — no
// re-encoding happens at this layer, so callers are responsible for
// producing a JPEG blob already sized/oriented/compressed the way they
// want it to end up in the PDF (see scan.ts).

export interface PdfPage {
	/** Raw JPEG bytes, exactly as they should appear in the PDF. */
	jpegBytes: Uint8Array;
	/** The JPEG's own pixel dimensions — required by the PDF image
	 *  dictionary, independent of the page's physical size below. */
	pixelWidth: number;
	pixelHeight: number;
	/** The page's physical size in PDF points (1/72 inch). */
	pageWidthPt: number;
	pageHeightPt: number;
}

const encoder = new TextEncoder();

export function buildPdf(pages: PdfPage[]): Blob {
	if (pages.length === 0) throw new Error('buildPdf: at least one page is required');

	const chunks: BlobPart[] = [];
	const offsets: number[] = []; // offsets[n] = byte offset of object n
	let length = 0;

	function write(part: string | Uint8Array) {
		const bytes = typeof part === 'string' ? encoder.encode(part) : part;
		// A Uint8Array is always a valid BlobPart at runtime regardless of
		// what kind of buffer backs it — lib.dom's BlobPart type is just
		// stricter than that (ArrayBuffer-backed only, excluding
		// SharedArrayBuffer) than the plain `Uint8Array` this function
		// accepts, which is a real type-level distinction with no bearing
		// here (nothing in this module ever touches a SharedArrayBuffer).
		chunks.push(bytes as BlobPart);
		length += bytes.length;
	}

	function beginObject(num: number) {
		offsets[num] = length;
	}

	write('%PDF-1.4\n');

	// Object numbering: 1 = Catalog, 2 = Pages (the tree root), then per
	// page i (0-based): 3+3i = Page, 4+3i = Image XObject, 5+3i = content
	// stream that draws it.
	const firstPageObj = 3;
	const pageObjNums = pages.map((_, i) => firstPageObj + i * 3);

	beginObject(1);
	write(`1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n`);

	beginObject(2);
	write(
		`2 0 obj\n<< /Type /Pages /Count ${pages.length} /Kids [${pageObjNums
			.map((n) => `${n} 0 R`)
			.join(' ')}] >>\nendobj\n`
	);

	for (let i = 0; i < pages.length; i++) {
		const page = pages[i];
		const pageObj = firstPageObj + i * 3;
		const imageObj = pageObj + 1;
		const contentObj = pageObj + 2;
		const w = round2(page.pageWidthPt);
		const h = round2(page.pageHeightPt);

		beginObject(pageObj);
		write(
			`${pageObj} 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 ${w} ${h}] ` +
				`/Resources << /XObject << /Im0 ${imageObj} 0 R >> >> /Contents ${contentObj} 0 R >>\nendobj\n`
		);

		beginObject(imageObj);
		write(
			`${imageObj} 0 obj\n<< /Type /XObject /Subtype /Image /Width ${page.pixelWidth} ` +
				`/Height ${page.pixelHeight} /ColorSpace /DeviceRGB /BitsPerComponent 8 ` +
				`/Filter /DCTDecode /Length ${page.jpegBytes.length} >>\nstream\n`
		);
		write(page.jpegBytes);
		write(`\nendstream\nendobj\n`);

		// The `cm` matrix scales the unit square Do always draws an image
		// XObject into up to the full page size, positioned at the origin —
		// the standard way to make one image fill one page exactly.
		const content = `q\n${w} 0 0 ${h} 0 0 cm\n/Im0 Do\nQ\n`;
		beginObject(contentObj);
		write(`${contentObj} 0 obj\n<< /Length ${content.length} >>\nstream\n${content}endstream\nendobj\n`);
	}

	const xrefOffset = length;
	const objectCount = offsets.length; // includes the unused index 0 slot
	write(`xref\n0 ${objectCount}\n`);
	write('0000000000 65535 f \n');
	for (let n = 1; n < objectCount; n++) {
		write(`${String(offsets[n]).padStart(10, '0')} 00000 n \n`);
	}
	write(`trailer\n<< /Size ${objectCount} /Root 1 0 R >>\nstartxref\n${xrefOffset}\n%%EOF`);

	return new Blob(chunks, { type: 'application/pdf' });
}

function round2(n: number): number {
	return Math.round(n * 100) / 100;
}
