import { test as setup, expect } from '@playwright/test';
import { readFileSync } from 'fs';
import { forceEnglishLocale } from './helpers/locale';

export const ADMIN_USERNAME = 'e2e-admin';
export const ADMIN_PASSWORD = 'e2e-admin-password-123';
export const ADMIN_AUTH_FILE = 'e2e/.auth/admin.json';

setup('register the bootstrap admin account', async ({ page }) => {
	const inviteCode = await waitForBootstrapInviteCode();

	// Denizen defaults to Italian — this storageState snapshot (captured
	// below) becomes the whole suite's own starting point (see
	// playwright.config.ts), so forcing English here is what keeps every
	// other test's English-text assertions valid without having to repeat
	// this per test.
	await forceEnglishLocale(page.context());

	await page.goto(`/register?code=${inviteCode}`);
	await expect(page.getByLabel('Invite code')).toHaveValue(inviteCode);

	await page.getByLabel('Username').fill(ADMIN_USERNAME);
	await page.getByLabel('Password').fill(ADMIN_PASSWORD);
	await page.getByRole('button', { name: 'Create account' }).click();

	// A successful register+auto-login lands on the file browser — no page
	// heading to check anymore (routes/+page.svelte dropped it, the
	// breadcrumb already said the same thing), so the search box is the
	// next-most-reliable "the file browser really loaded" signal instead.
	await expect(page).toHaveURL('/');
	await expect(page.getByPlaceholder('Search your whole drive…')).toBeVisible();

	await page.context().storageState({ path: ADMIN_AUTH_FILE });
});

/** main.go only ever logs the bootstrap invite code (see
 * AuthService.EnsureBootstrapInvite) — there's no HTTP endpoint that hands
 * it out, deliberately, since that would mean anyone reaching the API
 * could claim the admin account. global-setup.ts redirects the real
 * server's stdout/stderr to a file for exactly this reason. */
async function waitForBootstrapInviteCode(): Promise<string> {
	const logPath = process.env.DENIZEN_E2E_LOG_PATH;
	if (!logPath) {
		throw new Error('DENIZEN_E2E_LOG_PATH is not set — global-setup.ts should have set it');
	}
	const deadline = Date.now() + 10_000;
	while (Date.now() < deadline) {
		try {
			const log = readFileSync(logPath, 'utf-8');
			const match = log.match(/invite code: ([0-9a-f-]{36})/);
			if (match) return match[1];
		} catch {
			// the log file may not have been created by the OS yet on the
			// very first attempts
		}
		await new Promise((resolve) => setTimeout(resolve, 100));
	}
	throw new Error(`Timed out waiting for the bootstrap invite code in ${logPath}`);
}
