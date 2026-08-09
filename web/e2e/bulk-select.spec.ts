import { test, expect } from '@playwright/test';

function uniqueName(base: string): string {
	return `${base}-${Date.now()}-${Math.floor(Math.random() * 1e6)}.txt`;
}

test('selecting rows shows the bulk toolbar with the right count, and "select all" toggles everything', async ({
	page
}) => {
	await page.goto('/');

	const nameA = uniqueName('bulk-a');
	const nameB = uniqueName('bulk-b');
	await page.locator('input[type="file"]').setInputFiles([
		{ name: nameA, mimeType: 'text/plain', buffer: Buffer.from('a') },
		{ name: nameB, mimeType: 'text/plain', buffer: Buffer.from('b') }
	]);
	const rowA = page.locator('.item-row', { hasText: nameA });
	const rowB = page.locator('.item-row', { hasText: nameB });
	await expect(rowA).toBeVisible({ timeout: 15_000 });
	await expect(rowB).toBeVisible({ timeout: 15_000 });

	await expect(page.locator('.selection-toolbar')).toHaveCount(0);

	await rowA.locator('.row-checkbox').click();
	await expect(page.locator('.selection-toolbar')).toContainText('1 selected');

	await rowB.locator('.row-checkbox').click();
	await expect(page.locator('.selection-toolbar')).toContainText('2 selected');

	// "Select all" / "Deselect all" toggles the header checkbox and every row.
	await page.locator('.item-list-header .item-icon input[type="checkbox"]').click();
	await expect(rowA.locator('.row-checkbox')).not.toBeChecked();
	await expect(rowB.locator('.row-checkbox')).not.toBeChecked();
	await expect(page.locator('.selection-toolbar')).toHaveCount(0);
});

test('long-pressing an item selects it instead of opening it; a normal click still opens it', async ({ page }) => {
	await page.goto('/');

	const folderName = `E2E Long Press ${Date.now()}`;
	page.once('dialog', (dialog) => dialog.accept(folderName));
	await page.getByRole('button', { name: '+ New folder' }).click();
	const row = page.locator('.item-row', { hasText: folderName });
	await expect(row).toBeVisible();

	// A quick click (well under the 500ms hold threshold) still opens it
	// normally — long-press-to-select must not get in the way of the
	// ordinary open gesture.
	await row.locator('.item-name').click();
	await expect(page).toHaveURL(/\?folder=/);
	await page.goto('/');

	// Holding past the threshold selects it instead of navigating —
	// Playwright's click `delay` holds the mouse down for that long
	// before releasing, which fires the same pointerdown/pointerup pair a
	// touch long-press would (see routes/+page.svelte's startLongPress).
	await row.locator('.item-name').click({ delay: 600 });
	await expect(page).toHaveURL('/'); // never navigated away
	await expect(page.locator('.selection-toolbar')).toContainText('1 selected');
	await expect(row.locator('.row-checkbox')).toBeChecked();

	// Selection mode is now active — an ordinary tap on a second item
	// adds to the selection instead of opening it too.
	const secondFolderName = `E2E Long Press Second ${Date.now()}`;
	page.once('dialog', (dialog) => dialog.accept(secondFolderName));
	// Deselect first so "+ New folder" is reachable (the selection
	// toolbar sits where it'd otherwise be covered — this just confirms
	// state, not a real workflow step).
	await row.locator('.row-checkbox').click();
	await page.getByRole('button', { name: '+ New folder' }).click();
	const secondRow = page.locator('.item-row', { hasText: secondFolderName });
	await expect(secondRow).toBeVisible();

	await row.locator('.item-name').click({ delay: 600 });
	await secondRow.locator('.item-name').click();
	await expect(page).toHaveURL('/');
	await expect(page.locator('.selection-toolbar')).toContainText('2 selected');
});

test('the bulk Share button opens a dialog scoped to the whole selection', async ({ page }) => {
	await page.goto('/');

	const nameA = uniqueName('bulk-share-a');
	const nameB = uniqueName('bulk-share-b');
	await page.locator('input[type="file"]').setInputFiles([
		{ name: nameA, mimeType: 'text/plain', buffer: Buffer.from('a') },
		{ name: nameB, mimeType: 'text/plain', buffer: Buffer.from('b') }
	]);
	await expect(page.locator('.item-row', { hasText: nameB })).toBeVisible({ timeout: 15_000 });

	await page.locator('.item-row', { hasText: nameA }).locator('.row-checkbox').click();
	await page.locator('.item-row', { hasText: nameB }).locator('.row-checkbox').click();
	await page.locator('.selection-toolbar').getByRole('button', { name: 'Share' }).click();

	const dialog = page.locator('dialog.card[open]');
	await expect(dialog.getByRole('heading')).toHaveText('Share 2 items');
	await dialog.getByRole('button', { name: 'Cancel' }).click();
	await expect(dialog).not.toBeVisible();

	// Cancelling shares nothing — the selection itself is untouched too.
	await expect(page.locator('.selection-toolbar')).toContainText('2 selected');
});

test('bulk delete trashes every selected item, bulk move relocates every selected item together', async ({
	page
}) => {
	await page.goto('/');

	const destName = `Bulk Move Dest ${Date.now()}`;
	page.once('dialog', (dialog) => dialog.accept(destName));
	await page.getByRole('button', { name: '+ New folder' }).click();
	await expect(page.locator('.item-row', { hasText: destName })).toBeVisible();

	const toDelete = uniqueName('to-delete');
	const toMoveA = uniqueName('to-move-a');
	const toMoveB = uniqueName('to-move-b');
	await page.locator('input[type="file"]').setInputFiles([
		{ name: toDelete, mimeType: 'text/plain', buffer: Buffer.from('x') },
		{ name: toMoveA, mimeType: 'text/plain', buffer: Buffer.from('y') },
		{ name: toMoveB, mimeType: 'text/plain', buffer: Buffer.from('z') }
	]);
	await expect(page.locator('.item-row', { hasText: toMoveB })).toBeVisible({ timeout: 15_000 });

	// --- bulk delete -----------------------------------------------------------------
	await page.locator('.item-row', { hasText: toDelete }).locator('.row-checkbox').click();
	page.once('dialog', (dialog) => dialog.accept());
	await page.locator('.selection-toolbar').getByRole('button', { name: 'Delete' }).click();
	await expect(page.locator('.item-row', { hasText: toDelete })).toHaveCount(0);
	await expect(page.locator('.selection-toolbar')).toHaveCount(0); // selection clears after the bulk action

	// --- bulk move ---------------------------------------------------------------------
	await page.locator('.item-row', { hasText: toMoveA }).locator('.row-checkbox').click();
	await page.locator('.item-row', { hasText: toMoveB }).locator('.row-checkbox').click();
	await page.locator('.selection-toolbar').getByRole('button', { name: 'Move' }).click();

	const dialog = page.locator('dialog.card[open]');
	await expect(dialog.getByRole('heading')).toHaveText('Move 2 items');
	await dialog
		.locator('.item-row', { hasText: destName })
		.getByRole('button', { name: destName, exact: true })
		.click();
	await dialog.getByRole('button', { name: 'Move here' }).click();
	await expect(dialog).not.toBeVisible();

	await expect(page.locator('.item-row', { hasText: toMoveA })).toHaveCount(0);
	await expect(page.locator('.item-row', { hasText: toMoveB })).toHaveCount(0);

	await page
		.locator('.item-row', { hasText: destName })
		.getByRole('button', { name: destName, exact: true })
		.click();
	await expect(page.locator('.item-row', { hasText: toMoveA })).toBeVisible();
	await expect(page.locator('.item-row', { hasText: toMoveB })).toBeVisible();
});
