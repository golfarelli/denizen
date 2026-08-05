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

test('rename a folder', async ({ page }) => {
	await page.goto('/');

	const originalName = 'E2E Before Rename';
	const renamedName = 'E2E After Rename';

	page.once('dialog', (dialog) => dialog.accept(originalName));
	await page.getByRole('button', { name: '+ New folder' }).click();

	const folderRow = page.locator('.item-row', { hasText: originalName });
	await expect(folderRow).toBeVisible();

	// The "Rename" flow also uses a native prompt() (see
	// routes/+page.svelte's handleRename), pre-filled with the current name.
	page.once('dialog', (dialog) => {
		expect(dialog.defaultValue()).toBe(originalName);
		dialog.accept(renamedName);
	});
	// exact: true matters here — without it, "Rename" as a substring also
	// matches the item-name button itself, since its accessible name
	// ("E2E Before Rename") happens to contain the word "Rename" too.
	await folderRow.getByRole('button', { name: 'Rename', exact: true }).click();

	await expect(page.locator('.item-row', { hasText: renamedName })).toBeVisible();
	await expect(page.locator('.item-row', { hasText: originalName })).not.toBeVisible();
});
