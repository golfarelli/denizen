import { execFileSync, spawn, type ChildProcess } from 'child_process';
import { mkdtempSync, openSync } from 'fs';
import { tmpdir } from 'os';
import path from 'path';
import { E2E_PORT, E2E_BASE_URL } from '../playwright.config';

// Builds and runs the actual denizen binary — not `go run` (which
// supervises a child process of its own and doesn't always relay a kill
// signal to it cleanly) and not `npm run dev` (serves the frontend only,
// proxying to some other Go instance — see vite.config.ts's proxy comment,
// meant for interactive development, not this). This is the real deployed
// artifact, built fresh from whatever `npm run build` just wrote into
// internal/webui/dist (see package.json's "test:e2e" script, which runs
// that first).
export default async function globalSetup() {
	const repoRoot = path.resolve(process.cwd(), '..');

	const binDir = mkdtempSync(path.join(tmpdir(), 'denizen-e2e-bin-'));
	const binaryPath = path.join(binDir, 'denizen-server');
	execFileSync('go', ['build', '-o', binaryPath, './cmd/server'], { cwd: repoRoot, stdio: 'inherit' });

	const dataDir = mkdtempSync(path.join(tmpdir(), 'denizen-e2e-data-'));
	const logPath = path.join(dataDir, 'server.log');
	const logFd = openSync(logPath, 'w');

	const serverProcess: ChildProcess = spawn(binaryPath, [], {
		env: {
			...process.env,
			DENIZEN_DATA_DIR: dataDir,
			DENIZEN_LISTEN_ADDR: `:${E2E_PORT}`,
			DENIZEN_JWT_SECRET: 'e2e-test-secret-not-for-production'
		},
		stdio: ['ignore', logFd, logFd]
	});

	await waitForServerReady(`${E2E_BASE_URL}/`, 30_000);

	// Handed to auth.setup.ts (a separate process — Playwright test workers
	// inherit the environment globalSetup ran in, which is how state
	// crosses that boundary here) so it can find the bootstrap invite code
	// main.go only ever logs, never serves over HTTP.
	process.env.DENIZEN_E2E_LOG_PATH = logPath;

	return async () => {
		serverProcess.kill('SIGTERM');
	};
}

async function waitForServerReady(url: string, timeoutMs: number): Promise<void> {
	const deadline = Date.now() + timeoutMs;
	while (Date.now() < deadline) {
		try {
			const res = await fetch(url);
			if (res.ok) return;
		} catch {
			// not listening yet
		}
		await new Promise((resolve) => setTimeout(resolve, 200));
	}
	throw new Error(`denizen server did not become ready at ${url} within ${timeoutMs}ms`);
}
