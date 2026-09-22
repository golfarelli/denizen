import { test, expect, devices, type Page } from '@playwright/test';
import { answerPrompt } from './helpers/dialog';

// Drag-and-drop moving (lib/dragMove.ts): rows/tiles are draggable, a folder
// in the listing or in the sidebar tree (Home included) is a drop target.
//
// Every test works inside its own fresh folder (see openWorkFolder) instead
// of the root, which every other spec keeps adding rows to: the rows being
// dragged then stay near the top of the viewport. That matters because the
// fixed selection toolbar appears at the bottom the moment a drag selects
// its row — dragging a row that sits right at the bottom edge puts that
// toolbar under the pointer mid-gesture, which stalls Playwright's own drag
// implementation (not something a person hits, but not worth depending on).

let counter = 0;
function uniqueName(base: string, ext = ''): string {
	counter += 1;
	return `${base}-${Date.now()}-${counter}${ext}`;
}

function row(page: Page, name: string) {
	return page.locator('.item-row', { hasText: name });
}

async function createFolder(page: Page, name: string) {
	await page.getByRole('button', { name: '+ New' }).click();
	await page.getByRole('menuitem', { name: 'New folder' }).click();
	await answerPrompt(page, name);
	await expect(row(page, name)).toBeVisible();
}

async function uploadTextFiles(page: Page, names: string[]) {
	await page
		.locator('input[type="file"]')
		.setInputFiles(names.map((name) => ({ name, mimeType: 'text/plain', buffer: Buffer.from(name) })));
	for (const name of names) {
		await expect(row(page, name)).toBeVisible({ timeout: 15_000 });
	}
}

// Creates a folder at the root and opens it; returns its name.
async function openWorkFolder(page: Page): Promise<string> {
	await page.goto('/');
	const name = uniqueName('E2E DnD Work');
	await createFolder(page, name);
	await row(page, name).dblclick();
	await expect(page.locator('.breadcrumb').getByText(name)).toBeVisible();
	return name;
}

// Sidebar tree row for a folder (.tree-row, not .tree-item: the latter
// also "contains" every descendant's text once expanded).
function treeRow(page: Page, name: string) {
	return page.locator('.sidebar .tree-row', { hasText: name });
}

test('dragging a file onto a folder in the listing moves it — with the drop target highlighted and no upload overlay', async ({
	page
}) => {
	await openWorkFolder(page);
	const dest = uniqueName('dest');
	const file = uniqueName('dnd-file', '.txt');
	await createFolder(page, dest);
	await uploadTextFiles(page, [file]);

	// Drive the drag by hand so the in-flight state can be inspected.
	const src = await row(page, file).boundingBox();
	const dst = await row(page, dest).boundingBox();
	await page.mouse.move(src!.x + 80, src!.y + src!.height / 2);
	await page.mouse.down();
	await page.mouse.move(dst!.x + 80, dst!.y + dst!.height / 2, { steps: 10 });

	await expect(row(page, dest)).toHaveClass(/drop-target/);
	// An item drag inside Denizen is not a file upload from the OS.
	await expect(page.locator('.dropzone-overlay')).toHaveCount(0);

	await page.mouse.up();

	await expect(row(page, file)).not.toBeVisible();
	await expect(page.locator('.status-text')).toContainText(`Moved “${file}” to ${dest}`);
	await expect(page.locator('.drop-target')).toHaveCount(0);

	await row(page, dest).dblclick();
	await expect(row(page, file)).toBeVisible();
});

test('dragging a selected item drags the whole selection; dragging an unselected one drags only that one', async ({
	page
}) => {
	await openWorkFolder(page);
	const dest = uniqueName('dest');
	const a = uniqueName('multi-a', '.txt');
	const b = uniqueName('multi-b', '.txt');
	const c = uniqueName('multi-c', '.txt');
	await createFolder(page, dest);
	await uploadTextFiles(page, [a, b, c]);

	// Select a + b, then drag a: both travel, c stays.
	await row(page, a).click();
	await row(page, b).click({ modifiers: ['Control'] });
	await row(page, a).dragTo(row(page, dest));
	await expect(page.locator('.status-text')).toContainText(`Moved 2 items to ${dest}`);
	await expect(row(page, a)).not.toBeVisible();
	await expect(row(page, b)).not.toBeVisible();
	await expect(row(page, c)).toBeVisible();

	// Select d, then drag e (not part of the selection): only e moves, and
	// it replaces the selection (d stays put).
	const d = uniqueName('multi-d', '.txt');
	const e = uniqueName('multi-e', '.txt');
	await uploadTextFiles(page, [d, e]);
	await row(page, d).click();
	await row(page, e).dragTo(row(page, dest));
	await expect(row(page, e)).not.toBeVisible();
	await expect(row(page, d)).toBeVisible();
	await expect(row(page, c)).toBeVisible();

	await row(page, dest).dblclick();
	await expect(row(page, a)).toBeVisible();
	await expect(row(page, b)).toBeVisible();
	await expect(row(page, e)).toBeVisible();
	await expect(row(page, d)).not.toBeVisible();
});

test('a folder and Home in the sidebar tree are drop targets', async ({ page }) => {
	const work = await openWorkFolder(page);
	const dest = uniqueName('dest');
	const file = uniqueName('tree-file', '.txt');
	await createFolder(page, dest);
	await uploadTextFiles(page, [file]);

	// Entering the work folder revealed it in the tree; expand it to see dest.
	await treeRow(page, work).locator('.tree-toggle').click();
	await expect(treeRow(page, dest)).toBeVisible();

	// List row -> sidebar folder, without navigating into it.
	await row(page, file).dragTo(treeRow(page, dest));
	await expect(row(page, file)).not.toBeVisible();
	await expect(page.locator('.breadcrumb')).not.toContainText(dest);

	// Now inside dest: drag the file out onto Home (the root).
	await treeRow(page, dest).locator('.tree-name').click();
	await expect(page.locator('.breadcrumb').getByText(dest)).toBeVisible();
	await expect(row(page, file)).toBeVisible();
	await row(page, file).dragTo(page.locator('.sidebar .tree-root > .tree-row'));
	await expect(row(page, file)).not.toBeVisible();

	await page.locator('.sidebar .tree-root > .tree-row .tree-name').click();
	await expect(row(page, file)).toBeVisible();
});

test('dropping is only offered where it would move something, and the server still refuses an impossible move', async ({
	page
}) => {
	const work = await openWorkFolder(page);
	const parent = uniqueName('parent');
	const child = uniqueName('child');
	await createFolder(page, parent);
	await row(page, parent).dblclick();
	await createFolder(page, child);

	// A folder dragged over itself is not a drop target (nothing to move).
	const box = await row(page, child).boundingBox();
	await page.mouse.move(box!.x + 80, box!.y + box!.height / 2);
	await page.mouse.down();
	await page.mouse.move(box!.x + 90, box!.y + box!.height / 2 + 2, { steps: 4 });
	await expect(row(page, child)).not.toHaveClass(/drop-target/);
	await page.mouse.up();
	await expect(row(page, child)).toBeVisible();

	// Back up in the work folder: drag `parent` onto its own child in the
	// sidebar tree. The UI can't know the ancestry so it offers the drop;
	// the server's guard rejects it and the folder stays where it is.
	await page.locator('.breadcrumb').getByText(work).click();
	await expect(row(page, parent)).toBeVisible();
	await treeRow(page, work).locator('.tree-toggle').click();
	await treeRow(page, parent).locator('.tree-toggle').click();
	await expect(treeRow(page, child)).toBeVisible();
	await row(page, parent).dragTo(treeRow(page, child));
	await expect(page.locator('p.error-text')).toContainText('Could not move this item.');
	await expect(row(page, parent)).toBeVisible();
});

test('rows are not draggable on a touch device (touch keeps using "Move to…")', async ({ browser }) => {
	const context = await browser.newContext({
		...devices['Pixel 7'],
		hasTouch: true,
		isMobile: true,
		storageState: 'e2e/.auth/admin.json'
	});
	const page = await context.newPage();
	await page.goto('/');
	const name = uniqueName('touch-file', '.txt');
	await page
		.locator('input[type="file"]')
		.setInputFiles({ name, mimeType: 'text/plain', buffer: Buffer.from(name) });
	await expect(row(page, name)).toBeVisible({ timeout: 15_000 });
	await expect(row(page, name)).not.toHaveAttribute('draggable', 'true');
	await context.close();
});
