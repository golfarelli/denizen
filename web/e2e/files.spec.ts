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

	// Rename now lives behind the row's "⋮" action menu (see
	// routes/+page.svelte) rather than a flat button — open it first.
	await folderRow.getByRole('button', { name: 'Actions for' }).click();
	const menu = folderRow.locator('.dropdown-menu');
	await expect(menu).toBeVisible();

	// The "Rename" flow also uses a native prompt() (see
	// routes/+page.svelte's handleRename), pre-filled with the current name.
	page.once('dialog', (dialog) => {
		expect(dialog.defaultValue()).toBe(originalName);
		dialog.accept(renamedName);
	});
	await menu.getByRole('menuitem', { name: 'Rename' }).click();

	await expect(page.locator('.item-row', { hasText: renamedName })).toBeVisible();
	await expect(page.locator('.item-row', { hasText: originalName })).not.toBeVisible();
});

test('the row action menu closes on outside click', async ({ page }) => {
	await page.goto('/');

	const name = `E2E Menu Close ${Date.now()}`;
	page.once('dialog', (dialog) => dialog.accept(name));
	await page.getByRole('button', { name: '+ New folder' }).click();

	const row = page.locator('.item-row', { hasText: name });
	await expect(row).toBeVisible();

	await row.getByRole('button', { name: 'Actions for' }).click();
	const menu = row.locator('.dropdown-menu');
	await expect(menu).toBeVisible();

	await page.getByRole('heading', { name: 'Files' }).click();
	await expect(menu).not.toBeVisible();
});
