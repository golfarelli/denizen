import { defineConfig, devices } from '@playwright/test';

// E2E tests run against the *real* Go binary (built fresh — see
// e2e/global-setup.ts — from whatever internal/webui/dist currently holds,
// which `npm run build` just wrote), the same artifact that gets deployed,
// not `npm run dev`'s proxy setup. This is the browser-side counterpart to
// the backend's own no-mocks flow tests (see CONTRIBUTING.md).
export const E2E_PORT = 4173;
export const E2E_BASE_URL = `http://localhost:${E2E_PORT}`;

export default defineConfig({
	testDir: './e2e',
	// All tests share one backend process and one SQLite database (spinning
	// up a fresh Go server per test would be far too slow) — running them
	// serially avoids two tests racing on that shared state. Individual
	// tests still don't depend on each other's *data* (item names that
	// might collide just get auto-suffixed by the backend, same as two
	// people using the real app) — only the ordering (setup must run first,
	// which the "setup" project dependency below already guarantees).
	fullyParallel: false,
	workers: 1,
	forbidOnly: !!process.env.CI,
	retries: process.env.CI ? 1 : 0,
	reporter: 'list',
	globalSetup: './e2e/global-setup.ts',

	use: {
		baseURL: E2E_BASE_URL,
		trace: 'retain-on-failure',
		acceptDownloads: true
	},

	projects: [
		{ name: 'setup', testMatch: /.*\.setup\.ts/ },
		{
			name: 'chromium',
			use: { ...devices['Desktop Chrome'], storageState: 'e2e/.auth/admin.json' },
			dependencies: ['setup']
		}
	]
});
