import { test, expect } from '@playwright/test';
import path from 'path';

// Real image/PDF previews in grid view (lib/Thumbnail.svelte, backend:
// internal/thumbnail + GET /api/v1/items/{id}/thumbnail) instead of the
// generic per-type FileIcon glyph every tile used to show.

const PHOTO_PATH = path.join(import.meta.dirname, 'fixtures', 'sample-photo.jpg');
const PDF_PATH = path.join(import.meta.dirname, 'fixtures', 'sample.pdf');
const TXT_PATH = path.join(import.meta.dirname, 'fixtures', 'sample.txt');

test('grid view shows a real thumbnail image for a photo, not the generic icon', async ({ page }) => {
	await page.goto('/');
	await page.locator('input[type="file"]').setInputFiles(PHOTO_PATH);
	await expect(page.locator('.item-row', { hasText: 'sample-photo.jpg' })).toBeVisible({ timeout: 15_000 });

	await page.getByRole('button', { name: 'Grid view' }).click();
	const tile = page.locator('.item-tile', { hasText: 'sample-photo.jpg' });
	await expect(tile).toBeVisible();

	const thumb = tile.locator('.item-tile-thumb');
	await expect(thumb).toBeVisible({ timeout: 10_000 });
	const src = await thumb.getAttribute('src');
	expect(src).toMatch(/^blob:/);

	// The generic colored-chip icon (see FileIcon.svelte) is what shows
	// before/instead of a real thumbnail — for this file it must be gone.
	await expect(tile.locator('.file-icon-chip')).toHaveCount(0);

	await page.getByRole('button', { name: 'List view' }).click();
});

test('grid view shows a real thumbnail for a PDF too', async ({ page }) => {
	await page.goto('/');
	await page.locator('input[type="file"]').setInputFiles(PDF_PATH);
	await expect(page.locator('.item-row', { hasText: 'sample.pdf' })).toBeVisible({ timeout: 15_000 });

	await page.getByRole('button', { name: 'Grid view' }).click();
	const tile = page.locator('.item-tile', { hasText: 'sample.pdf' });
	await expect(tile.locator('.item-tile-thumb')).toBeVisible({ timeout: 10_000 });

	await page.getByRole('button', { name: 'List view' }).click();
});

test('grid view falls back to the generic icon for a type with no thumbnail support', async ({ page }) => {
	await page.goto('/');
	await page.locator('input[type="file"]').setInputFiles(TXT_PATH);
	await expect(page.locator('.item-row', { hasText: 'sample.txt' })).toBeVisible({ timeout: 15_000 });

	await page.getByRole('button', { name: 'Grid view' }).click();
	const tile = page.locator('.item-tile', { hasText: 'sample.txt' });
	await expect(tile).toBeVisible();
	// Give a would-be thumbnail fetch a moment to (not) resolve before
	// asserting the fallback stayed put.
	await page.waitForTimeout(500);
	await expect(tile.locator('.item-tile-thumb')).toHaveCount(0);
	await expect(tile.locator('.file-icon-chip')).toBeVisible();

	await page.getByRole('button', { name: 'List view' }).click();
});
