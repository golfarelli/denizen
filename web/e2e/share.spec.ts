import { test, expect } from '@playwright/test';
import path from 'path';
import { readFileSync } from 'fs';
import { forceEnglishLocale } from './helpers/locale';
import { answerPrompt, acceptConfirm } from './helpers/dialog';

const FIXTURE_PATH = path.join(import.meta.dirname, 'fixtures', 'sample.txt');
const PDF_BYTES = readFileSync(path.join(import.meta.dirname, 'fixtures', 'sample.pdf'));

// A genuinely blank context — browser.newContext() with no options here
// inherits this *project's* own storageState (e2e/.auth/admin.json, see
// playwright.config.ts), which defeats the whole point of "a visitor with
// no account" for anything gated by auth. Found the hard way: a
// requires_auth share appeared to work for an "anonymous" visitor only
// because that visitor was secretly still the admin.
const ANONYMOUS = { storageState: { cookies: [], origins: [] } };

let uploadCounter = 0;
// A unique name per share, not the fixture's own literal name — several
// tests below share the same underlying bytes, and the backend
// auto-suffixes on a name collision, which would otherwise make ".item-row
// { hasText: ... }" ambiguous between two different tests' rows.
function uniqueName(ext: string): string {
	uploadCounter += 1;
	return `share-test-${uploadCounter}-${Date.now()}.${ext}`;
}

async function shareNewFile(page: import('@playwright/test').Page): Promise<string> {
	const name = uniqueName('pdf');
	await page.goto('/');
	await page.locator('input[type="file"]').setInputFiles({ name, mimeType: 'application/pdf', buffer: PDF_BYTES });
	const row = page.locator('.item-row', { hasText: name });
	await expect(row).toBeVisible({ timeout: 15_000 });
	await row.getByRole('button', { name: `Actions for ${name}` }).click();
	await row.locator('.dropdown-menu').getByRole('menuitem', { name: 'Share' }).click();
	return name;
}

test('share a file, a visitor with no account can fetch it, then revoking the link cuts them off', async ({
	page
}) => {
	await page.goto('/');
	await page.locator('input[type="file"]').setInputFiles(FIXTURE_PATH);

	const fileRow = page.locator('.item-row', { hasText: 'sample.txt' });
	await expect(fileRow).toBeVisible({ timeout: 15_000 });

	await fileRow.getByRole('button', { name: 'Actions for' }).click();
	await fileRow.locator('.dropdown-menu').getByRole('menuitem', { name: 'Share' }).click();
	await page.getByRole('button', { name: 'Create link' }).click();

	const linkInput = page.locator('dialog input[readonly]');
	await expect(linkInput).toBeVisible();
	const shareUrl = await linkInput.inputValue();
	expect(shareUrl).toContain('/s/');

	await page.getByRole('button', { name: 'Done' }).click();

	// The bare share URL — what actually gets copied/sent to someone — has
	// to reach Denizen's own landing page (routes/s/[token]/+page.svelte),
	// not the raw JSON metadata endpoint (that one lives at .../meta, a
	// separate path specifically so this doesn't happen — see router.New
	// and the frontend's own +layout.svelte guard for /s/ paths). This is
	// the actual regression test for "share links open JSON, not the
	// file": a real page.goto, not just an API assertion.
	await page.goto(shareUrl);
	await expect(page.locator('.preview-title')).toHaveText('sample.txt');
	await expect(page.getByRole('button', { name: 'Download' })).toBeVisible();
	// The real regression check for "opens JSON, not the file": actual
	// file content rendered inline (this page's own full preview, same as
	// the private file page), not a `{"name":...}` blob dumped as plain
	// text.
	await expect(page.locator('.preview-text')).toHaveText(readFileSync(FIXTURE_PATH, 'utf8'));
	await expect(page.getByText('{"name"', { exact: false })).toHaveCount(0);

	// page.request never attaches this app's Authorization header (that's
	// added by our own fetch wrapper in lib/api.ts, not something a browser
	// does on its own) — it's naturally the same as a visitor with no
	// Denizen account at all, exactly who these public /s/ routes are for.
	const metaRes = await page.request.get(`${shareUrl}/meta`);
	expect(metaRes.ok()).toBeTruthy();
	const meta = await metaRes.json();
	expect(meta).toMatchObject({ name: 'sample.txt', type: 'file' });

	const contentRes = await page.request.get(`${shareUrl}/content`);
	expect(contentRes.ok()).toBeTruthy();
	const downloadedBody = await contentRes.body();
	expect(downloadedBody.equals(readFileSync(FIXTURE_PATH))).toBe(true);

	// --- revoking it from "My shares" actually cuts the visitor off ------------
	await page.goto('/shares');
	const shareRow = page.locator('.item-row', { hasText: 'sample.txt' });
	await expect(shareRow).toBeVisible();

	await shareRow.getByRole('button', { name: 'Actions for' }).click();
	await shareRow.locator('.dropdown-menu').getByRole('menuitem', { name: 'Revoke' }).click();
	await acceptConfirm(page);
	await expect(shareRow).not.toBeVisible();

	const afterRevokeRes = await page.request.get(`${shareUrl}/meta`);
	expect(afterRevokeRes.status()).toBe(404);
});

test('the landing page\'s own Download button fetches real bytes, for a visitor with no account', async ({
	page
}) => {
	await shareNewFile(page);
	await page.getByRole('button', { name: 'Create link' }).click();
	const shareUrl = await page.locator('dialog input[readonly]').inputValue();

	const visitor = await page.context().browser()!.newContext(ANONYMOUS);
	await forceEnglishLocale(visitor);
	const visitorPage = await visitor.newPage();
	await visitorPage.goto(shareUrl);
	const [download] = await Promise.all([
		visitorPage.waitForEvent('download'),
		visitorPage.getByRole('button', { name: 'Download' }).click()
	]);
	const downloadPath = await download.path();
	expect(downloadPath).toBeTruthy();
	expect(readFileSync(downloadPath!).equals(PDF_BYTES)).toBe(true);
	await visitor.close();
});

test('a revoked share\'s landing page says so instead of erroring', async ({ page }) => {
	const name = await shareNewFile(page);
	await page.getByRole('button', { name: 'Create link' }).click();
	const shareUrl = await page.locator('dialog input[readonly]').inputValue();
	await page.getByRole('button', { name: 'Done' }).click();

	await page.goto('/shares');
	const shareRow = page.locator('.item-row', { hasText: name });
	await expect(shareRow).toBeVisible();
	await shareRow.getByRole('button', { name: 'Actions for' }).click();
	await shareRow.locator('.dropdown-menu').getByRole('menuitem', { name: 'Revoke' }).click();
	await acceptConfirm(page);
	await expect(shareRow).toBeHidden();

	const visitor = await page.context().browser()!.newContext(ANONYMOUS);
	await forceEnglishLocale(visitor);
	const visitorPage = await visitor.newPage();
	await visitorPage.goto(shareUrl);
	await expect(visitorPage.getByText('This link is no longer available.')).toBeVisible();
	await visitor.close();
});

test('a requires_auth share prompts an anonymous visitor to log in, then lands back on the share', async ({
	page
}) => {
	await shareNewFile(page);
	await page.getByRole('checkbox', { name: 'Require the visitor to be logged in' }).check();
	await page.getByRole('button', { name: 'Create link' }).click();
	const shareUrl = await page.locator('dialog input[readonly]').inputValue();

	const visitor = await page.context().browser()!.newContext(ANONYMOUS);
	await forceEnglishLocale(visitor);
	const visitorPage = await visitor.newPage();
	await visitorPage.goto(shareUrl);
	await expect(visitorPage.getByText(/requires you to be logged in/)).toBeVisible();
	const loginLink = visitorPage.getByRole('link', { name: 'Log in' });
	await expect(loginLink).toBeVisible();
	await loginLink.click();
	// Lands on /login carrying this share's own URL as the post-login
	// redirect target (routes/login/+page.svelte's redirectTarget) — not
	// just any /login, one that'll actually return the visitor here.
	await expect(visitorPage).toHaveURL(/\/login\?then=/);
	await visitor.close();
});

test('sharing a folder shows its name but no broken Download button', async ({ page }) => {
	await page.goto('/');
	const folderName = `E2E Share Folder ${Date.now()}`;
	await page.getByRole('button', { name: '+ New' }).click();
	await page.getByRole('menuitem', { name: 'New folder' }).click();
	await answerPrompt(page, folderName);

	const folderRow = page.locator('.item-row', { hasText: folderName });
	await expect(folderRow).toBeVisible();
	await folderRow.getByRole('button', { name: `Actions for ${folderName}` }).click();
	await folderRow.locator('.dropdown-menu').getByRole('menuitem', { name: 'Share' }).click();
	await page.getByRole('button', { name: 'Create link' }).click();
	const shareUrl = await page.locator('dialog input[readonly]').inputValue();

	const visitor = await page.context().browser()!.newContext(ANONYMOUS);
	await forceEnglishLocale(visitor);
	const visitorPage = await visitor.newPage();
	await visitorPage.goto(shareUrl);
	await expect(visitorPage.getByRole('heading', { name: folderName })).toBeVisible();
	await expect(visitorPage.getByRole('button', { name: 'Download' })).toHaveCount(0);
	await visitor.close();
});

test('a shared PDF renders a full inline preview, not just a name/size card', async ({ page }) => {
	await shareNewFile(page);
	await page.getByRole('button', { name: 'Create link' }).click();
	const shareUrl = await page.locator('dialog input[readonly]').inputValue();

	const visitor = await page.context().browser()!.newContext(ANONYMOUS);
	await forceEnglishLocale(visitor);
	const visitorPage = await visitor.newPage();
	await visitorPage.goto(shareUrl);

	// Same real-pixels check preview.spec.ts uses for the private page's
	// own PDF viewer — a blank-but-present canvas would be exactly what a
	// broken/stubbed-out public viewer would produce.
	const canvas = visitorPage.locator('.pdf-pages canvas.pdf-page').first();
	await expect(canvas).toBeVisible({ timeout: 10_000 });
	const hasContent = await canvas.evaluate((el: HTMLCanvasElement) => {
		const ctx = el.getContext('2d');
		if (!ctx) return false;
		const { data } = ctx.getImageData(0, 0, el.width, el.height);
		for (let i = 0; i < data.length; i += 4) {
			if (data[i] !== 255 || data[i + 1] !== 255 || data[i + 2] !== 255) return true;
		}
		return false;
	});
	expect(hasContent).toBe(true);
	await visitor.close();
});
