import { expect, type Browser, type Page } from '@playwright/test';
import { forceEnglishLocale } from './locale';

// Registers a brand new user through a fresh admin-created invite, in its
// own browser context (never the admin's own `page` — registering there
// would overwrite the admin's session, a gotcha several specs guard
// against). Returns that context's own page, logged in for real, plus the
// username picked (unique per call so a "Choose a person…" directory
// dropdown never has stale rows from an earlier test).
export async function registerSecondUser(adminPage: Page, browser: Browser): Promise<{ page: Page; username: string }> {
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
