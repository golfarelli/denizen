import { test, expect } from '@playwright/test';

let counter = 0;
function uniqueName(base: string, ext: string): string {
	counter += 1;
	return `${base}-${counter}-${Date.now()}.${ext}`;
}

test('the search box finds a file in a different folder by name, and by content', async ({ page }) => {
	await page.goto('/');

	const folderName = `Search Folder ${Date.now()}`;
	page.once('dialog', (dialog) => dialog.accept(folderName));
	await page.getByRole('button', { name: '+ New' }).click();
	await page.getByRole('menuitem', { name: 'New folder' }).click();
	const folderRow = page.locator('.item-row', { hasText: folderName });
	await expect(folderRow).toBeVisible();
	await folderRow.locator('.item-name').click();
	await expect(page).toHaveURL(/\?folder=/);

	// Upload, inside this nested folder, one file findable by name and one
	// only findable by its content — a real cross-drive search has to
	// find both, from root, without opening the folder again.
	const byName = uniqueName('bolletta-luce', 'txt');
	const byContent = uniqueName('nota', 'txt');
	await page.locator('input[type="file"]').setInputFiles([
		{ name: byName, mimeType: 'text/plain', buffer: Buffer.from('contenuto qualsiasi') },
		{ name: byContent, mimeType: 'text/plain', buffer: Buffer.from('il preventivo per il tetto è arrivato') }
	]);
	await expect(page.locator('.item-row', { hasText: byName })).toBeVisible({ timeout: 15_000 });

	// --- back to root, search by name --------------------------------------------
	await page.goto('/');
	const searchBox = page.getByLabel('Search files');
	await searchBox.fill('bolletta-luce');
	await expect(page.locator('.item-row', { hasText: byName })).toBeVisible({ timeout: 5_000 });
	await expect(page.locator('.hint')).toContainText('1');

	// --- search by content ---------------------------------------------------------
	await searchBox.fill('');
	await searchBox.fill('preventivo');
	await expect(page.locator('.item-row', { hasText: byContent })).toBeVisible({ timeout: 5_000 });
	// The name-only match from the previous search is gone — a fresh query
	// replaces the results, it doesn't accumulate them.
	await expect(page.locator('.item-row', { hasText: byName })).toHaveCount(0);

	// --- opening a result works exactly like a normal row --------------------------
	await page.locator('.item-row', { hasText: byContent }).locator('.item-name').click();
	await expect(page).toHaveURL(/\/file\//);
	await expect(page.locator('.preview-text')).toContainText('preventivo');

	// --- clearing the box returns to normal folder browsing -------------------------
	await page.goto('/');
	await expect(page.locator('.item-row', { hasText: byName })).toHaveCount(0); // back at root, nested file not shown
	await expect(page.locator('.item-row', { hasText: folderName })).toBeVisible();
});

test('the search box reports no matches for a query that hits nothing', async ({ page }) => {
	await page.goto('/');
	const searchBox = page.getByLabel('Search files');
	await searchBox.fill(`nothing-should-ever-match-${Date.now()}`);
	await expect(page.locator('.empty-state')).toBeVisible({ timeout: 5_000 });
});
