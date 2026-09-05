import { test, expect } from '@playwright/test';
import path from 'path';

const PHOTO_PATH = path.join(import.meta.dirname, 'fixtures', 'sample-photo.jpg');

test('scan two pages and upload as one multi-page PDF', async ({ page }) => {
	await page.goto('/');

	await page.getByRole('button', { name: '+ New' }).click();
	await page.getByRole('menuitem', { name: 'Scan' }).click();
	const dialog = page.locator('dialog.card[open]');
	await expect(dialog).toBeVisible();

	// The `capture` attribute just tells the OS which native UI to prefer —
	// from the page's own perspective it's an ordinary <input type="file">,
	// so setInputFiles drives it exactly like a real camera capture would
	// hand back a photo. "+ Add page" re-triggers the same hidden input for
	// each page, one at a time, same as a real person tapping it twice.
	await dialog.getByRole('button', { name: '+ Add page' }).click();
	await dialog.locator('input[type="file"]').setInputFiles(PHOTO_PATH);
	await expect(dialog.locator('.scan-page-thumb')).toHaveCount(1);

	await dialog.getByRole('button', { name: '+ Add page' }).click();
	await dialog.locator('input[type="file"]').setInputFiles(PHOTO_PATH);
	await expect(dialog.locator('.scan-page-thumb')).toHaveCount(2);

	await dialog.getByRole('button', { name: 'Save as PDF (2)' }).click();
	await expect(dialog).not.toBeVisible();

	// Uploaded like any other file, through the same progress-tracked path
	// — waiting for it to land in the listing is the same signal every
	// other upload test in this suite waits for.
	// Not anchored: the row's text content is icon + name + size concatenated
	// (e.g. "📄Scan 2026-....pdf12.3 KB"), so `^Scan` would never match.
	const fileRow = page.locator('.item-row', { hasText: /Scan .*\.pdf/ });
	await expect(fileRow).toBeVisible({ timeout: 15_000 });

	// Download it back and check it's a real, structurally valid multi-page
	// PDF — not a full parser (consistent with this app's own PDF writer
	// being a deliberately minimal hand-rolled subset, see lib/pdf.ts), but
	// enough to catch a genuinely broken file: the format's own magic
	// header/footer, and one /Type /Page object per page actually scanned.
	await fileRow.getByRole('button', { name: 'Actions for' }).click();
	const downloadPromise = page.waitForEvent('download');
	await fileRow.locator('.dropdown-menu').getByRole('menuitem', { name: 'Download' }).click();
	const download = await downloadPromise;
	const downloadedPath = await download.path();
	if (!downloadedPath) throw new Error('download did not produce a local file path');

	const fs = await import('fs');
	const bytes = fs.readFileSync(downloadedPath);
	const text = bytes.toString('latin1'); // byte-preserving enough to find ASCII markers around binary JPEG data

	expect(text.startsWith('%PDF-1.4')).toBe(true);
	expect(text.trimEnd().endsWith('%%EOF')).toBe(true);
	expect(text.match(/\/Type\s*\/Page[^s]/g)?.length).toBe(2);
	expect(text).toContain('/Filter /DCTDecode');
});
