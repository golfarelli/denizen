import { test, expect } from '@playwright/test';

test('create a folder, navigate into it, and back via breadcrumb', async ({ page }) => {
	await page.goto('/');
	await expect(page.getByRole('heading', { name: 'Files' })).toBeVisible();

	const folderName = 'E2E Documents';
	// The "+ New folder" flow uses a native prompt() (see routes/+page.svelte)
	// — Playwright's dialog event is how a real browser lets a test answer one.
	page.once('dialog', (dialog) => dialog.accept(folderName));
	await page.getByRole('button', { name: '+ New folder' }).click();

	const folderRow = page.locator('.item-row', { hasText: folderName });
	await expect(folderRow).toBeVisible();

	await folderRow.getByRole('button', { name: folderName, exact: true }).click();

	await expect(page.locator('.breadcrumb').getByText(folderName)).toBeVisible();
	await expect(page.getByText('This folder is empty. Drop files here, or use "+ Upload".')).toBeVisible();

	await page.getByRole('button', { name: 'Home' }).click();
	await expect(folderRow).toBeVisible();
});
