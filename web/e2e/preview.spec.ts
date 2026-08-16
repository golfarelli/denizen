import { test, expect } from '@playwright/test';
import path from 'path';
import { readFileSync } from 'fs';
import { openViaDblclick } from './helpers/dblclick';
import { answerPrompt } from './helpers/dialog';

const PHOTO_PATH = path.join(import.meta.dirname, 'fixtures', 'sample-photo.jpg');
const PDF_PATH = path.join(import.meta.dirname, 'fixtures', 'sample.pdf');
const TEXT_PATH = path.join(import.meta.dirname, 'fixtures', 'sample.txt');
const VIDEO_PATH = path.join(import.meta.dirname, 'fixtures', 'sample-video.webm');
const DOCX_PATH = path.join(import.meta.dirname, 'fixtures', 'sample.docx');
const XLSX_PATH = path.join(import.meta.dirname, 'fixtures', 'sample.xlsx');

test('opening an image file shows an inline preview', async ({ page }) => {
	await page.goto('/');
	await page.locator('input[type="file"]').setInputFiles(PHOTO_PATH);

	const row = page.locator('.item-row', { hasText: 'sample-photo.jpg' });
	await expect(row).toBeVisible({ timeout: 15_000 });
	await row.dblclick();

	await expect(page).toHaveURL(/\/file\/.+/);
	const img = page.locator('.preview-frame img');
	await expect(img).toBeVisible();
	// A real, non-empty image actually loaded — not just an <img> tag with a
	// broken src (naturalWidth stays 0 if decoding failed).
	await expect
		.poll(async () => img.evaluate((el: HTMLImageElement) => el.naturalWidth))
		.toBeGreaterThan(0);
});

test('opening a PDF renders it inline via canvas', async ({ page }) => {
	await page.goto('/');
	await page.locator('input[type="file"]').setInputFiles(PDF_PATH);

	const row = page.locator('.item-row', { hasText: 'sample.pdf' });
	await expect(row).toBeVisible({ timeout: 15_000 });
	await row.dblclick();

	await expect(page).toHaveURL(/\/file\/.+/);
	// pdf.js renders every page to its own <canvas> (see PdfViewer.svelte) —
	// not an <iframe src="blob:...">, which doesn't reliably show anything
	// on Android Chrome despite working fine in every desktop/E2E check,
	// which is exactly what made this worth switching away from.
	const canvas = page.locator('.pdf-pages canvas.pdf-page').first();
	await expect(canvas).toBeVisible({ timeout: 10_000 });

	// The real assertion: actual pixels were drawn, not just an empty
	// canvas element sitting there — a blank-but-present canvas is exactly
	// the failure mode a broken renderer would produce, and `toBeVisible`
	// alone can't tell the two apart.
	const hasContent = await canvas.evaluate((el: HTMLCanvasElement) => {
		const ctx = el.getContext('2d');
		if (!ctx) return false;
		const { data } = ctx.getImageData(0, 0, el.width, el.height);
		for (let i = 0; i < data.length; i += 4) {
			if (data[i] !== 255 || data[i + 1] !== 255 || data[i + 2] !== 255) return true;
		}
		return false;
	});
	expect(hasContent).toBe(true);
});

test('opening a video streams it via a content-token URL, not a full blob download', async ({ page }) => {
	await page.goto('/');
	await page.locator('input[type="file"]').setInputFiles(VIDEO_PATH);

	const row = page.locator('.item-row', { hasText: 'sample-video.webm' });
	await expect(row).toBeVisible({ timeout: 15_000 });
	await row.dblclick();

	await expect(page).toHaveURL(/\/file\/.+/);
	const video = page.locator('.preview-frame video');
	await expect(video).toBeVisible();

	// A real, direct element src carrying a short-lived content token — not
	// a blob: URL — is the whole point (see api.ts's getContentToken):
	// only a direct src, not a pre-fetched blob, gets real HTTP Range
	// streaming from the browser's own <video> implementation.
	const src = await video.getAttribute('src');
	expect(src).toMatch(/^\/api\/v1\/items\/.+\/content\?token=/);

	// And it actually decoded real media over that URL — not just an
	// element with a plausible-looking src that silently failed to load.
	await expect
		.poll(async () => video.evaluate((el: HTMLVideoElement) => el.readyState))
		.toBeGreaterThanOrEqual(1); // HAVE_METADATA — duration/dimensions are known
	await expect
		.poll(async () => video.evaluate((el: HTMLVideoElement) => el.videoWidth))
		.toBeGreaterThan(0);
});

test('opening a Word document renders its real text content', async ({ page }) => {
	await page.goto('/');
	await page.locator('input[type="file"]').setInputFiles(DOCX_PATH);

	const row = page.locator('.item-row', { hasText: 'sample.docx' });
	await expect(row).toBeVisible({ timeout: 15_000 });
	await row.dblclick();

	await expect(page).toHaveURL(/\/file\/.+/);
	// The real assertion: actual document text made it into the rendered
	// DOM, not just that a container element appeared — docx-preview
	// parses the .docx (a zip of XML) and rebuilds it as HTML, so this only
	// passes if that whole pipeline actually ran.
	await expect(page.locator('.docx-container')).toContainText('Fixture Word document');
	await expect(page.locator('.docx-container')).toContainText('Used by preview E2E tests.');
});

test('opening a spreadsheet renders real cell values and switches sheets', async ({ page }) => {
	await page.goto('/');
	await page.locator('input[type="file"]').setInputFiles(XLSX_PATH);

	const row = page.locator('.item-row', { hasText: 'sample.xlsx' });
	await expect(row).toBeVisible({ timeout: 15_000 });
	await openViaDblclick(page, row, /\/file\/.+/);

	// Real parsed cell values from the fixture's first sheet ("Results"),
	// not just an empty table shell.
	const table = page.locator('.xlsx-table');
	await expect(table).toContainText('Alice');
	await expect(table).toContainText('92');
	await expect(table).toContainText('Bob');

	// Switching sheets actually re-reads the workbook rather than always
	// showing the first one.
	await page.getByRole('button', { name: 'Notes', exact: true }).click();
	await expect(table).toContainText('Second sheet');
	await expect(table).not.toContainText('Alice');
});

test('opening a text file shows its content, and back returns to the same folder', async ({ page }) => {
	await page.goto('/');

	const folderName = `E2E Preview Folder ${Date.now()}`;
	await page.getByRole('button', { name: '+ New' }).click();
	await page.getByRole('menuitem', { name: 'New folder' }).click();
	await answerPrompt(page, folderName);
	await page.locator('.item-row', { hasText: folderName }).dblclick();
	await expect(page).toHaveURL(/folder=/);

	await page.locator('input[type="file"]').setInputFiles(TEXT_PATH);
	const row = page.locator('.item-row', { hasText: 'sample.txt' });
	await expect(row).toBeVisible({ timeout: 15_000 });
	await row.dblclick();

	await expect(page).toHaveURL(/\/file\/.+from=/);
	const expectedText = readFileSync(TEXT_PATH, 'utf8');
	await expect(page.locator('.preview-text')).toHaveText(expectedText);

	// The whole point of fullscreen mode (lib/fullscreen.ts): the app's own
	// nav chrome gets out of the way while looking at a file, and comes
	// back the moment you leave.
	await expect(page.locator('.topbar')).toBeHidden();

	await page.getByRole('link', { name: 'Back' }).click();
	await expect(page).toHaveURL(/folder=/);
	await expect(page.locator('.breadcrumb').getByText(folderName)).toBeVisible();
	await expect(page.locator('.topbar')).toBeVisible();
});

test('a file type without preview support falls back to a download prompt', async ({ page }) => {
	await page.goto('/');

	// Any mime type this app doesn't preview does the job here — a few
	// unrecognized bytes with a .bin extension is the simplest way to hit
	// the "unsupported" branch deliberately.
	const binPath = path.join(import.meta.dirname, 'fixtures', 'unsupported.bin');
	await page.locator('input[type="file"]').setInputFiles(binPath);
	const row = page.locator('.item-row', { hasText: 'unsupported.bin' });
	await expect(row).toBeVisible({ timeout: 15_000 });
	await openViaDblclick(page, row, /\/file\/.+/);

	await expect(page.getByText("Preview isn't available for this file type yet.")).toBeVisible();
	// Download now lives behind the page's own "⋮" action menu (the same
	// one the file list's rows use), not a directly-visible button.
	await page.getByRole('button', { name: 'Actions for unsupported.bin' }).click();
	await expect(page.locator('.dropdown-menu').getByRole('menuitem', { name: 'Download' })).toBeVisible();
});
