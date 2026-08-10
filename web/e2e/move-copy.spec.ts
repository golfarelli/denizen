import { test, expect } from '@playwright/test';
import path from 'path';

const FIXTURE_PATH = path.join(import.meta.dirname, 'fixtures', 'sample.txt');

test('move a folder into another folder via the destination picker', async ({ page }) => {
	await page.goto('/');

	const destName = `E2E Move Dest ${Date.now()}`;
	page.once('dialog', (dialog) => dialog.accept(destName));
	await page.getByRole('button', { name: '+ New folder' }).click();
	await expect(page.locator('.item-row', { hasText: destName })).toBeVisible();

	const sourceName = `E2E Move Source ${Date.now()}`;
	page.once('dialog', (dialog) => dialog.accept(sourceName));
	await page.getByRole('button', { name: '+ New folder' }).click();
	const sourceRow = page.locator('.item-row', { hasText: sourceName });
	await expect(sourceRow).toBeVisible();

	await sourceRow.getByRole('button', { name: 'Actions for' }).click();
	await sourceRow.locator('.dropdown-menu').getByRole('menuitem', { name: 'Move' }).click();

	// ShareDialog's own <dialog> is always present in the DOM too (just
	// closed), so plain `dialog.card` matches both — [open] narrows to the
	// one actually showing.
	const dialog = page.locator('dialog.card[open]');
	await expect(dialog).toBeVisible();
	await expect(dialog.getByRole('heading')).toHaveText(`Move "${sourceName}"`);

	// Navigate the picker into the destination folder (the item being moved
	// itself is filtered out of the list — see MoveDialog.svelte), then
	// confirm.
	await dialog
		.locator('.item-row', { hasText: destName })
		.getByRole('button', { name: destName, exact: true })
		.click();
	await expect(dialog.getByText('No subfolders here.')).toBeVisible();

	await dialog.getByRole('button', { name: 'Move here' }).click();
	await expect(dialog).not.toBeVisible();

	// Gone from the root listing...
	await expect(page.locator('.item-row', { hasText: sourceName })).not.toBeVisible();

	// ...and actually inside the destination folder now, not just hidden.
	await page.locator('.item-row', { hasText: destName }).click();
	await expect(page.locator('.item-row', { hasText: sourceName })).toBeVisible();
});

test('make a copy of a file lands in the same folder with an auto-suffixed name', async ({ page }) => {
	await page.goto('/');
	await page.locator('input[type="file"]').setInputFiles(FIXTURE_PATH);

	const original = page.locator('.item-row', { hasText: 'sample.txt' });
	await expect(original).toBeVisible({ timeout: 15_000 });

	await original.getByRole('button', { name: 'Actions for' }).click();
	await original.locator('.dropdown-menu').getByRole('menuitem', { name: 'Make a copy' }).click();

	// "sample (1).txt" doesn't contain "sample.txt" as a substring, so this
	// stays a distinct match from the original row above rather than also
	// matching it.
	const copy = page.locator('.item-row', { hasText: 'sample (1).txt' });
	await expect(copy).toBeVisible();
	await expect(page.locator('.item-row', { hasText: 'sample.txt' })).toHaveCount(1);
});
