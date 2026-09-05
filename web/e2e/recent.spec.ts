import { test, expect } from '@playwright/test';
import { answerPrompt } from './helpers/dialog';

// Flat "recently modified files, whole drive" list — routes/recent/
// +page.svelte, backend GET /api/v1/recent (ItemService.ListRecent).

test('shows uploaded files across folders sorted by recency, opens one, folders never appear', async ({
	page
}) => {
	await page.goto('/');

	const folderName = `E2E Recent Folder ${Date.now()}`;
	await page.getByRole('button', { name: '+ New' }).click();
	await page.getByRole('menuitem', { name: 'New folder' }).click();
	await answerPrompt(page, folderName);
	const folderRow = page.locator('.item-row', { hasText: folderName });
	await expect(folderRow).toBeVisible();

	// A root-level file, then a nested one inside the folder just created —
	// Recent has to reach across both, unlike the main browser's own
	// per-folder listing.
	const rootFile = `recent-root-${Date.now()}.txt`;
	await page.locator('input[type="file"]').setInputFiles({
		name: rootFile,
		mimeType: 'text/plain',
		buffer: Buffer.from('root-level file for the Recent view')
	});
	await expect(page.locator('.item-row', { hasText: rootFile })).toBeVisible({ timeout: 15_000 });

	await folderRow.locator('.item-name').dblclick();
	await expect(page.locator('.breadcrumb').getByText(folderName)).toBeVisible();
	// updated_at is second-resolution (see recent_flow_test.go's own
	// comment on the same issue) — without a real gap past that boundary,
	// this and the previous upload could land on the same second and the
	// ordering assertion below would be testing nothing.
	await page.waitForTimeout(1100);
	const nestedFile = `recent-nested-${Date.now()}.txt`;
	await page.locator('input[type="file"]').setInputFiles({
		name: nestedFile,
		mimeType: 'text/plain',
		buffer: Buffer.from('nested file, uploaded after the root one')
	});
	await expect(page.locator('.item-row', { hasText: nestedFile })).toBeVisible({ timeout: 15_000 });

	await page.getByRole('link', { name: 'Recent' }).click();
	await expect(page).toHaveURL(/\/recent$/);

	const nestedRow = page.locator('.item-row', { hasText: nestedFile });
	const rootRow = page.locator('.item-row', { hasText: rootFile });
	await expect(nestedRow).toBeVisible();
	await expect(rootRow).toBeVisible();
	await expect(page.locator('.item-row', { hasText: folderName })).toHaveCount(0);

	// Most recently modified first: the nested file, uploaded second,
	// appears above the root one uploaded first.
	const names = await page.locator('.item-row .item-name').allTextContents();
	expect(names.indexOf(nestedFile)).toBeLessThan(names.indexOf(rootFile));

	await nestedRow.locator('.item-name').click();
	await expect(page).toHaveURL(/\/file\//);
	await expect(page.getByText(nestedFile)).toBeVisible();

	// Back returns to Recent itself, not the root or the nested folder.
	await page.getByRole('link', { name: 'Back' }).click();
	await expect(page).toHaveURL(/\/recent$/);
});
