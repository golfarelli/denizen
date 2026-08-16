import { test, expect } from '@playwright/test';
import { answerPrompt, acceptConfirm } from './helpers/dialog';

test('delete a folder, see it in trash, and restore it', async ({ page }) => {
	await page.goto('/');

	const name = `E2E Trash Restore ${Date.now()}`;
	await page.getByRole('button', { name: '+ New' }).click();
	await page.getByRole('menuitem', { name: 'New folder' }).click();
	await answerPrompt(page, name);

	const row = page.locator('.item-row', { hasText: name });
	await expect(row).toBeVisible();

	await row.getByRole('button', { name: 'Actions for' }).click();
	await row.locator('.dropdown-menu').getByRole('menuitem', { name: 'Delete' }).click();
	await acceptConfirm(page);
	await expect(row).not.toBeVisible();

	await page.goto('/trash');
	const trashRow = page.locator('.item-row', { hasText: name });
	await expect(trashRow).toBeVisible();

	// Restore/Delete forever live behind the same "⋮" action menu the file
	// browser's own rows use now, not always-visible buttons.
	await trashRow.getByRole('button', { name: 'Actions for' }).click();
	await trashRow.locator('.dropdown-menu').getByRole('menuitem', { name: 'Restore' }).click();
	await expect(trashRow).not.toBeVisible();

	// Restored items land back in the file browser, not just out of the
	// trash listing — this is the part a merely-optimistic UI could fake.
	await page.goto('/');
	await expect(page.locator('.item-row', { hasText: name })).toBeVisible();
});

test('delete a folder, then permanently delete it from trash', async ({ page }) => {
	await page.goto('/');

	const name = `E2E Trash Purge ${Date.now()}`;
	await page.getByRole('button', { name: '+ New' }).click();
	await page.getByRole('menuitem', { name: 'New folder' }).click();
	await answerPrompt(page, name);

	const row = page.locator('.item-row', { hasText: name });
	await expect(row).toBeVisible();

	await row.getByRole('button', { name: 'Actions for' }).click();
	await row.locator('.dropdown-menu').getByRole('menuitem', { name: 'Delete' }).click();
	await acceptConfirm(page);
	await expect(row).not.toBeVisible();

	await page.goto('/trash');
	const trashRow = page.locator('.item-row', { hasText: name });
	await expect(trashRow).toBeVisible();

	await trashRow.getByRole('button', { name: 'Actions for' }).click();
	await trashRow.locator('.dropdown-menu').getByRole('menuitem', { name: 'Delete forever' }).click();
	await acceptConfirm(page);
	await expect(trashRow).not.toBeVisible();

	// Gone for good: it must not reappear anywhere, trash included.
	await page.reload();
	await expect(page.locator('.item-row', { hasText: name })).not.toBeVisible();
});

test('a trashed file can be opened for a quick look before restoring or deleting it', async ({ page }) => {
	await page.goto('/');
	const name = `share-test-${Date.now()}.txt`;
	await page.locator('input[type="file"]').setInputFiles({
		name,
		mimeType: 'text/plain',
		buffer: Buffer.from('trashed but still viewable')
	});

	const row = page.locator('.item-row', { hasText: name });
	await expect(row).toBeVisible({ timeout: 15_000 });
	await row.getByRole('button', { name: `Actions for ${name}` }).click();
	await row.locator('.dropdown-menu').getByRole('menuitem', { name: 'Delete' }).click();
	await acceptConfirm(page);
	await expect(row).not.toBeVisible();

	await page.goto('/trash');
	const trashRow = page.locator('.item-row', { hasText: name });
	await expect(trashRow).toBeVisible();

	// The row's own name is clickable for a file (unlike a folder, which
	// this app has no "browse into a trashed folder" view for). Scoped to
	// .item-name specifically, not just getByRole('button', {name}) — the
	// kebab's own "Actions for <name>" label also contains the file name
	// as a substring, matching both by accessible name alone.
	await trashRow.locator('.item-name').click();
	await expect(page).toHaveURL(/\/file\/.+from=trash/);
	await expect(page.locator('.preview-text')).toHaveText('trashed but still viewable');

	// Back returns to Trash, not the root file browser.
	await page.getByRole('link', { name: 'Back' }).click();
	await expect(page).toHaveURL(/\/trash$/);
});
