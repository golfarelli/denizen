import { test, expect } from '@playwright/test';

// Mobile-only bottom nav for Home/Shares/Shared with me/Trash (routes/
// +layout.svelte's .bottom-tabbar) — reachable in one tap instead of
// hamburger-then-tap. Not shown at desktop width (the sidebar already
// covers this there) or while a file-list selection is active (the fixed
// .selection-toolbar owns that same screen edge instead).

test('is visible on a mobile viewport and navigates, but not at desktop width', async ({ page }) => {
	await page.setViewportSize({ width: 390, height: 844 });
	await page.goto('/');

	const tabbar = page.locator('.bottom-tabbar');
	await expect(tabbar).toBeVisible();
	await expect(tabbar.getByRole('link', { name: 'Trash' })).toBeVisible();

	await tabbar.getByRole('link', { name: 'Trash' }).click();
	await expect(page).toHaveURL(/\/trash$/);
	await expect(tabbar.getByRole('link', { name: 'Trash' })).toHaveClass(/active/);

	await tabbar.getByRole('link', { name: 'Home' }).click();
	await expect(page).toHaveURL('/');

	await page.setViewportSize({ width: 1280, height: 900 });
	await expect(tabbar).not.toBeVisible();
});

test('hides while a file-list selection is active, reappears once it clears', async ({ page }) => {
	await page.setViewportSize({ width: 390, height: 844 });
	await page.goto('/');

	const name = `bottom-tabbar-e2e-${Date.now()}.txt`;
	await page.locator('input[type="file"]').setInputFiles({
		name,
		mimeType: 'text/plain',
		buffer: Buffer.from('for the bottom tab bar selection test')
	});
	const row = page.locator('.item-row', { hasText: name });
	await expect(row).toBeVisible({ timeout: 15_000 });

	const tabbar = page.locator('.bottom-tabbar');
	await expect(tabbar).toBeVisible();

	await row.click();
	await expect(page.locator('.selection-toolbar')).toBeVisible();
	await expect(tabbar).not.toBeVisible();

	await page.locator('.selection-toolbar').getByRole('button', { name: 'Cancel' }).click();
	await expect(page.locator('.selection-toolbar')).toHaveCount(0);
	await expect(tabbar).toBeVisible();
});
