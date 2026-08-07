import { test, expect } from '@playwright/test';

// Grid/list toggle and column sorting — routes/+page.svelte
// (lib/viewMode.ts, lib/sortItems.ts).

test('switching to grid view shows tiles and persists across a reload', async ({ page }) => {
	await page.goto('/');

	const folderName = `E2E Grid ${Date.now()}`;
	page.once('dialog', (dialog) => dialog.accept(folderName));
	await page.getByRole('button', { name: '+ New folder' }).click();
	await expect(page.locator('.item-row', { hasText: folderName })).toBeVisible();

	await expect(page.locator('.item-list')).toBeVisible();
	await expect(page.locator('.item-grid')).toHaveCount(0);

	await page.getByRole('button', { name: 'Grid view' }).click();

	await expect(page.locator('.item-grid')).toBeVisible();
	await expect(page.locator('.item-list')).toHaveCount(0);
	await expect(page.locator('.item-tile', { hasText: folderName })).toBeVisible();

	// Persisted (localStorage), unlike sort — a reload should still be grid.
	await page.reload();
	await expect(page.locator('.item-grid')).toBeVisible();

	// A tile's own kebab menu works the same as a list row's.
	const tile = page.locator('.item-tile', { hasText: folderName });
	await tile.getByRole('button', { name: 'Actions for' }).click();
	await expect(tile.locator('.dropdown-menu')).toBeVisible();
	await expect(tile.locator('.dropdown-menu').getByRole('menuitem', { name: 'Rename' })).toBeVisible();

	// Opening a folder still works by clicking the tile itself.
	await page.keyboard.press('Escape');
	await page.locator('.item-tile-main', { hasText: folderName }).click();
	await expect(page.locator('.breadcrumb').getByText(folderName)).toBeVisible();

	// Back to list view for the next test in this file.
	await page.getByRole('button', { name: 'List view' }).click();
});

test('clicking a column header sorts the list, and clicking again reverses it', async ({ page }) => {
	await page.goto('/');
	await expect(page.getByRole('button', { name: 'List view' })).toHaveAttribute('aria-pressed', 'true');

	const stamp = Date.now();
	const names = [`b-e2e-sort-${stamp}`, `a-e2e-sort-${stamp}`, `c-e2e-sort-${stamp}`];
	for (const name of names) {
		page.once('dialog', (dialog) => dialog.accept(name));
		await page.getByRole('button', { name: '+ New folder' }).click();
		await expect(page.locator('.item-row', { hasText: name })).toBeVisible();
	}

	// Default sort is name/ascending — the three new folders should already
	// read a, b, c top-to-bottom among themselves.
	const rowNames = () => page.locator('.item-row .item-name').allTextContents();
	const orderOf = (all: string[]) => all.filter((n) => names.includes(n));
	await expect.poll(async () => orderOf(await rowNames())).toEqual([names[1], names[0], names[2]]);

	// Click "Name" again to reverse to descending. exact: true, since the
	// shared test folder accumulates rows like "E2E After Rename" whose own
	// name text also matches the header's { name: 'Name' } substring query.
	await page.getByRole('button', { name: 'Name', exact: true }).click();
	await expect.poll(async () => orderOf(await rowNames())).toEqual([names[2], names[0], names[1]]);
});
