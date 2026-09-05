import { test, expect } from '@playwright/test';
import { registerSecondUser } from './helpers/secondUser';

function uniqueName(base: string): string {
	return `${base}-${Date.now()}-${Math.floor(Math.random() * 1e6)}.txt`;
}

test('starring a file from its row menu shows it on /favorites, unstarring removes it', async ({ page }) => {
	await page.goto('/');

	const name = uniqueName('favorite-me');
	await page.locator('input[type="file"]').setInputFiles({
		name,
		mimeType: 'text/plain',
		buffer: Buffer.from('star this one')
	});
	const row = page.locator('.item-row', { hasText: name });
	await expect(row).toBeVisible({ timeout: 15_000 });

	await row.getByRole('button', { name: 'Actions for' }).click();
	await row.locator('.dropdown-menu').getByRole('menuitem', { name: 'Add to favorites' }).click();

	// The menu item itself relabels once it's a favorite (mutated in place,
	// no reload — see routes/+page.svelte's handleToggleFavorite).
	await row.getByRole('button', { name: 'Actions for' }).click();
	await expect(row.locator('.dropdown-menu').getByRole('menuitem', { name: 'Remove from favorites' })).toBeVisible();
	await page.keyboard.press('Escape');

	await page.getByRole('link', { name: 'Favorites' }).click();
	await expect(page).toHaveURL(/\/favorites$/);
	const favRow = page.locator('.item-row', { hasText: name });
	await expect(favRow).toBeVisible();

	await favRow.getByRole('button', { name: 'Actions for' }).click();
	await favRow.locator('.dropdown-menu').getByRole('menuitem', { name: 'Remove from favorites' }).click();
	await expect(favRow).toHaveCount(0);
});

test('bulk-favoriting a selection adds every item, a shared item can be favorited too', async ({ page, browser }) => {
	const { page: annaPage, username: anna } = await registerSecondUser(page, browser);
	// registerSecondUser navigates the admin's own page to /admin to create
	// the invite — back to the file browser before uploading anything.
	await page.goto('/');

	// --- bulk favorite on alice's own two files -----------------------------------
	const nameA = uniqueName('bulk-fav-a');
	const nameB = uniqueName('bulk-fav-b');
	await page.locator('input[type="file"]').setInputFiles([
		{ name: nameA, mimeType: 'text/plain', buffer: Buffer.from('a') },
		{ name: nameB, mimeType: 'text/plain', buffer: Buffer.from('b') }
	]);
	await expect(page.locator('.item-row', { hasText: nameB })).toBeVisible({ timeout: 15_000 });

	await page.locator('.item-row', { hasText: nameA }).click();
	await page.locator('.item-row', { hasText: nameB }).click({ modifiers: ['Control'] });
	await page.locator('.selection-toolbar').getByRole('button', { name: 'Add to favorites' }).click();

	await page.getByRole('link', { name: 'Favorites' }).click();
	await expect(page.locator('.item-row', { hasText: nameA })).toBeVisible();
	await expect(page.locator('.item-row', { hasText: nameB })).toBeVisible();

	// --- a file alice shares with anna, anna favorites it from Shared with me -----
	await page.goto('/');
	const sharedName = uniqueName('shared-favorite');
	await page.locator('input[type="file"]').setInputFiles({
		name: sharedName,
		mimeType: 'text/plain',
		buffer: Buffer.from('shared and starred')
	});
	const sharedRow = page.locator('.item-row', { hasText: sharedName });
	await expect(sharedRow).toBeVisible({ timeout: 15_000 });
	await sharedRow.getByRole('button', { name: `Actions for ${sharedName}` }).click();
	await sharedRow.locator('.dropdown-menu').getByRole('menuitem', { name: 'Share' }).click();

	const shareDialog = page.locator('dialog.card[open]');
	await shareDialog.locator('.share-person-select').selectOption({ label: anna });
	await shareDialog.locator('.share-people-add').getByRole('button', { name: 'Share', exact: true }).click();
	await shareDialog.getByRole('button', { name: 'Cancel' }).click();

	await annaPage.goto('/shared-with-me');
	const annaRow = annaPage.locator('.item-row', { hasText: sharedName });
	await expect(annaRow).toBeVisible();
	await annaRow.getByRole('button', { name: `Actions for ${sharedName}` }).click();
	await annaRow.locator('.dropdown-menu').getByRole('menuitem', { name: 'Add to favorites' }).click();

	await annaPage.getByRole('link', { name: 'Favorites' }).click();
	await expect(annaPage.locator('.item-row', { hasText: sharedName })).toBeVisible();
});
