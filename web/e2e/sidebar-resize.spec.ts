import { test, expect, type Page } from '@playwright/test';

// The desktop sidebar's drag handle (routes/+layout.svelte's
// .sidebar-resizer, lib/sidebarWidth.ts): resizable within a floor and a
// ceiling, remembered for the current browser session only (sessionStorage —
// a new tab starts from the default again), keyboard-operable, and absent
// below the drawer breakpoint where the sidebar is a slide-in drawer.

const DEFAULT_WIDTH = 248;
const MIN = 200;
const MAX = 420;

async function sidebarWidth(page: Page): Promise<number> {
	const box = await page.locator('.sidebar').boundingBox();
	return Math.round(box!.width);
}

// Drag the handle horizontally to viewport x, like a user would.
async function dragHandleTo(page: Page, x: number) {
	const box = await page.locator('.sidebar-resizer').boundingBox();
	const startX = box!.x + box!.width / 2;
	const y = box!.y + 300;
	await page.mouse.move(startX, y);
	await page.mouse.down();
	await page.mouse.move(x, y, { steps: 8 });
	await page.mouse.up();
}

test('sidebar resize: drag within limits, min/max clamp, session-only memory, keyboard', async ({
	page,
	context
}) => {
	await page.goto('/');
	await expect(page.locator('.sidebar-resizer')).toBeVisible();
	expect(await sidebarWidth(page)).toBe(DEFAULT_WIDTH);

	// A plain drag lands the sidebar's right edge where the pointer was
	// released, and the main content column follows (it's flex: 1).
	await dragHandleTo(page, 340);
	expect(Math.abs((await sidebarWidth(page)) - 340)).toBeLessThanOrEqual(2);

	// Dragging far past either limit stops at it — the floor is what keeps
	// folder names and the tree's indentation readable.
	await dragHandleTo(page, 40);
	expect(await sidebarWidth(page)).toBe(MIN);
	await dragHandleTo(page, 1000);
	expect(await sidebarWidth(page)).toBe(MAX);

	// Survives a reload in the same tab (sessionStorage)...
	await page.reload();
	await expect(page.locator('.sidebar-resizer')).toBeVisible();
	expect(await sidebarWidth(page)).toBe(MAX);

	// ...but not a fresh tab: session-only, unlike view mode/sort/language.
	const otherTab = await context.newPage();
	await otherTab.goto('/');
	await expect(otherTab.locator('.sidebar-resizer')).toBeVisible();
	expect(await sidebarWidth(otherTab)).toBe(DEFAULT_WIDTH);
	await otherTab.close();

	// Keyboard: the handle is a focusable separator — arrows nudge by 16px,
	// Home/End jump to the limits.
	await dragHandleTo(page, 300);
	const before = await sidebarWidth(page);
	await page.locator('.sidebar-resizer').focus();
	await page.keyboard.press('ArrowRight');
	expect(await sidebarWidth(page)).toBe(before + 16);
	await page.keyboard.press('ArrowLeft');
	await page.keyboard.press('ArrowLeft');
	expect(await sidebarWidth(page)).toBe(before - 16);
	await page.keyboard.press('Home');
	expect(await sidebarWidth(page)).toBe(MIN);
	await page.keyboard.press('End');
	expect(await sidebarWidth(page)).toBe(MAX);
});

test('sidebar resize: no handle below the drawer breakpoint, and a desktop width never leaks into the drawer', async ({
	page
}) => {
	await page.goto('/');
	await dragHandleTo(page, 380);
	expect(Math.abs((await sidebarWidth(page)) - 380)).toBeLessThanOrEqual(2);

	// Shrinking the window below the breakpoint turns the sidebar into the
	// slide-in drawer: no handle, and the default drawer width (not the
	// dragged one) applies.
	await page.setViewportSize({ width: 600, height: 800 });
	await expect(page.locator('.sidebar-resizer')).toBeHidden();
	expect(await sidebarWidth(page)).toBe(DEFAULT_WIDTH);

	// And growing back restores the dragged width.
	await page.setViewportSize({ width: 1280, height: 720 });
	await expect(page.locator('.sidebar-resizer')).toBeVisible();
	expect(Math.abs((await sidebarWidth(page)) - 380)).toBeLessThanOrEqual(2);
});
