import { test, expect } from '@playwright/test';
import path from 'path';
import { readFileSync } from 'fs';

const FIXTURE_PATH = path.join(import.meta.dirname, 'fixtures', 'sample.txt');

test('share a file, a visitor with no account can fetch it, then revoking the link cuts them off', async ({
	page
}) => {
	await page.goto('/');
	await page.locator('input[type="file"]').setInputFiles(FIXTURE_PATH);

	const fileRow = page.locator('.item-row', { hasText: 'sample.txt' });
	await expect(fileRow).toBeVisible({ timeout: 15_000 });

	await fileRow.getByRole('button', { name: 'Actions for' }).click();
	await fileRow.locator('.dropdown-menu').getByRole('menuitem', { name: 'Share' }).click();
	await page.getByRole('button', { name: 'Create link' }).click();

	const linkInput = page.locator('dialog input[readonly]');
	await expect(linkInput).toBeVisible();
	const shareUrl = await linkInput.inputValue();
	expect(shareUrl).toContain('/s/');

	await page.getByRole('button', { name: 'Done' }).click();

	// page.request never attaches this app's Authorization header (that's
	// added by our own fetch wrapper in lib/api.ts, not something a browser
	// does on its own) — it's naturally the same as a visitor with no
	// Denizen account at all, exactly who these public /s/ routes are for.
	const metaRes = await page.request.get(shareUrl);
	expect(metaRes.ok()).toBeTruthy();
	const meta = await metaRes.json();
	expect(meta).toMatchObject({ name: 'sample.txt', type: 'file' });

	const contentRes = await page.request.get(`${shareUrl}/content`);
	expect(contentRes.ok()).toBeTruthy();
	const downloadedBody = await contentRes.body();
	expect(downloadedBody.equals(readFileSync(FIXTURE_PATH))).toBe(true);

	// --- revoking it from "My shares" actually cuts the visitor off ------------
	await page.goto('/shares');
	const shareRow = page.locator('.item-row', { hasText: 'Public' });
	await expect(shareRow).toBeVisible();

	page.once('dialog', (dialog) => dialog.accept());
	await shareRow.getByRole('button', { name: 'Revoke' }).click();
	await expect(shareRow).not.toBeVisible();

	const afterRevokeRes = await page.request.get(shareUrl);
	expect(afterRevokeRes.status()).toBe(404);
});
