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

// .item-list-header (the column headers sorting normally hangs off) is
// display:none below 640px (see app.css) — a real, pre-existing mobile
// layout choice, not a bug. Sorting still has to be reachable there
// though: lib/SortMenu.svelte is the fix, a toolbar button that works
// regardless of viewport width. Regression test for exactly that gap.
test('on a mobile-width viewport, sorting is reachable via the toolbar SortMenu, not just the (hidden) column headers', async ({
	page
}) => {
	await page.setViewportSize({ width: 390, height: 844 });
	await page.goto('/');

	const stamp = Date.now();
	const names = [`b-e2e-mobile-sort-${stamp}`, `a-e2e-mobile-sort-${stamp}`];
	for (const name of names) {
		page.once('dialog', (dialog) => dialog.accept(name));
		// The toolbar's own New folder/Upload/Scan buttons are hidden on
		// mobile (see app.css) in favor of the FAB — same reason this test
		// has to go through it instead of "+ New folder" directly.
		await page.locator('.fab').click();
		await page.getByRole('menuitem', { name: 'New folder' }).click();
		await expect(page.locator('.item-row', { hasText: name })).toBeVisible();
	}

	// Confirm the premise: the column headers really are unreachable here.
	await expect(page.locator('.item-list-header')).not.toBeVisible();

	const sortButton = page.getByRole('button', { name: 'Sort by' });
	await expect(sortButton).toBeVisible();
	await sortButton.click();

	const menu = page.locator('.dropdown-menu');
	await expect(menu).toBeVisible();
	await menu.getByRole('menuitem', { name: 'Name' }).click();

	const rowNames = () => page.locator('.item-row .item-name').allTextContents();
	const orderOf = (all: string[]) => all.filter((n) => names.includes(n));
	// Default sort is already name/ascending (a before b) — since "Name"
	// is already the active field, picking it from the menu toggles
	// direction the same way clicking an already-active header does, so
	// this first pick flips straight to descending (b before a).
	await expect.poll(async () => orderOf(await rowNames())).toEqual([names[0], names[1]]);

	// Picking it again flips back to ascending.
	await sortButton.click();
	await menu.getByRole('menuitem', { name: 'Name' }).click();
	await expect.poll(async () => orderOf(await rowNames())).toEqual([names[1], names[0]]);
});
