import { test, expect, devices } from '@playwright/test';
import { answerPrompt, acceptConfirm } from './helpers/dialog';

function uniqueName(base: string): string {
	return `${base}-${Date.now()}-${Math.floor(Math.random() * 1e6)}.txt`;
}

test('selecting rows shows the bulk toolbar with the right count, and "Select all" toggles everything', async ({
	page
}) => {
	await page.goto('/');

	// A third, deliberately unselected file — "Select all" needs to
	// actually extend the selection to prove it selects *everything*, not
	// just toggle back off (which happens whenever selectedIds.size
	// already equals visibleItems.length — a genuinely different button
	// state, "Deselect all", not just a different assertion here).
	const nameA = uniqueName('bulk-a');
	const nameB = uniqueName('bulk-b');
	const nameC = uniqueName('bulk-c');
	await page.locator('input[type="file"]').setInputFiles([
		{ name: nameA, mimeType: 'text/plain', buffer: Buffer.from('a') },
		{ name: nameB, mimeType: 'text/plain', buffer: Buffer.from('b') },
		{ name: nameC, mimeType: 'text/plain', buffer: Buffer.from('c') }
	]);
	const rowA = page.locator('.item-row', { hasText: nameA });
	const rowB = page.locator('.item-row', { hasText: nameB });
	const rowC = page.locator('.item-row', { hasText: nameC });
	await expect(rowA).toBeVisible({ timeout: 15_000 });
	await expect(rowB).toBeVisible({ timeout: 15_000 });
	await expect(rowC).toBeVisible({ timeout: 15_000 });

	await expect(page.locator('.selection-toolbar')).toHaveCount(0);

	// A plain click selects — the desktop checkbox is gone (see
	// routes/+page.svelte's handleItemClick), Ctrl+click adds to the
	// selection instead of replacing it.
	await rowA.click();
	await expect(page.locator('.selection-toolbar')).toContainText('1 selected');
	await rowB.click({ modifiers: ['Control'] });
	await expect(page.locator('.selection-toolbar')).toContainText('2 selected');

	// "Select all" / "Deselect all" (the toolbar button that replaces the
	// old header checkbox) toggles every row in the folder, not just the
	// two already selected — exact: true matters here, "Select all" is
	// otherwise a substring of "Deselect all" too.
	await page.locator('.selection-toolbar').getByRole('button', { name: 'Select all', exact: true }).click();
	await expect(rowA).toHaveClass(/item-row-selected/);
	await expect(rowB).toHaveClass(/item-row-selected/);
	await expect(rowC).toHaveClass(/item-row-selected/);

	await page.locator('.selection-toolbar').getByRole('button', { name: 'Deselect all' }).click();
	await expect(rowA).not.toHaveClass(/item-row-selected/);
	await expect(rowB).not.toHaveClass(/item-row-selected/);
	await expect(rowC).not.toHaveClass(/item-row-selected/);
	await expect(page.locator('.selection-toolbar')).toHaveCount(0);
});

test('a click selects a row instead of opening it; double-click opens it (desktop)', async ({ page }) => {
	await page.goto('/');

	const folderName = `E2E Click Select ${Date.now()}`;
	await page.getByRole('button', { name: '+ New' }).click();
	await page.getByRole('menuitem', { name: 'New folder' }).click();
	await answerPrompt(page, folderName);
	const row = page.locator('.item-row', { hasText: folderName });
	await expect(row).toBeVisible();

	// A plain click on a real mouse selects, full stop — it never opens
	// anything on its own (see routes/+page.svelte's handleItemClick,
	// dispatched instead of the touch-only long-press flow whenever the
	// last pointerdown was a mouse).
	await row.click();
	await expect(page).toHaveURL('/'); // never navigated away
	await expect(page.locator('.selection-toolbar')).toContainText('1 selected');
	await expect(row).toHaveClass(/item-row-selected/);

	// A plain click on a *different*, unselected row replaces the
	// selection instead of adding to it — Ctrl+click is what adds.
	const secondFolderName = `E2E Click Select Second ${Date.now()}`;
	await page.getByRole('button', { name: '+ New' }).click();
	await page.getByRole('menuitem', { name: 'New folder' }).click();
	await answerPrompt(page, secondFolderName);
	const secondRow = page.locator('.item-row', { hasText: secondFolderName });
	await expect(secondRow).toBeVisible();

	await secondRow.click();
	await expect(page.locator('.selection-toolbar')).toContainText('1 selected');
	await expect(row).not.toHaveClass(/item-row-selected/);
	await expect(secondRow).toHaveClass(/item-row-selected/);

	// Double-click is what actually opens it.
	await secondRow.dblclick();
	await expect(page).toHaveURL(/\?folder=/);
});

test('shift+click selects a range, ctrl+click toggles individual rows', async ({ page }) => {
	await page.goto('/');

	// A fresh, empty folder first — shift's range depends on DOM order,
	// which the shared root (full of leftovers from every other test in
	// this file) can't guarantee for three specific names.
	const containerName = `E2E Range Container ${Date.now()}`;
	await page.getByRole('button', { name: '+ New' }).click();
	await page.getByRole('menuitem', { name: 'New folder' }).click();
	await answerPrompt(page, containerName);
	await page.locator('.item-row', { hasText: containerName }).dblclick();
	await expect(page).toHaveURL(/\?folder=/);

	const names = ['a-range', 'b-range', 'c-range'];
	for (const name of names) {
		await page.getByRole('button', { name: '+ New' }).click();
		await page.getByRole('menuitem', { name: 'New folder' }).click();
		await answerPrompt(page, name);
		await expect(page.locator('.item-row', { hasText: name })).toBeVisible();
	}
	// Default sort (name/ascending) plus these being the only three items
	// here guarantees this exact DOM order — a real prerequisite for a
	// shift-range test, not just a nicety.
	const [rowA, rowB, rowC] = names.map((name) => page.locator('.item-row', { hasText: name }));

	await rowA.click();
	await rowC.click({ modifiers: ['Shift'] });
	await expect(rowA).toHaveClass(/item-row-selected/);
	await expect(rowB).toHaveClass(/item-row-selected/);
	await expect(rowC).toHaveClass(/item-row-selected/);
	await expect(page.locator('.selection-toolbar')).toContainText('3 selected');

	// Ctrl+click on the middle one removes just that one from the range.
	await rowB.click({ modifiers: ['Control'] });
	await expect(rowB).not.toHaveClass(/item-row-selected/);
	await expect(rowA).toHaveClass(/item-row-selected/);
	await expect(rowC).toHaveClass(/item-row-selected/);
	await expect(page.locator('.selection-toolbar')).toContainText('2 selected');
});

test('dragging over empty space rubber-band-selects the rows it touches', async ({ page }) => {
	await page.goto('/');

	const containerName = `E2E Marquee Container ${Date.now()}`;
	await page.getByRole('button', { name: '+ New' }).click();
	await page.getByRole('menuitem', { name: 'New folder' }).click();
	await answerPrompt(page, containerName);
	await page.locator('.item-row', { hasText: containerName }).dblclick();
	await expect(page).toHaveURL(/\?folder=/);

	const names = ['a-marquee', 'b-marquee', 'c-marquee'];
	for (const name of names) {
		await page.getByRole('button', { name: '+ New' }).click();
		await page.getByRole('menuitem', { name: 'New folder' }).click();
		await answerPrompt(page, name);
		await expect(page.locator('.item-row', { hasText: name })).toBeVisible();
	}
	const [rowA, rowB, rowC] = names.map((name) => page.locator('.item-row', { hasText: name }));
	const boxA = await rowA.boundingBox();
	const boxB = await rowB.boundingBox();
	const boxC = await rowC.boundingBox();
	const dropzoneBox = await page.locator('.dropzone').boundingBox();
	if (!boxA || !boxB || !boxC || !dropzoneBox) throw new Error('rows/dropzone not found');

	// Only genuinely empty space (rows sit flush against each other, no
	// gap to drag-start from in between) is below the last row, and only
	// up to the dropzone's own bottom edge (its min-height, see app.css,
	// is generous but finite — a fixed offset below row C isn't
	// guaranteed to still land inside it). Drag from just above that edge
	// up to row B's own top edge: touches B and C, not A above them.
	const startY = dropzoneBox.y + dropzoneBox.height - 8;
	await page.mouse.move(boxC.x + 5, startY);
	await page.mouse.down();
	await page.mouse.move(boxB.x + 5, boxB.y + 2, { steps: 5 });
	await page.mouse.up();

	await expect(rowA).not.toHaveClass(/item-row-selected/);
	await expect(rowB).toHaveClass(/item-row-selected/);
	await expect(rowC).toHaveClass(/item-row-selected/);
	await expect(page.locator('.selection-toolbar')).toContainText('2 selected');
});

test('the native long-press context menu is suppressed on an item row', async ({ page }) => {
	// A real touch-and-hold on Android fires the browser's own
	// contextmenu event alongside our timer-based selection — left
	// unprevented, it leaves the gesture half-handled by native code and
	// every *subsequent* tap on another row stops registering (a real bug
	// hit on an actual phone; not reproducible via mouse-only interaction,
	// so it needs its own direct check here rather than another click-based
	// scenario like the test above).
	await page.goto('/');

	const folderName = `E2E Context Menu ${Date.now()}`;
	await page.getByRole('button', { name: '+ New' }).click();
	await page.getByRole('menuitem', { name: 'New folder' }).click();
	await answerPrompt(page, folderName);
	const row = page.locator('.item-row', { hasText: folderName });
	await expect(row).toBeVisible();

	const defaultPrevented = await row.locator('.item-name').evaluate((el) => {
		const event = new MouseEvent('contextmenu', { bubbles: true, cancelable: true });
		el.dispatchEvent(event);
		return event.defaultPrevented;
	});
	expect(defaultPrevented).toBe(true);
});

test('the bulk Share button opens a dialog scoped to the whole selection', async ({ page }) => {
	await page.goto('/');

	const nameA = uniqueName('bulk-share-a');
	const nameB = uniqueName('bulk-share-b');
	await page.locator('input[type="file"]').setInputFiles([
		{ name: nameA, mimeType: 'text/plain', buffer: Buffer.from('a') },
		{ name: nameB, mimeType: 'text/plain', buffer: Buffer.from('b') }
	]);
	await expect(page.locator('.item-row', { hasText: nameB })).toBeVisible({ timeout: 15_000 });

	await page.locator('.item-row', { hasText: nameA }).click();
	await page.locator('.item-row', { hasText: nameB }).click({ modifiers: ['Control'] });
	await page.locator('.selection-toolbar').getByRole('button', { name: 'Share' }).click();

	const dialog = page.locator('dialog.card[open]');
	await expect(dialog.getByRole('heading')).toHaveText('Share 2 items');
	await dialog.getByRole('button', { name: 'Cancel' }).click();
	await expect(dialog).not.toBeVisible();

	// Cancelling shares nothing — the selection itself is untouched too.
	await expect(page.locator('.selection-toolbar')).toContainText('2 selected');
});

test('bulk delete trashes every selected item, bulk move relocates every selected item together', async ({
	page
}) => {
	await page.goto('/');

	const destName = `Bulk Move Dest ${Date.now()}`;
	await page.getByRole('button', { name: '+ New' }).click();
	await page.getByRole('menuitem', { name: 'New folder' }).click();
	await answerPrompt(page, destName);
	await expect(page.locator('.item-row', { hasText: destName })).toBeVisible();

	const toDelete = uniqueName('to-delete');
	const toMoveA = uniqueName('to-move-a');
	const toMoveB = uniqueName('to-move-b');
	await page.locator('input[type="file"]').setInputFiles([
		{ name: toDelete, mimeType: 'text/plain', buffer: Buffer.from('x') },
		{ name: toMoveA, mimeType: 'text/plain', buffer: Buffer.from('y') },
		{ name: toMoveB, mimeType: 'text/plain', buffer: Buffer.from('z') }
	]);
	await expect(page.locator('.item-row', { hasText: toMoveB })).toBeVisible({ timeout: 15_000 });

	// --- bulk delete -----------------------------------------------------------------
	await page.locator('.item-row', { hasText: toDelete }).click();
	await page.locator('.selection-toolbar').getByRole('button', { name: 'Delete' }).click();
	await acceptConfirm(page);
	await expect(page.locator('.item-row', { hasText: toDelete })).toHaveCount(0);
	await expect(page.locator('.selection-toolbar')).toHaveCount(0); // selection clears after the bulk action

	// --- bulk move ---------------------------------------------------------------------
	await page.locator('.item-row', { hasText: toMoveA }).click();
	await page.locator('.item-row', { hasText: toMoveB }).click({ modifiers: ['Control'] });
	await page.locator('.selection-toolbar').getByRole('button', { name: 'Move' }).click();

	const dialog = page.locator('dialog.card[open]');
	await expect(dialog.getByRole('heading')).toHaveText('Move 2 items');
	await dialog
		.locator('.item-row', { hasText: destName })
		.getByRole('button', { name: destName, exact: true })
		.click();
	await dialog.getByRole('button', { name: 'Move here' }).click();
	await expect(dialog).not.toBeVisible();

	await expect(page.locator('.item-row', { hasText: toMoveA })).toHaveCount(0);
	await expect(page.locator('.item-row', { hasText: toMoveB })).toHaveCount(0);

	await page.locator('.item-row', { hasText: destName }).dblclick();
	await expect(page.locator('.item-row', { hasText: toMoveA })).toBeVisible();
	await expect(page.locator('.item-row', { hasText: toMoveB })).toBeVisible();
});

// A regression test for a real bug hit on an actual phone (not caught by
// a mouse-based test above, which never moves the target element under a
// stale tap the way an appearing-and-shifting layout can): the selection
// toolbar used to sit in normal document flow above the list, so the
// instant a first item got selected, the whole list jumped down
// underneath it. A real finger tapping a *second* row — aimed at wherever
// that row was a moment ago, before the shift, the way an actual person
// taps without re-checking mid-gesture — landed on nothing.
// Reproduced with real touch events (CDP Input.dispatchTouchEvent, not
// Playwright's mouse-based click()) against coordinates captured *before*
// the first selection, mimicking exactly that "aim once, tap twice"
// motion. Fixed by taking .selection-toolbar out of flow entirely
// (position: fixed) — this test pins that down so it can't regress. Touch
// still keeps the original long-press-to-select flow untouched by this
// pass's desktop click/shift/ctrl/drag model (see routes/+page.svelte's
// startLongPress — a real mouse skips its timer entirely now, but a touch
// pointer runs it exactly as before).
//
// Deliberately last in this file: it (like every test here) leaves items
// in the root folder behind, and the "select all" test above assumes it's
// the first to touch a clean root — moving this one after it avoids that
// pre-existing assumption breaking, rather than trying to fix it here too.
test('selecting a second row still works when tapping where it was *before* the first selection shifted anything', async ({
	browser
}) => {
	// storageState carries over the already-authenticated admin session the
	// "chromium" project's own context would otherwise provide — a fresh
	// context needs it explicitly since this test builds its own (for the
	// touch-capable device profile the shared context doesn't use).
	const context = await browser.newContext({
		...devices['Pixel 7'],
		hasTouch: true,
		isMobile: true,
		storageState: 'e2e/.auth/admin.json'
	});
	const page = await context.newPage();
	const cdp = await context.newCDPSession(page);

	async function touchTap(x: number, y: number) {
		await cdp.send('Input.dispatchTouchEvent', { type: 'touchStart', touchPoints: [{ x, y }] });
		await page.waitForTimeout(50);
		await cdp.send('Input.dispatchTouchEvent', { type: 'touchEnd', touchPoints: [] });
	}

	async function touchLongPress(x: number, y: number) {
		await cdp.send('Input.dispatchTouchEvent', { type: 'touchStart', touchPoints: [{ x, y }] });
		await page.waitForTimeout(700); // past the 500ms long-press threshold
		await cdp.send('Input.dispatchTouchEvent', { type: 'touchEnd', touchPoints: [] });
	}

	await page.goto('/');
	// A fresh, empty folder — not root — so rowA/rowB's screen position is
	// deterministic regardless of everything every other test in this
	// shared-backend file has already left lying around in root by the
	// time this one (deliberately last) runs. Via the FAB, not the
	// toolbar's own "+ New folder" (display:none below 640px — see
	// app.css — and this context's own device profile is phone-width).
	const folderName = `E2E Regress Shift ${Date.now()}`;
	await page.getByRole('button', { name: 'Add' }).click();
	await page.getByRole('menuitem', { name: 'New folder' }).click();
	await answerPrompt(page, folderName);
	// .tap(), not .click() — this context has hasTouch: true, but a plain
	// Playwright .click() still simulates a mouse regardless (see
	// routes/+page.svelte's startLongPress: a real mouse now skips the
	// long-press flow entirely in favor of instant click-select, which
	// would wrongly select this row instead of opening it here).
	await page.locator('.item-row', { hasText: folderName }).locator('.item-name').tap();
	await expect(page).toHaveURL(/\?folder=/);

	const nameA = `regress-shift-a-${Date.now()}.txt`;
	const nameB = `regress-shift-b-${Date.now()}.txt`;
	await page.locator('input[type="file"]').setInputFiles([
		{ name: nameA, mimeType: 'text/plain', buffer: Buffer.from('a') },
		{ name: nameB, mimeType: 'text/plain', buffer: Buffer.from('b') }
	]);
	const rowA = page.locator('.item-row', { hasText: nameA });
	const rowB = page.locator('.item-row', { hasText: nameB });
	await expect(rowA).toBeVisible({ timeout: 15_000 });
	await expect(rowB).toBeVisible({ timeout: 15_000 });

	// Captured once, before anything is selected — never recomputed, on
	// purpose, since a real finger doesn't re-measure the page mid-gesture.
	const staleBoxB = await rowB.locator('.item-name').boundingBox();
	if (!staleBoxB) throw new Error('row B not found before selecting anything');

	const boxA = await rowA.locator('.item-name').boundingBox();
	if (!boxA) throw new Error('row A not found');
	await touchLongPress(boxA.x + boxA.width / 2, boxA.y + boxA.height / 2);
	await expect(page.locator('.selection-toolbar')).toContainText('1');

	await touchTap(staleBoxB.x + staleBoxB.width / 2, staleBoxB.y + staleBoxB.height / 2);
	await expect(page.locator('.selection-toolbar')).toContainText('2');
	await expect(rowB.locator('.row-checkbox')).toBeChecked();

	await context.close();
});
