import { test, expect } from '@playwright/test';
import path from 'path';
import { readFileSync } from 'fs';

const PHOTO_PATH = path.join(import.meta.dirname, 'fixtures', 'sample-photo.jpg');
const PDF_PATH = path.join(import.meta.dirname, 'fixtures', 'sample.pdf');
const TEXT_PATH = path.join(import.meta.dirname, 'fixtures', 'sample.txt');

test('opening an image file shows an inline preview', async ({ page }) => {
	await page.goto('/');
	await page.locator('input[type="file"]').setInputFiles(PHOTO_PATH);

	const row = page.locator('.item-row', { hasText: 'sample-photo.jpg' });
	await expect(row).toBeVisible({ timeout: 15_000 });
	await row.getByRole('button', { name: 'sample-photo.jpg', exact: true }).click();

	await expect(page).toHaveURL(/\/file\/.+/);
	const img = page.locator('.preview-frame img');
	await expect(img).toBeVisible();
	// A real, non-empty image actually loaded — not just an <img> tag with a
	// broken src (naturalWidth stays 0 if decoding failed).
	await expect
		.poll(async () => img.evaluate((el: HTMLImageElement) => el.naturalWidth))
		.toBeGreaterThan(0);
});

test('opening a PDF shows it in an inline viewer', async ({ page }) => {
	await page.goto('/');
	await page.locator('input[type="file"]').setInputFiles(PDF_PATH);

	const row = page.locator('.item-row', { hasText: 'sample.pdf' });
	await expect(row).toBeVisible({ timeout: 15_000 });
	await row.getByRole('button', { name: 'sample.pdf', exact: true }).click();

	await expect(page).toHaveURL(/\/file\/.+/);
	const frame = page.locator('iframe.preview-pdf');
	await expect(frame).toBeVisible();
	await expect(frame).toHaveAttribute('src', /^blob:/);
});

test('opening a text file shows its content, and back returns to the same folder', async ({ page }) => {
	await page.goto('/');

	const folderName = `E2E Preview Folder ${Date.now()}`;
	page.once('dialog', (dialog) => dialog.accept(folderName));
	await page.getByRole('button', { name: '+ New folder' }).click();
	await page
		.locator('.item-row', { hasText: folderName })
		.getByRole('button', { name: folderName, exact: true })
		.click();
	await expect(page).toHaveURL(/folder=/);

	await page.locator('input[type="file"]').setInputFiles(TEXT_PATH);
	const row = page.locator('.item-row', { hasText: 'sample.txt' });
	await expect(row).toBeVisible({ timeout: 15_000 });
	await row.getByRole('button', { name: 'sample.txt', exact: true }).click();

	await expect(page).toHaveURL(/\/file\/.+from=/);
	const expectedText = readFileSync(TEXT_PATH, 'utf8');
	await expect(page.locator('.preview-text')).toHaveText(expectedText);

	await page.getByRole('link', { name: '← Back' }).click();
	await expect(page).toHaveURL(/folder=/);
	await expect(page.locator('.breadcrumb').getByText(folderName)).toBeVisible();
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
	await row.getByRole('button', { name: 'unsupported.bin', exact: true }).click();

	await expect(page).toHaveURL(/\/file\/.+/);
	await expect(page.getByText("Preview isn't available for this file type yet.")).toBeVisible();
	await expect(page.getByRole('button', { name: 'Download' })).toBeVisible();
});
