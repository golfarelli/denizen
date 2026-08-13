import { test, expect } from '@playwright/test';

// "+ New" → "Word document" / "Spreadsheet" / "Presentation": creates a real
// blank OOXML file server-side and navigates straight into the preview page
// (which picks OnlyOffice up automatically when configured — not exercised
// here since the e2e environment runs without a Document Server, same as
// preview.spec.ts's docx/xlsx tests).

const CASES = [
	{ menuItem: 'Word document', defaultName: 'Untitled document.docx' },
	{ menuItem: 'Spreadsheet', defaultName: 'Untitled spreadsheet.xlsx' },
	{ menuItem: 'Presentation', defaultName: 'Untitled presentation.pptx' }
];

for (const { menuItem, defaultName } of CASES) {
	test(`create a blank ${menuItem} and land in its preview`, async ({ page }) => {
		await page.goto('/');
		await page.getByRole('button', { name: '+ New' }).click();
		await page.getByRole('menuitem', { name: menuItem, exact: true }).click();

		await expect(page).toHaveURL(/\/file\/.+/);
		await expect(page.locator('.preview-title')).toHaveText(defaultName);

		await page.locator('.preview-back').click();
		await expect(page.locator('.item-row', { hasText: defaultName })).toBeVisible();
	});
}

test('a second blank Word document in the same folder gets a disambiguated name', async ({ page }) => {
	await page.goto('/');

	await page.getByRole('button', { name: '+ New' }).click();
	await page.getByRole('menuitem', { name: 'Word document', exact: true }).click();
	await expect(page).toHaveURL(/\/file\/.+/);
	await page.locator('.preview-back').click();

	await page.getByRole('button', { name: '+ New' }).click();
	await page.getByRole('menuitem', { name: 'Word document', exact: true }).click();
	await expect(page).toHaveURL(/\/file\/.+/);
	const secondTitle = await page.locator('.preview-title').textContent();
	expect(secondTitle).not.toBe('Untitled document.docx');
	expect(secondTitle).toContain('Untitled document');
});
