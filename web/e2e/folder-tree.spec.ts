import { test, expect } from '@playwright/test';
import { answerPrompt } from './helpers/dialog';

// The sidebar's own folder tree (routes/+layout.svelte's "Home" entry +
// lib/FolderTreeItem.svelte) — collapsed by default, lazily loads each
// branch's children on expand, navigates on a name click, and reveals
// wherever the current folder actually is on its own, even right after a
// plain page load (not just navigation that started from the tree itself).
test('folder tree: collapsed by default, auto-reveals the current folder, expand/navigate/collapse all work', async ({
	page
}) => {
	await page.goto('/');

	const sidebar = page.locator('.sidebar');
	const treeChildren = sidebar.locator('.tree-root > .tree-children');

	// Collapsed by default, before anything's been created or navigated.
	await expect(treeChildren).toHaveCount(0);

	const stamp = Date.now();
	const parentName = `E2E Tree Parent ${stamp}`;
	const childName = `E2E Tree Child ${stamp}`;

	await page.getByRole('button', { name: '+ New' }).click();
	await page.getByRole('menuitem', { name: 'New folder' }).click();
	await answerPrompt(page, parentName);
	const parentRow = page.locator('.item-row', { hasText: parentName });
	await expect(parentRow).toBeVisible();

	// Navigating into it (via the file list, not the tree) is enough on its
	// own to reveal it in the sidebar — Home expands itself.
	await parentRow.dblclick();
	await expect(page.locator('.breadcrumb').getByText(parentName)).toBeVisible();
	// .tree-row (toggle + name only, not the nested <ul> of children) is the
	// safe thing to match by text — .tree-item would also "contain" every
	// descendant's text once expanded, matching more than one node.
	const parentTreeRow = sidebar.locator('.tree-row', { hasText: parentName });
	await expect(parentTreeRow).toBeVisible();

	await page.getByRole('button', { name: '+ New' }).click();
	await page.getByRole('menuitem', { name: 'New folder' }).click();
	await answerPrompt(page, childName);
	await expect(page.locator('.item-row', { hasText: childName })).toBeVisible();

	// The parent's own node isn't auto-expanded (only its ancestors are) —
	// expanding it by hand reveals the child folder underneath it.
	await parentTreeRow.locator('.tree-toggle').click();
	const childTreeRow = sidebar.locator('.tree-row', { hasText: childName });
	await expect(childTreeRow).toBeVisible();

	// Clicking its name navigates into it, same as the file list would.
	await childTreeRow.locator('.tree-name').click();
	await expect(page.locator('.breadcrumb').getByText(childName)).toBeVisible();
	await expect(childTreeRow.locator('.tree-name')).toHaveClass(/active/);

	// Collapsing Home is a real, sticky user override — it doesn't fight
	// with auto-reveal as long as the current folder itself doesn't change.
	await sidebar.getByRole('button', { name: 'Collapse Home' }).click();
	await expect(treeChildren).toHaveCount(0);

	// Reach the same folder from a fresh, fully-collapsed state, through
	// the file list instead of the tree — proves auto-reveal isn't just
	// something that happened to already be open from earlier.
	await sidebar.getByRole('link', { name: 'Home', exact: true }).click();
	await expect(treeChildren).toHaveCount(0);
	await page.locator('.item-row', { hasText: parentName }).dblclick();
	await page.locator('.item-row', { hasText: childName }).dblclick();
	await expect(page.locator('.breadcrumb').getByText(childName)).toBeVisible();

	await expect(treeChildren).toBeVisible();
	await expect(childTreeRow.locator('.tree-name')).toHaveClass(/active/);
});
