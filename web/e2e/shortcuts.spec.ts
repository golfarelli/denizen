import { test, expect } from '@playwright/test';
import { registerSecondUser } from './helpers/secondUser';
import { answerPrompt, acceptConfirm } from './helpers/dialog';

test('adding a shortcut to your own file: badge shown, opens the real target, rename/delete never touch the original', async ({
	page
}) => {
	await page.goto('/');

	const destName = `E2E Shortcut Dest ${Date.now()}`;
	await page.getByRole('button', { name: '+ New' }).click();
	await page.getByRole('menuitem', { name: 'New folder' }).click();
	await answerPrompt(page, destName);
	await expect(page.locator('.item-row', { hasText: destName })).toBeVisible();

	const original = uniqueTxtName('shortcut-original');
	await page.locator('input[type="file"]').setInputFiles({
		name: original,
		mimeType: 'text/plain',
		buffer: Buffer.from('the real content')
	});
	const originalRow = page.locator('.item-row', { hasText: original });
	await expect(originalRow).toBeVisible({ timeout: 15_000 });

	// --- create, via the destination picker -----------------------------------------
	await originalRow.getByRole('button', { name: 'Actions for' }).click();
	await originalRow.locator('.dropdown-menu').getByRole('menuitem', { name: 'Add shortcut' }).click();

	const dialog = page.locator('dialog.card[open]');
	await expect(dialog).toBeVisible();
	await expect(dialog.getByRole('heading')).toHaveText(`Add shortcut to "${original}"`);
	await dialog
		.locator('.item-row', { hasText: destName })
		.getByRole('button', { name: destName, exact: true })
		.click();
	await dialog.getByRole('button', { name: 'Add here' }).click();
	await expect(dialog).not.toBeVisible();

	// --- badge + opening the shortcut lands on the real target's own location -------
	await page.locator('.item-row', { hasText: destName }).dblclick();
	await expect(page).toHaveURL(/\?folder=/);
	const shortcutRow = page.locator('.item-row', { hasText: original });
	await expect(shortcutRow).toBeVisible();
	await expect(shortcutRow.locator('.shortcut-badge')).toBeVisible();

	await shortcutRow.dblclick();
	// No .breadcrumb here at all — /file/[id] is a different route from the
	// folder browser — the real proof this opened the actual target (not
	// some dead end scoped to the shortcut's own id) is its real content.
	await expect(page.locator('.preview-text')).toHaveText('the real content');
	await page.getByRole('link', { name: 'Back' }).click();
	// Back lands right where we were — inside destName, shortcutRow already
	// visible there — not at root (destName is a location, not a row here).
	await expect(page).toHaveURL(/\?folder=/);

	// --- rename the shortcut leaves the original untouched --------------------------
	await shortcutRow.getByRole('button', { name: 'Actions for' }).click();
	await shortcutRow.locator('.dropdown-menu').getByRole('menuitem', { name: 'Rename' }).click();
	await expect(page.locator('dialog[open] input[type="text"]')).toHaveValue(original);
	await answerPrompt(page, 'renamed-shortcut-only.txt');
	await expect(page.locator('.item-row', { hasText: 'renamed-shortcut-only.txt' })).toBeVisible();

	await page.getByRole('link', { name: 'Home', exact: true }).click();
	await expect(page.locator('.item-row', { hasText: original })).toBeVisible();

	// --- deleting the shortcut ("Remove shortcut", not "Delete") leaves the original too
	await page.locator('.item-row', { hasText: destName }).dblclick();
	const stillThereRow = page.locator('.item-row', { hasText: 'renamed-shortcut-only.txt' });
	await stillThereRow.getByRole('button', { name: 'Actions for' }).click();
	const menu = stillThereRow.locator('.dropdown-menu');
	await expect(menu.getByRole('menuitem', { name: 'Remove shortcut' })).toBeVisible();
	await menu.getByRole('menuitem', { name: 'Remove shortcut' }).click();
	await acceptConfirm(page);
	await expect(stillThereRow).toHaveCount(0);

	await page.getByRole('link', { name: 'Home', exact: true }).click();
	await expect(page.locator('.item-row', { hasText: original })).toBeVisible();
});

test('adding a shortcut from Shared with me', async ({ page, browser }) => {
	const { username: anna, page: annaPage } = await registerSecondUser(page, browser);

	await page.goto('/');
	const content = 'anna organizes this in her own drive now';
	const name = uniqueTxtName('shared-for-shortcut');
	await page.locator('input[type="file"]').setInputFiles({ name, mimeType: 'text/plain', buffer: Buffer.from(content) });
	const row = page.locator('.item-row', { hasText: name });
	await expect(row).toBeVisible({ timeout: 15_000 });

	await row.getByRole('button', { name: 'Actions for' }).click();
	await row.locator('.dropdown-menu').getByRole('menuitem', { name: 'Share' }).click();
	const shareDialog = page.locator('dialog.card[open]');
	await shareDialog.locator('.share-person-select').selectOption({ label: anna });
	await shareDialog.locator('.share-people-add').getByRole('button', { name: 'Share', exact: true }).click();
	await shareDialog.getByRole('button', { name: 'Cancel' }).click();

	await annaPage.goto('/shared-with-me');
	const annaRow = annaPage.locator('.item-row', { hasText: name });
	await expect(annaRow).toBeVisible();
	await annaRow.getByRole('button', { name: 'Actions for' }).click();
	await annaRow.locator('.dropdown-menu').getByRole('menuitem', { name: 'Add shortcut' }).click();

	const dialog = annaPage.locator('dialog.card[open]');
	await expect(dialog).toBeVisible();
	await dialog.getByRole('button', { name: 'Add here' }).click();
	await expect(dialog).not.toBeVisible();

	await annaPage.goto('/');
	const shortcutRow = annaPage.locator('.item-row', { hasText: name });
	await expect(shortcutRow).toBeVisible();
	await expect(shortcutRow.locator('.shortcut-badge')).toBeVisible();
	await shortcutRow.dblclick();
	await expect(annaPage.locator('.preview-text')).toHaveText(content);
});

let counter = 0;
function uniqueTxtName(base: string): string {
	counter += 1;
	return `${base}-${counter}-${Date.now()}.txt`;
}
