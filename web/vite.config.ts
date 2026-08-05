import adapter from '@sveltejs/adapter-static';
import { sveltekit } from '@sveltejs/kit/vite';
import { defineConfig } from 'vite';

export default defineConfig({
	plugins: [
		sveltekit({
			compilerOptions: {
				// Force runes mode for the project, except for libraries. Can be removed in svelte 6.
				runes: ({ filename }) =>
					filename.split(/[/\\]/).includes('node_modules') ? undefined : true
			},

			// Static SPA build: this app is served entirely from the Go binary
			// (go:embed — see docs/ARCHITECTURE.md's "single container" decision),
			// which has no Node runtime to run SvelteKit's own server. The
			// fallback makes every route resolve to index.html so client-side
			// routing works for a path Go doesn't otherwise recognize. Output
			// goes straight into internal/webui/dist — go:embed can only reach
			// files inside its own package directory (see internal/db/schema.sql
			// for the same constraint hit earlier), so this skips a separate
			// copy step between `npm run build` and `go build`.
			adapter: adapter({
				fallback: 'index.html',
				pages: '../internal/webui/dist',
				assets: '../internal/webui/dist'
			})
		})
	],
	server: {
		// `npm run dev` only serves this app — API calls need an actual Go
		// instance to talk to (`go run ./cmd/server` from the repo root,
		// default port 8080). Without this, every /api and /s request from
		// the dev server would 404 against Vite itself.
		proxy: {
			'/api': 'http://localhost:8080',
			'/s': 'http://localhost:8080'
		}
	}
});
