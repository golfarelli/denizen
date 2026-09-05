import type { Page } from '@playwright/test';

// Fills and confirms the app's custom prompt dialog (lib/GlobalDialog.svelte,
// replacing window.prompt()). Call right
// after whatever opens it (a "New folder"/"Rename" menu item, ...) instead
// of the old page.once('dialog', ...) pattern, which stopped firing once
// there was no more native browser dialog to catch.
export async function answerPrompt(page: Page, value: string): Promise<void> {
	const dialog = page.locator('dialog[open]');
	await dialog.locator('input[type="text"]').fill(value);
	await dialog.locator('.dialog-actions button').last().click();
}

// Confirms the app's custom confirm dialog (replacing window.confirm()).
export async function acceptConfirm(page: Page): Promise<void> {
	const dialog = page.locator('dialog[open]');
	await dialog.locator('.dialog-actions button').last().click();
}

// Cancels either dialog above via its own Cancel button, same effect as
// window.prompt()/confirm() returning null/false.
export async function cancelDialog(page: Page): Promise<void> {
	const dialog = page.locator('dialog[open]');
	await dialog.locator('.dialog-actions button').first().click();
}
