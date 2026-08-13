import { test, expect } from '@playwright/test';
import { registerSecondUser } from './helpers/secondUser';

test('advanced search: type filter narrows results and shows a removable chip', async ({ page }) => {
	await page.goto('/');

	const stamp = Date.now();
	// "advsearch-<stamp>" must be a genuine shared prefix of both names —
	// the query below matches on substring, so the type-specific suffix
	// has to come *after* it, not interleaved before the stamp.
	const folderName = `advsearch-${stamp}-folder`;
	const fileName = `advsearch-${stamp}-file.txt`;

	page.once('dialog', (dialog) => dialog.accept(folderName));
	await page.getByRole('button', { name: '+ New' }).click();
	await page.getByRole('menuitem', { name: 'New folder' }).click();
	await expect(page.locator('.item-row', { hasText: folderName })).toBeVisible();

	await page.locator('input[type="file"]').setInputFiles({
		name: fileName,
		mimeType: 'text/plain',
		buffer: Buffer.from('hello')
	});
	await expect(page.locator('.item-row', { hasText: fileName })).toBeVisible({ timeout: 15_000 });

	// A shared "advsearch-<stamp>" prefix matches both the folder and the
	// file — the filter is what has to tell them apart, not the query.
	const searchBox = page.getByLabel('Search files');
	await searchBox.fill(`advsearch-${stamp}`);
	await expect(page.locator('.item-row', { hasText: folderName })).toBeVisible({ timeout: 5_000 });
	await expect(page.locator('.item-row', { hasText: fileName })).toBeVisible();

	// --- open the modal, filter to Folders only -------------------------------------
	await page.getByRole('button', { name: 'Advanced search' }).click();
	const dialog = page.locator('dialog.card[open]');
	await expect(dialog.getByRole('heading', { name: 'Advanced search' })).toBeVisible();
	await dialog.locator('#search-filter-type').selectOption('folder');
	await dialog.getByRole('button', { name: 'Search', exact: true }).click();
	await expect(dialog).not.toBeVisible();

	await expect(page.locator('.item-row', { hasText: folderName })).toBeVisible();
	await expect(page.locator('.item-row', { hasText: fileName })).toHaveCount(0);
	await expect(page.locator('.hint')).toContainText('1');

	// --- the active filter shows as a chip, removable on its own --------------------
	const chip = page.locator('.chip', { hasText: 'Folders' });
	await expect(chip).toBeVisible();
	await chip.click();
	await expect(page.locator('.item-row', { hasText: fileName })).toBeVisible();
	await expect(page.locator('.search-filter-chips')).toHaveCount(0);
});

test('advanced search: owner filter (mine vs shared with me)', async ({ page, browser }) => {
	const { username: anna, page: annaPage } = await registerSecondUser(page, browser);

	await page.goto('/');
	const stamp = Date.now();
	const sharedName = `advsearch-owner-${stamp}-shared.txt`;
	await page.locator('input[type="file"]').setInputFiles({
		name: sharedName,
		mimeType: 'text/plain',
		buffer: Buffer.from('shared with anna')
	});
	const row = page.locator('.item-row', { hasText: sharedName });
	await expect(row).toBeVisible({ timeout: 15_000 });
	await row.getByRole('button', { name: 'Actions for' }).click();
	await row.locator('.dropdown-menu').getByRole('menuitem', { name: 'Share' }).click();
	const shareDialog = page.locator('dialog.card[open]');
	await shareDialog.locator('.share-person-select').selectOption({ label: anna });
	await shareDialog.locator('.share-people-add').getByRole('button', { name: 'Share', exact: true }).click();
	await shareDialog.getByRole('button', { name: 'Cancel' }).click();

	await annaPage.goto('/');
	const annaOwnName = `advsearch-owner-${stamp}-mine.txt`;
	await annaPage.locator('input[type="file"]').setInputFiles({
		name: annaOwnName,
		mimeType: 'text/plain',
		buffer: Buffer.from('anna owns this one')
	});
	await expect(annaPage.locator('.item-row', { hasText: annaOwnName })).toBeVisible({ timeout: 15_000 });

	const annaSearchBox = annaPage.getByLabel('Search files');
	await annaSearchBox.fill(`advsearch-owner-${stamp}`);
	await expect(annaPage.locator('.item-row', { hasText: annaOwnName })).toBeVisible({ timeout: 5_000 });
	await expect(annaPage.locator('.item-row', { hasText: sharedName })).toBeVisible();

	await annaPage.getByRole('button', { name: 'Advanced search' }).click();
	const annaDialog = annaPage.locator('dialog.card[open]');
	await annaDialog.locator('#search-filter-owner').selectOption('mine');
	await annaDialog.getByRole('button', { name: 'Search', exact: true }).click();

	await expect(annaPage.locator('.item-row', { hasText: annaOwnName })).toBeVisible();
	await expect(annaPage.locator('.item-row', { hasText: sharedName })).toHaveCount(0);

	// Flip it: only what's shared with her, not her own.
	await annaPage.locator('.chip', { hasText: 'Mine only' }).click();
	await annaPage.getByRole('button', { name: 'Advanced search' }).click();
	await annaDialog.locator('#search-filter-owner').selectOption('shared');
	await annaDialog.getByRole('button', { name: 'Search', exact: true }).click();
	await expect(annaPage.locator('.item-row', { hasText: sharedName })).toBeVisible();
	await expect(annaPage.locator('.item-row', { hasText: annaOwnName })).toHaveCount(0);
});

test('advanced search: a date filter that should match everything freshly created does not wrongly exclude it', async ({
	page
}) => {
	await page.goto('/');
	const name = `advsearch-today-${Date.now()}.txt`;
	await page.locator('input[type="file"]').setInputFiles({ name, mimeType: 'text/plain', buffer: Buffer.from('x') });
	await expect(page.locator('.item-row', { hasText: name })).toBeVisible({ timeout: 15_000 });

	const searchBox = page.getByLabel('Search files');
	await searchBox.fill(name);
	await expect(page.locator('.item-row', { hasText: name })).toBeVisible({ timeout: 5_000 });

	await page.getByRole('button', { name: 'Advanced search' }).click();
	const dialog = page.locator('dialog.card[open]');
	await dialog.locator('#search-filter-modified').selectOption('today');
	await dialog.getByRole('button', { name: 'Search', exact: true }).click();

	await expect(page.locator('.item-row', { hasText: name })).toBeVisible();
});
