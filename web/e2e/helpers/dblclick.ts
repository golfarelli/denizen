import type { Locator, Page } from '@playwright/test';
import { expect } from '@playwright/test';

// A real double-click, synthesized from two raw click events close enough
// together in wall-clock time, occasionally doesn't land as such under
// heavier load (running late in a long suite, with a lot of accumulated
// DOM/reactivity from every prior test sharing this backend) — the second
// click's dispatch gets delayed enough by a busy main thread that the
// browser's own double-click window closes. Retrying the whole gesture
// (not just re-querying the element) is the standard fix for this specific
// class of flakiness; toPass() is Playwright's own mechanism for it.
export async function openViaDblclick(page: Page, locator: Locator, expectedUrl: RegExp): Promise<void> {
	await expect(async () => {
		await locator.dblclick();
		await expect(page).toHaveURL(expectedUrl, { timeout: 2_000 });
	}).toPass({ timeout: 10_000 });
}
