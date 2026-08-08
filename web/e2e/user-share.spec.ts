import { test, expect } from '@playwright/test';
import { forceEnglishLocale } from './helpers/locale';

// Mirrors auth.setup.ts's own ADMIN_USERNAME constant — can't import it
// directly, Playwright disallows a regular spec importing a *.setup.ts
// file.
const ADMIN_USERNAME = 'e2e-admin';

let uploadCounter = 0;
function uniqueName(ext: string): string {
	uploadCounter += 1;
	return `user-share-test-${uploadCounter}-${Date.now()}.${ext}`;
}

// Registers a brand new user through a fresh admin-created invite, in its
// own browser context (never the admin's own `page` — registering there
// would overwrite the admin's session, same gotcha share.spec.ts/
// admin.spec.ts already guard against). Returns that context's own page,
// logged in for real, plus the username picked (unique per test run so
// the "Choose a person…" directory dropdown never has stale rows from an
// earlier test).
async function registerSecondUser(
	adminPage: import('@playwright/test').Page,
	browser: import('@playwright/test').Browser
): Promise<{ page: import('@playwright/test').Page; username: string }> {
	await adminPage.goto('/admin');
	await adminPage.getByRole('button', { name: 'Create invite' }).click();
	const inviteUrl = await adminPage.locator('#invite-url').inputValue();

	const context = await browser.newContext();
	await forceEnglishLocale(context);
	const page = await context.newPage();
	await page.goto(inviteUrl);
	const username = `anna-${Date.now()}`;
	await page.getByLabel('Username').fill(username);
	await page.getByLabel('Password').fill('another-strong-password-123');
	await page.getByRole('button', { name: 'Create account' }).click();
	await expect(page).toHaveURL('/');
	return { page, username };
}

test('sharing a file with a specific person: they see it view-only in Shared with me, and losing the grant cuts them off', async ({
	page,
	browser
}) => {
	const { page: annaPage, username: anna } = await registerSecondUser(page, browser);

	// --- fabio uploads a file and shares it directly with anna --------------------
	await page.goto('/');
	const name = uniqueName('txt');
	await page
		.locator('input[type="file"]')
		.setInputFiles({ name, mimeType: 'text/plain', buffer: Buffer.from('hello anna') });
	const row = page.locator('.item-row', { hasText: name });
	await expect(row).toBeVisible({ timeout: 15_000 });
	await row.getByRole('button', { name: `Actions for ${name}` }).click();
	await row.locator('.dropdown-menu').getByRole('menuitem', { name: 'Share' }).click();

	const dialog = page.locator('dialog.card[open]');
	await expect(dialog.getByRole('heading', { name: 'People with access', level: 3 })).toBeVisible();
	await dialog.locator('.share-person-select').selectOption({ label: anna });
	await dialog.locator('.share-people-add').getByRole('button', { name: 'Share', exact: true }).click();
	await expect(dialog.locator('.share-people-list')).toContainText(anna);
	await dialog.getByRole('button', { name: 'Cancel' }).click();
	await expect(dialog).not.toBeVisible();

	// --- anna sees it in her own "Shared with me", view-only ----------------------
	await annaPage.goto('/shared-with-me');
	const annaRow = annaPage.locator('.item-row', { hasText: name });
	await expect(annaRow).toBeVisible();
	await expect(annaRow).toContainText(ADMIN_USERNAME); // the "Shared by" column

	await annaRow.locator('.item-name').click();
	await expect(annaPage).toHaveURL(/\/file\//);
	await expect(annaPage.locator('.preview-title')).toHaveText(name);
	await expect(annaPage.locator('.preview-text')).toContainText('hello anna');

	await annaPage.getByRole('button', { name: `Actions for ${name}` }).click();
	const annaMenu = annaPage.locator('.dropdown-menu');
	await expect(annaMenu.getByRole('menuitem', { name: 'Download' })).toBeVisible();
	// View-only: none of the owner-only actions exist for her at all — not
	// just disabled, since the backend would 404 them anyway (see
	// internal/service/item.go's GetIncludingTrashed).
	await expect(annaMenu.getByRole('menuitem', { name: 'Rename' })).toHaveCount(0);
	await expect(annaMenu.getByRole('menuitem', { name: 'Move', exact: true })).toHaveCount(0);
	await expect(annaMenu.getByRole('menuitem', { name: 'Delete' })).toHaveCount(0);
	await annaMenu.getByRole('button', { name: 'Cancel' }).click();

	await annaPage.locator('.preview-back').click();
	await expect(annaPage).toHaveURL('/shared-with-me');

	// --- fabio revokes access -------------------------------------------------------
	await page.goto('/');
	await row.getByRole('button', { name: `Actions for ${name}` }).click();
	await row.locator('.dropdown-menu').getByRole('menuitem', { name: 'Share' }).click();
	await expect(dialog.locator('.share-people-list')).toContainText(anna);
	await dialog.locator('.share-people-list li', { hasText: anna }).getByRole('button').click();
	// The whole list unmounts once it's empty (`{#if grants.length > 0}` —
	// see ShareDialog.svelte), not just anna's own row disappearing.
	await expect(dialog.locator('.share-people-list')).toHaveCount(0);
	await dialog.getByRole('button', { name: 'Cancel' }).click();

	// --- anna loses access immediately ----------------------------------------------
	await annaPage.goto('/shared-with-me');
	await expect(annaPage.locator('.item-row', { hasText: name })).toHaveCount(0);
});

test('sharing a folder offers both the per-person section and a link', async ({ page }) => {
	await page.goto('/');

	const folderName = `E2E Folder Share ${Date.now()}`;
	page.once('dialog', (dialog) => dialog.accept(folderName));
	await page.getByRole('button', { name: '+ New folder' }).click();

	const row = page.locator('.item-row', { hasText: folderName });
	await expect(row).toBeVisible();
	await row.getByRole('button', { name: `Actions for ${folderName}` }).click();
	await row.locator('.dropdown-menu').getByRole('menuitem', { name: 'Share' }).click();

	const dialog = page.locator('dialog.card[open]');
	await expect(dialog.getByRole('heading', { name: `Share "${folderName}"` })).toBeVisible();
	// Folders get the same per-person section files do now — a grant on a
	// folder is inherited by everything nested inside it.
	await expect(dialog.getByRole('heading', { name: 'People with access', level: 3 })).toBeVisible();
	await expect(dialog.getByRole('button', { name: 'Create link' })).toBeVisible();
	await dialog.getByRole('button', { name: 'Cancel' }).click();
});
