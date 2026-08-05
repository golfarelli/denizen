import { test, expect } from '@playwright/test';
import path from 'path';
import { createHash } from 'crypto';
import { readFileSync } from 'fs';

const FIXTURE_PATH = path.join(import.meta.dirname, 'fixtures', 'sample.txt');

test('upload a file and download it back byte-for-byte', async ({ page }) => {
	await page.goto('/');

	// The upload input is a real (if visually hidden) <input type="file"> —
	// setInputFiles works on it directly without needing to click the
	// "+ Upload" button first (which would only open a native OS file
	// picker Playwright can't drive anyway; this is the standard way
	// Playwright feeds files into a page under test).
	await page.locator('input[type="file"]').setInputFiles(FIXTURE_PATH);

	// onSuccess refreshes the listing (see routes/+page.svelte) — wait for
	// the file to actually show up there, the same signal a real person
	// would be watching for, rather than the upload panel's own "Done" label.
	const fileRow = page.locator('.item-row', { hasText: 'sample.txt' });
	await expect(fileRow).toBeVisible({ timeout: 15_000 });

	const downloadPromise = page.waitForEvent('download');
	await fileRow.getByRole('button', { name: 'Actions for' }).click();
	await fileRow.locator('.dropdown-menu').getByRole('menuitem', { name: 'Download' }).click();
	const download = await downloadPromise;

	const downloadedPath = await download.path();
	if (!downloadedPath) throw new Error('download did not produce a local file path');

	// The real assertion this test exists for: the round-tripped file is
	// byte-for-byte identical to what was uploaded — the browser-side
	// counterpart to test/flow/upload_flow_test.go's own comparison, done
	// over a real HTTP upload and a real HTTP download instead of Go's
	// net/http/httptest.
	const original = readFileSync(FIXTURE_PATH);
	const downloaded = readFileSync(downloadedPath);
	expect(sha256(downloaded)).toBe(sha256(original));
});

function sha256(data: Buffer): string {
	return createHash('sha256').update(data).digest('hex');
}
