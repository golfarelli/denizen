import type { BrowserContext } from '@playwright/test';

// Denizen defaults to Italian (see lib/i18n) — every assertion in this
// suite checks against the English source strings, not the translations,
// so every fresh browser context needs English forced before its first
// page load. auth.setup.ts does this once for the storageState snapshot
// that most tests inherit (see playwright.config.ts's project-level
// storageState); any test that spins up its own separate context (an
// anonymous visitor, a second invited user, ...) starts blank and needs
// this called on it directly, before that context's first goto().
export async function forceEnglishLocale(context: BrowserContext): Promise<void> {
	await context.addInitScript(() => localStorage.setItem('denizen.locale', 'en'));
}
