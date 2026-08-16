import { test, expect } from '@playwright/test';
import { openViaDblclick } from './helpers/dblclick';
import { answerPrompt, cancelDialog } from './helpers/dialog';

test('create a folder, navigate into it, and back via breadcrumb', async ({ page }) => {
	await page.goto('/');
	// No page heading anymore (routes/+page.svelte dropped it, the
	// breadcrumb already said the same thing — secondbrain session
	// 2026-08-16) — the search box is the reliable "file browser loaded"
	// signal instead.
	await expect(page.getByPlaceholder('Search your whole drive…')).toBeVisible();

	const folderName = 'E2E Documents';
	// "New folder" opens the app's own prompt dialog (lib/GlobalDialog.svelte)
	// — fill and confirm it, no more native browser dialog to catch.
	await page.getByRole('button', { name: '+ New' }).click();
	await page.getByRole('menuitem', { name: 'New folder' }).click();
	await answerPrompt(page, folderName);

	const folderRow = page.locator('.item-row', { hasText: folderName });
	await expect(folderRow).toBeVisible();

	// The whole row is the click target now, not just the name text (see
	// routes/+page.svelte's own comment on why) — so the row itself, not
	// a nested button role. Retried as a whole gesture (see
	// helpers/dblclick.ts) — a real double-click occasionally doesn't land
	// as such this late in the suite.
	await openViaDblclick(page, folderRow, /\/\?folder=/);

	await expect(page.locator('.breadcrumb').getByText(folderName)).toBeVisible();
	await expect(page.getByText('This folder is empty. Drop files here, or use "+ New".')).toBeVisible();

	await page.locator('.breadcrumb').getByRole('button', { name: 'Home' }).click();
	await expect(folderRow).toBeVisible();
});

test('rename a folder', async ({ page }) => {
	await page.goto('/');

	const originalName = 'E2E Before Rename';
	const renamedName = 'E2E After Rename';

	await page.getByRole('button', { name: '+ New' }).click();
	await page.getByRole('menuitem', { name: 'New folder' }).click();
	await answerPrompt(page, originalName);

	const folderRow = page.locator('.item-row', { hasText: originalName });
	await expect(folderRow).toBeVisible();

	// Rename now lives behind the row's "⋮" action menu (see
	// routes/+page.svelte) rather than a flat button — open it first.
	await folderRow.getByRole('button', { name: 'Actions for' }).click();
	const menu = folderRow.locator('.dropdown-menu');
	await expect(menu).toBeVisible();

	// "Rename" opens the same prompt dialog, pre-filled with the current
	// name (routes/+page.svelte's handleRename) — checked directly on the
	// dialog's own input instead of a native dialog's defaultValue().
	await menu.getByRole('menuitem', { name: 'Rename' }).click();
	await expect(page.locator('dialog[open] input[type="text"]')).toHaveValue(originalName);
	await answerPrompt(page, renamedName);

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
	await page.getByRole('button', { name: '+ New' }).click();
	await page.getByRole('menuitem', { name: 'New folder' }).click();
	await answerPrompt(page, containerName);
	await openViaDblclick(page, page.locator('.item-row', { hasText: containerName }), /\/\?folder=/);

	const onlyItemName = 'Only Item';
	await page.getByRole('button', { name: '+ New' }).click();
	await page.getByRole('menuitem', { name: 'New folder' }).click();
	await answerPrompt(page, onlyItemName);

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
	// reach its real handler, opening the app's own confirm dialog.
	await menu.getByRole('menuitem', { name: 'Delete' }).click();
	await expect(menu).not.toBeVisible();
	await expect(page.locator('dialog[open]')).toBeVisible();
	await cancelDialog(page); // don't actually delete it — this test is about the menu, not the row
});

test('the row action menu closes on outside click', async ({ page }) => {
	await page.goto('/');

	const name = `E2E Menu Close ${Date.now()}`;
	await page.getByRole('button', { name: '+ New' }).click();
	await page.getByRole('menuitem', { name: 'New folder' }).click();
	await answerPrompt(page, name);

	const row = page.locator('.item-row', { hasText: name });
	await expect(row).toBeVisible();

	await row.getByRole('button', { name: 'Actions for' }).click();
	const menu = row.locator('.dropdown-menu');
	await expect(menu).toBeVisible();

	// Any click outside the menu closes it — the search row is just a
	// reliably-present, always-clickable spot to click for that, not
	// meaningful to this test on its own (no heading to click anymore).
	await page.locator('.search-bar-row').click();
	await expect(menu).not.toBeVisible();
});
