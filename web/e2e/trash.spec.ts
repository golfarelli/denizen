import { test, expect } from '@playwright/test';

test('delete a folder, see it in trash, and restore it', async ({ page }) => {
	await page.goto('/');

	const name = `E2E Trash Restore ${Date.now()}`;
	page.once('dialog', (dialog) => dialog.accept(name));
	await page.getByRole('button', { name: '+ New folder' }).click();

	const row = page.locator('.item-row', { hasText: name });
	await expect(row).toBeVisible();

	await row.getByRole('button', { name: 'Actions for' }).click();
	page.once('dialog', (dialog) => dialog.accept());
	await row.locator('.dropdown-menu').getByRole('menuitem', { name: 'Delete' }).click();
	await expect(row).not.toBeVisible();

	await page.goto('/trash');
	const trashRow = page.locator('.item-row', { hasText: name });
	await expect(trashRow).toBeVisible();

	await trashRow.getByRole('button', { name: 'Restore' }).click();
	await expect(trashRow).not.toBeVisible();

	// Restored items land back in the file browser, not just out of the
	// trash listing — this is the part a merely-optimistic UI could fake.
	await page.goto('/');
	await expect(page.locator('.item-row', { hasText: name })).toBeVisible();
});

test('delete a folder, then permanently delete it from trash', async ({ page }) => {
	await page.goto('/');

	const name = `E2E Trash Purge ${Date.now()}`;
	page.once('dialog', (dialog) => dialog.accept(name));
	await page.getByRole('button', { name: '+ New folder' }).click();

	const row = page.locator('.item-row', { hasText: name });
	await expect(row).toBeVisible();

	await row.getByRole('button', { name: 'Actions for' }).click();
	page.once('dialog', (dialog) => dialog.accept());
	await row.locator('.dropdown-menu').getByRole('menuitem', { name: 'Delete' }).click();
	await expect(row).not.toBeVisible();

	await page.goto('/trash');
	const trashRow = page.locator('.item-row', { hasText: name });
	await expect(trashRow).toBeVisible();

	page.once('dialog', (dialog) => dialog.accept());
	await trashRow.getByRole('button', { name: 'Delete forever' }).click();
	await expect(trashRow).not.toBeVisible();

	// Gone for good: it must not reappear anywhere, trash included.
	await page.reload();
	await expect(page.locator('.item-row', { hasText: name })).not.toBeVisible();
});
