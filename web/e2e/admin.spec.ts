import { test, expect } from '@playwright/test';

test('create an invite, register through it in a separate session, then disable that user', async ({
	page,
	browser
}) => {
	const newUsername = `e2e-invitee-${Date.now()}`;
	const newPassword = 'e2e-invitee-password-123';

	// --- admin: create the invite ------------------------------------------------
	await page.goto('/admin');
	await expect(page.getByRole('heading', { name: 'Admin' })).toBeVisible();

	await page.getByRole('button', { name: 'Create invite' }).click();
	const inviteLinkInput = page.locator('#invite-url');
	await expect(inviteLinkInput).toBeVisible();
	const inviteUrl = await inviteLinkInput.inputValue();
	expect(inviteUrl).toContain('/register?code=');

	// --- a separate person follows the link and registers ------------------------
	// A fresh, storageState-less context — registering in the admin's own
	// `page` would overwrite *their* session (setAuth replaces whatever
	// tokens were there — see lib/auth.ts), which is exactly the mistake a
	// shared-session test here would make.
	const guestContext = await browser.newContext();
	const guestPage = await guestContext.newPage();
	await guestPage.goto(inviteUrl);
	await expect(guestPage.getByLabel('Invite code')).not.toHaveValue('');
	await guestPage.getByLabel('Username').fill(newUsername);
	await guestPage.getByLabel('Password').fill(newPassword);
	await guestPage.getByRole('button', { name: 'Create account' }).click();
	await expect(guestPage).toHaveURL('/');
	await guestContext.close();

	// --- admin: the new account shows up, and can be disabled --------------------
	await page.reload();
	const userRow = page.locator('.item-row', { hasText: newUsername });
	await expect(userRow).toBeVisible();
	await expect(userRow).not.toContainText('disabled');

	await userRow.getByRole('button', { name: 'Disable' }).click();
	await expect(userRow).toContainText('(disabled)');

	// --- disabling actually took effect: that account can no longer log in -------
	const disabledContext = await browser.newContext();
	const disabledPage = await disabledContext.newPage();
	await disabledPage.goto('/login');
	await disabledPage.getByLabel('Username').fill(newUsername);
	await disabledPage.getByLabel('Password').fill(newPassword);
	await disabledPage.getByRole('button', { name: 'Log in' }).click();
	await expect(disabledPage.locator('.error-text')).not.toHaveText('');
	await expect(disabledPage).toHaveURL(/\/login/);
	await disabledContext.close();
});
