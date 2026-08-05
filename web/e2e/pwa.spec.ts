import { test, expect } from '@playwright/test';

test('manifest.json is valid and lists real, fetchable icons', async ({ page, request }) => {
	const res = await request.get('/manifest.json');
	expect(res.ok()).toBeTruthy();
	const manifest = await res.json();

	expect(manifest.name).toBe('Denizen');
	expect(manifest.display).toBe('standalone');
	expect(Array.isArray(manifest.icons)).toBe(true);
	expect(manifest.icons.length).toBeGreaterThan(0);

	for (const icon of manifest.icons) {
		const iconRes = await request.get(icon.src);
		expect(iconRes.ok(), `icon ${icon.src} should be fetchable`).toBeTruthy();
	}

	expect(manifest.share_target.action).toBe('/share-target');
	expect(manifest.share_target.method).toBe('POST');

	// Not exercised by this test (nothing loaded the app), just confirming
	// the tag that wires the manifest into the page is actually there.
	await page.goto('/login');
	await expect(page.locator('link[rel="manifest"]')).toHaveAttribute('href', '/manifest.json');
});

test('the service worker registers and takes control of the page', async ({ page }) => {
	await page.goto('/');
	await page.evaluate(() => navigator.serviceWorker.ready);
	// The first load after a registration happens is never itself
	// controlled by that worker (see sw.js's activate handler and its own
	// comment on clients.claim()) — a fresh navigation after it's active is.
	await page.reload();

	const controlled = await page.evaluate(() => !!navigator.serviceWorker.controller);
	expect(controlled).toBe(true);
});

test('sharing a file from another app lands as a real authenticated upload', async ({ page }) => {
	await page.goto('/');
	await page.evaluate(() => navigator.serviceWorker.ready);
	await page.reload();

	// Simulates exactly what the OS share sheet does to an installed PWA: a
	// same-origin POST with multipart form data. sw.js's fetch handler is
	// what actually intercepts this — nothing in this test talks to it
	// directly, it's a real fetch() from the page, same as the browser
	// would issue on a real share.
	const shareId = await page.evaluate(async () => {
		const file = new File([new Uint8Array([1, 2, 3, 4])], 'shared-note.txt', { type: 'text/plain' });
		const formData = new FormData();
		formData.append('files', file);
		const res = await fetch('/share-target', { method: 'POST', body: formData });
		return new URL(res.url).searchParams.get('share_id');
	});
	expect(shareId).toBeTruthy();

	await page.goto(`/share-target?share_id=${shareId}`);
	await expect(page.getByRole('heading', { name: 'Share to Denizen' })).toBeVisible();
	await expect(page.getByText('shared-note.txt')).toBeVisible();

	await page.getByRole('button', { name: 'Upload' }).click();
	await expect(page).toHaveURL('/');
	await expect(page.locator('.item-row', { hasText: 'shared-note.txt' })).toBeVisible({ timeout: 15_000 });
});
