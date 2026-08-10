import { test, expect } from '@playwright/test';

test('create a folder, navigate into it, and back via breadcrumb', async ({ page }) => {
	await page.goto('/');
	await expect(page.getByRole('heading', { name: 'Files' })).toBeVisible();

	const folderName = 'E2E Documents';
	// The "+ New folder" flow uses a native prompt() (see routes/+page.svelte)
	// — Playwright's dialog event is how a real browser lets a test answer one.
	page.once('dialog', (dialog) => dialog.accept(folderName));
	await page.getByRole('button', { name: '+ New folder' }).click();

	const folderRow = page.locator('.item-row', { hasText: folderName });
	await expect(folderRow).toBeVisible();

	// The whole row is the click target now, not just the name text (see
	// routes/+page.svelte's own comment on why) — so the row itself, not
	// a nested button role.
	await folderRow.click();

	await expect(page.locator('.breadcrumb').getByText(folderName)).toBeVisible();
	await expect(page.getByText('This folder is empty. Drop files here, or use "+ Upload".')).toBeVisible();

	await page.getByRole('button', { name: 'Home' }).click();
	await expect(folderRow).toBeVisible();
});

test('rename a folder', async ({ page }) => {
	await page.goto('/');

	const originalName = 'E2E Before Rename';
	const renamedName = 'E2E After Rename';

	page.once('dialog', (dialog) => dialog.accept(originalName));
	await page.getByRole('button', { name: '+ New folder' }).click();

	const folderRow = page.locator('.item-row', { hasText: originalName });
	await expect(folderRow).toBeVisible();

	// Rename now lives behind the row's "⋮" action menu (see
	// routes/+page.svelte) rather than a flat button — open it first.
	await folderRow.getByRole('button', { name: 'Actions for' }).click();
	const menu = folderRow.locator('.dropdown-menu');
	await expect(menu).toBeVisible();

	// The "Rename" flow also uses a native prompt() (see
	// routes/+page.svelte's handleRename), pre-filled with the current name.
	page.once('dialog', (dialog) => {
		expect(dialog.defaultValue()).toBe(originalName);
		dialog.accept(renamedName);
	});
	await menu.getByRole('menuitem', { name: 'Rename' }).click();

	await expect(page.locator('.item-row', { hasText: renamedName })).toBeVisible();
	await expect(page.locator('.item-row', { hasText: originalName })).not.toBeVisible();
});

test('the row action menu is not clipped by a short item list', async ({ page }) => {
	await page.goto('/');

	// A fresh, otherwise-empty folder guarantees a genuinely short list —
	// the dropdown opening below a lone row has nowhere to go but past the
	// list's own bottom edge, which is exactly the case `.item-list`'s old
	// `overflow: hidden` used to clip it in (see app.css).
	const containerName = `E2E Short List ${Date.now()}`;
	page.once('dialog', (dialog) => dialog.accept(containerName));
	await page.getByRole('button', { name: '+ New folder' }).click();
	await page.locator('.item-row', { hasText: containerName }).click();

	const onlyItemName = 'Only Item';
	page.once('dialog', (dialog) => dialog.accept(onlyItemName));
	await page.getByRole('button', { name: '+ New folder' }).click();

	const row = page.locator('.item-row', { hasText: onlyItemName });
	await expect(row).toBeVisible();

	await row.getByRole('button', { name: 'Actions for' }).click();
	const menu = row.locator('.dropdown-menu');
	await expect(menu).toBeVisible();

	// The direct regression check: `.item-list` — the ancestor whose old
	// `overflow: hidden` clipped this menu away — must not clip its
	// overflow. A getBoundingClientRect()-based check wouldn't catch this
	// (an ancestor's overflow: hidden clips *painting*, not the clipped
	// element's own box geometry, so bounding boxes look identical either
	// way); the computed style is the one thing that actually distinguishes
	// "renders past the list" from "invisible past the list".
	const listOverflow = await page.locator('.item-list').evaluate((el) => getComputedStyle(el).overflow);
	expect(listOverflow).not.toBe('hidden');

	// And the end-to-end confirmation: the menu is also genuinely usable,
	// not just present in the DOM — clicking its last (furthest-down, so
	// furthest into the clipped region the old CSS left) item must still
	// reach its real handler. The dialog listener has to be registered
	// *before* the click: window.prompt() blocks synchronously, and
	// Playwright auto-dismisses any dialog with no listener attached yet,
	// which would otherwise silently eat it before this test ever sees it.
	page.once('dialog', (dialog) => {
		expect(dialog.type()).toBe('confirm');
		dialog.dismiss();
	});
	await menu.getByRole('menuitem', { name: 'Delete' }).click();
	await expect(menu).not.toBeVisible();
});

test('the row action menu closes on outside click', async ({ page }) => {
	await page.goto('/');

	const name = `E2E Menu Close ${Date.now()}`;
	page.once('dialog', (dialog) => dialog.accept(name));
	await page.getByRole('button', { name: '+ New folder' }).click();

	const row = page.locator('.item-row', { hasText: name });
	await expect(row).toBeVisible();

	await row.getByRole('button', { name: 'Actions for' }).click();
	const menu = row.locator('.dropdown-menu');
	await expect(menu).toBeVisible();

	await page.getByRole('heading', { name: 'Files' }).click();
	await expect(menu).not.toBeVisible();
});
