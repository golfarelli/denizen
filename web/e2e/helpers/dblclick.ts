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
		// Defensive: a stray open <dialog> (there are several always-mounted
		// ones — MoveDialog, ShareDialog, the global prompt/confirm dialog,
		// ...) intercepting pointer events would make dblclick() itself hang
		// retrying forever rather than failing fast — seen for real deep
		// into the full suite (2026-08-16). Escape is a native <dialog>'s own
		// default close behavior, harmless if nothing is actually open.
		await page.keyboard.press('Escape');
		// An explicit, short timeout here (unlike letting dblclick() use its
		// unbounded default) is what actually lets this retry loop cycle
		// through many attempts within the outer toPass budget below —
		// without it, one attempt stuck waiting on a blocking dialog just
		// hangs on its own, consuming the whole budget in a single "attempt"
		// that never even reaches the toPass retry logic.
		await locator.dblclick({ timeout: 3_000 });
		await expect(page).toHaveURL(expectedUrl, { timeout: 2_000 });
	}).toPass({ timeout: 20_000 });
}
