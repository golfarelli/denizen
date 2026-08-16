import { test, expect } from '@playwright/test';

// routes/settings/+page.svelte — picks what fills the bottom tab bar's 3
// customizable slots (lib/tabbarConfig.ts). Home is fixed and not tested
// here since it's not a slot.

test('changing a tab bar slot updates the bar and survives a reload', async ({ page }) => {
	await page.goto('/settings');

	// Default pool order is Shares/Shared with me/Trash — swap slot 2
	// (index 0) to Recent and confirm both the settings page's own select
	// and the mobile tab bar itself reflect it.
	const firstSelect = page.locator('.settings-row select').first();
	await expect(firstSelect).toHaveValue('shares');
	await firstSelect.selectOption('recent');

	await page.setViewportSize({ width: 390, height: 844 });
	const tabbar = page.locator('.bottom-tabbar');
	await expect(tabbar.getByRole('link', { name: 'Recent' })).toBeVisible();
	await expect(tabbar.getByRole('link', { name: 'My shares' })).toHaveCount(0);

	await page.reload();
	await expect(page.locator('.settings-row select').first()).toHaveValue('recent');
	await expect(tabbar.getByRole('link', { name: 'Recent' })).toBeVisible();
});

test('the same destination cannot be picked in two slots', async ({ page }) => {
	await page.goto('/settings');

	const selects = page.locator('.settings-row select');
	const secondOptionValues = await selects.nth(1).locator('option').evaluateAll((opts) => opts.map((o) => (o as HTMLOptionElement).value));
	const firstValue = await selects.first().inputValue();
	expect(secondOptionValues).not.toContain(firstValue);
});

// The mobile drawer (routes/+layout.svelte's .sidebar) shouldn't repeat
// whatever's already one tap away in .bottom-tabbar — see .drawer-hide-mobile
// in app.css.
test('the mobile drawer hides only whatever is currently in the tab bar, and updates live when settings change', async ({ page }) => {
	await page.setViewportSize({ width: 390, height: 844 });
	await page.goto('/');

	const drawer = page.locator('.sidebar');
	await page.getByRole('button', { name: 'Open menu' }).click();
	await expect(drawer).toHaveClass(/open/);

	// Default slots are Shares/Shared with me/Trash — those hide, Recent/
	// Favorites (not in the tab bar) stay visible.
	await expect(drawer.getByRole('link', { name: 'My shares' })).toBeHidden();
	await expect(drawer.getByRole('link', { name: 'Shared with me' })).toBeHidden();
	await expect(drawer.getByRole('link', { name: 'Trash' })).toBeHidden();
	await expect(drawer.getByRole('link', { name: 'Recent' })).toBeVisible();
	await expect(drawer.getByRole('link', { name: 'Favorites' })).toBeVisible();
	await expect(drawer.getByRole('link', { name: 'Settings' })).toBeVisible();

	await page.goto('/settings');
	await page.locator('.settings-row select').first().selectOption('recent');

	await page.goto('/');
	await page.getByRole('button', { name: 'Open menu' }).click();
	await expect(drawer.getByRole('link', { name: 'Recent' })).toBeHidden();
	await expect(drawer.getByRole('link', { name: 'My shares' })).toBeVisible();
});
