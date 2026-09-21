import { test, expect, devices, type Page, type Locator } from '@playwright/test';

// The scanner's crop step (lib/ScanDialog.svelte, ScanCropper.svelte,
// scanDetect.ts, scanWarp.ts): after each capture, the page's corners are
// found automatically, can be dragged into place, and the confirmed corners
// are what gets flattened into the page.
//
// The photos are synthetic — a light "sheet of paper" (with ruled lines) at
// a known, deliberately skewed position on a dark surface — so what the
// detector and the warp should produce is known exactly, without depending
// on a fixture that could be swapped for one that happens to work.

const W = 1200;
const H = 900;
// Skewed on purpose (not axis-aligned), so the perspective warp has real work to do.
const PAPER: [number, number][] = [
	[210, 150], // top-left
	[980, 190], // top-right
	[940, 760], // bottom-right
	[170, 700] // bottom-left
];

async function makePhoto(page: Page, paper: [number, number][] | null, surface = '#2b2b2b'): Promise<Buffer> {
	const dataUrl = await page.evaluate(
		({ w, h, quad, surface }) => {
			const c = document.createElement('canvas');
			c.width = w;
			c.height = h;
			const g = c.getContext('2d')!;
			g.fillStyle = surface;
			g.fillRect(0, 0, w, h);
			if (quad) {
				g.fillStyle = '#ecece4';
				g.beginPath();
				quad.forEach(([x, y], i) => (i ? g.lineTo(x, y) : g.moveTo(x, y)));
				g.closePath();
				g.fill();
				// Ruled lines inside the sheet, so it isn't a flat colour.
				g.strokeStyle = '#8a8a8a';
				g.lineWidth = 3;
				const [tl, tr, br, bl] = quad;
				for (let i = 1; i < 12; i++) {
					const t = i / 12;
					g.beginPath();
					g.moveTo(tl[0] + (bl[0] - tl[0]) * t + 30, tl[1] + (bl[1] - tl[1]) * t);
					g.lineTo(tr[0] + (br[0] - tr[0]) * t - 30, tr[1] + (br[1] - tr[1]) * t);
					g.stroke();
				}
			}
			return c.toDataURL('image/jpeg', 0.92);
		},
		{ w: W, h: H, quad: paper, surface }
	);
	return Buffer.from(dataUrl.split(',')[1], 'base64');
}

async function openScanner(page: Page): Promise<Locator> {
	await page.goto('/');
	// On a phone-sized screen the sidebar's "+ New" isn't there: the floating
	// button takes over.
	const fab = page.locator('.fab');
	if (await fab.isVisible()) await fab.click();
	else await page.getByRole('button', { name: '+ New' }).click();
	await page.getByRole('menuitem', { name: 'Scan' }).click();
	const dialog = page.locator('dialog.card[open]');
	await expect(dialog).toBeVisible();
	return dialog;
}

async function capture(dialog: Locator, photo: Buffer) {
	await dialog.getByRole('button', { name: '+ Add page' }).click();
	await dialog.locator('input[type="file"]').setInputFiles({ name: 'photo.jpg', mimeType: 'image/jpeg', buffer: photo });
	await expect(dialog.locator('.scan-cropper')).toBeVisible();
}

// Waits out detection (the first scan loads OpenCV's WebAssembly build).
async function detectionSettled(dialog: Locator) {
	await expect(dialog.locator('.hint[data-detecting="false"]')).toBeVisible({ timeout: 60_000 });
}

async function corner(dialog: Locator, name: 'tl' | 'tr' | 'br' | 'bl'): Promise<[number, number]> {
	const handle = dialog.locator(`[data-corner="${name}"]`);
	return [Number(await handle.getAttribute('data-x')), Number(await handle.getAttribute('data-y'))];
}

test.describe('scanner crop step', () => {
	test.setTimeout(120_000);

	test('finds the page automatically, and the confirmed page is the flattened sheet', async ({ page }) => {
		const dialog = await openScanner(page);
		await capture(dialog, await makePhoto(page, PAPER));
		await detectionSettled(dialog);

		await expect(dialog.locator('.hint')).toHaveAttribute('data-detected', 'true');
		// Detection runs on a downscaled copy, so allow a few dozen px of slack.
		const names = ['tl', 'tr', 'br', 'bl'] as const;
		for (let i = 0; i < 4; i++) {
			const [x, y] = await corner(dialog, names[i]);
			expect(Math.abs(x - PAPER[i][0]), `${names[i]} x`).toBeLessThan(40);
			expect(Math.abs(y - PAPER[i][1]), `${names[i]} y`).toBeLessThan(40);
		}

		await dialog.getByRole('button', { name: 'Use this page' }).click();
		const thumb = dialog.locator('.scan-page-thumb img');
		await expect(thumb).toHaveCount(1);

		// The page is the sheet flattened: roughly its own aspect ratio (772 x
		// 571 for this quad) and almost entirely light paper, not the dark
		// surface around it.
		const result = await thumb.evaluate(async (img: HTMLImageElement) => {
			await img.decode();
			const c = document.createElement('canvas');
			c.width = img.naturalWidth;
			c.height = img.naturalHeight;
			const g = c.getContext('2d')!;
			g.drawImage(img, 0, 0);
			const d = g.getImageData(0, 0, c.width, c.height).data;
			let sum = 0;
			for (let i = 0; i < d.length; i += 4) sum += (d[i] + d[i + 1] + d[i + 2]) / 3;
			return { w: img.naturalWidth, h: img.naturalHeight, mean: sum / (d.length / 4) };
		});
		expect(result.w / result.h).toBeGreaterThan(1.28);
		expect(result.w / result.h).toBeLessThan(1.42);
		expect(result.mean).toBeGreaterThan(170); // the dark surface would drag this far below
	});

	test('corners can be dragged, Reset puts them back, and a moved corner survives late detection', async ({ page }) => {
		const dialog = await openScanner(page);
		await capture(dialog, await makePhoto(page, PAPER));

		// Drag the top-left corner straight away — before detection can have
		// finished the first time OpenCV loads — to a spot the detector would
		// never pick.
		const box = (await dialog.locator('.scan-cropper svg').boundingBox())!;
		const target: [number, number] = [90, 60]; // in photo pixels
		const toScreen = ([x, y]: [number, number]) => [box.x + (x / W) * box.width, box.y + (y / H) * box.height] as const;
		const [startX, startY] = toScreen(await corner(dialog, 'tl'));
		const [endX, endY] = toScreen(target);
		await page.mouse.move(startX, startY);
		await page.mouse.down();
		await page.mouse.move(endX, endY, { steps: 8 });
		await page.mouse.up();

		const moved = await corner(dialog, 'tl');
		expect(Math.abs(moved[0] - target[0])).toBeLessThan(8);
		expect(Math.abs(moved[1] - target[1])).toBeLessThan(8);

		// Detection lands afterwards and must not undo the user's own adjustment.
		await detectionSettled(dialog);
		const afterDetection = await corner(dialog, 'tl');
		expect(Math.abs(afterDetection[0] - target[0])).toBeLessThan(8);
		expect(Math.abs(afterDetection[1] - target[1])).toBeLessThan(8);

		// Reset returns to the detected corners.
		await dialog.getByRole('button', { name: 'Reset corners' }).click();
		const [rx, ry] = await corner(dialog, 'tl');
		expect(Math.abs(rx - PAPER[0][0])).toBeLessThan(40);
		expect(Math.abs(ry - PAPER[0][1])).toBeLessThan(40);
	});

	// A sheet on a light wooden table: far less contrast than the dark surface
	// above, closer to a real photo.
	test('finds a light page on a light surface', async ({ page }) => {
		const dialog = await openScanner(page);
		await capture(dialog, await makePhoto(page, PAPER, '#a8916f'));
		await detectionSettled(dialog);
		await expect(dialog.locator('.hint')).toHaveAttribute('data-detected', 'true');
		const [x, y] = await corner(dialog, 'tl');
		expect(Math.abs(x - PAPER[0][0])).toBeLessThan(40);
		expect(Math.abs(y - PAPER[0][1])).toBeLessThan(40);
	});

	test('keyboard nudges a corner', async ({ page }) => {
		const dialog = await openScanner(page);
		await capture(dialog, await makePhoto(page, PAPER));
		await detectionSettled(dialog);

		const before = await corner(dialog, 'br');
		await dialog.locator('[data-corner="br"]').focus();
		await page.keyboard.press('ArrowLeft');
		await page.keyboard.press('ArrowUp');
		const after = await corner(dialog, 'br');
		expect(after[0]).toBeLessThan(before[0]);
		expect(after[1]).toBeLessThan(before[1]);
	});

	test('with no page to find, the corners start as a default inset and say so', async ({ page }) => {
		const dialog = await openScanner(page);
		await capture(dialog, await makePhoto(page, null)); // a uniform dark frame
		await detectionSettled(dialog);

		await expect(dialog.locator('.hint')).toHaveAttribute('data-detected', 'false');
		await expect(dialog.locator('.hint')).toContainText('Couldn’t find the page automatically');
		const [x, y] = await corner(dialog, 'tl');
		expect(Math.abs(x - W * 0.06)).toBeLessThan(3);
		expect(Math.abs(y - H * 0.06)).toBeLessThan(3);

		// Still perfectly usable by hand.
		await dialog.getByRole('button', { name: 'Use this page' }).click();
		await expect(dialog.locator('.scan-page-thumb')).toHaveCount(1);
	});

	test('Retake discards the photo and asks for another', async ({ page }) => {
		const dialog = await openScanner(page);
		await capture(dialog, await makePhoto(page, PAPER));

		const chooserPromise = page.waitForEvent('filechooser');
		await dialog.getByRole('button', { name: 'Retake' }).click();
		const chooser = await chooserPromise;
		// The first photo is gone while the camera/picker is open.
		await expect(dialog.locator('.scan-cropper')).toHaveCount(0);

		await chooser.setFiles({ name: 'again.jpg', mimeType: 'image/jpeg', buffer: await makePhoto(page, PAPER) });
		await expect(dialog.locator('.scan-cropper')).toBeVisible();
		await dialog.getByRole('button', { name: 'Use this page' }).click();
		await expect(dialog.locator('.scan-page-thumb')).toHaveCount(1);
	});

	// The scanner is used mostly on a phone: a finger must be able to grab a
	// corner and move it (touch-action: none on the cropper, pointer events).
	test('a corner can be dragged with a finger on a touch device', async ({ browser }) => {
		const context = await browser.newContext({
			...devices['Pixel 7'],
			hasTouch: true,
			isMobile: true,
			storageState: 'e2e/.auth/admin.json'
		});
		const page = await context.newPage();
		const cdp = await context.newCDPSession(page);
		const dialog = await openScanner(page);
		await capture(dialog, await makePhoto(page, PAPER));

		const box = (await dialog.locator('.scan-cropper svg').boundingBox())!;
		const toScreen = ([x, y]: [number, number]) => ({ x: box.x + (x / W) * box.width, y: box.y + (y / H) * box.height });
		const from = toScreen(await corner(dialog, 'br'));
		const targetPhoto: [number, number] = [1100, 850];
		const to = toScreen(targetPhoto);

		await cdp.send('Input.dispatchTouchEvent', { type: 'touchStart', touchPoints: [from] });
		for (let i = 1; i <= 6; i++) {
			const p = { x: from.x + ((to.x - from.x) * i) / 6, y: from.y + ((to.y - from.y) * i) / 6 };
			await cdp.send('Input.dispatchTouchEvent', { type: 'touchMove', touchPoints: [p] });
		}
		await cdp.send('Input.dispatchTouchEvent', { type: 'touchEnd', touchPoints: [] });

		const moved = await corner(dialog, 'br');
		expect(Math.abs(moved[0] - targetPhoto[0])).toBeLessThan(12);
		expect(Math.abs(moved[1] - targetPhoto[1])).toBeLessThan(12);
		await context.close();
	});
});
