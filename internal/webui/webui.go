// Package webui embeds the built SvelteKit frontend (web/, built into
// dist/ by `npm run build` — see vite.config.ts) directly into the Go
// binary, so the single-container deployment (docs/ARCHITECTURE.md) has
// nothing to serve from outside itself.
//
// dist/'s real contents aren't checked into version control (they're
// generated — see .gitignore) and must exist before `go build`/`go test`
// run: `npm run build` inside web/ first, same as any other
// generated-then-embedded asset in this repo (see internal/db/db.go's own
// //go:embed migrations/*.sql for the go:embed-must-be-same-directory
// constraint that shaped this layout). Only a `dist/.gitkeep` placeholder
// is tracked, so go:embed —
// which fails to *compile* against a missing or fully empty directory —
// doesn't break a fresh clone before anyone has run the frontend build;
// Handler() below fails at runtime instead, with a clearer message, if
// that's the actual state it finds.
package webui

import (
	"embed"
	"fmt"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed all:dist
var distFS embed.FS

// FS returns the built frontend, rooted so that "index.html" and "_app/..."
// sit directly at its root (not behind a "dist/" prefix).
func FS() (fs.FS, error) {
	return fs.Sub(distFS, "dist")
}

// Handler serves the built frontend as a single-page app: a real file
// (e.g. anything under /_app/) is served as-is, but any other path falls
// back to index.html so the client-side router (not Go) decides what a
// path like /login or /?folder=... renders — there's no server-side route
// for those, it's a pure SPA (see web/src/routes/+layout.ts).
func Handler() (http.Handler, error) {
	files, err := FS()
	if err != nil {
		return nil, err
	}
	// Read once and serve the fallback directly, rather than rewriting the
	// request path to "/index.html" and delegating to http.FileServerFS —
	// that trips net/http's built-in "path ends in /index.html → redirect
	// to ./" behavior (meant to canonicalize literal index.html links),
	// which would send every deep link (e.g. /login) back to "/" and lose
	// the very path the client-side router needs to see.
	indexHTML, err := fs.ReadFile(files, "index.html")
	if err != nil {
		return nil, fmt.Errorf("webui: index.html not found in the embedded frontend — "+
			"run `npm run build` in web/ before building denizen: %w", err)
	}
	fileServer := http.FileServerFS(files)

	return http.HandlerFunc(func(res http.ResponseWriter, req *http.Request) {
		path := strings.TrimPrefix(req.URL.Path, "/")
		if path != "" {
			if _, err := fs.Stat(files, path); err == nil {
				fileServer.ServeHTTP(res, req)
				return
			}
		}
		res.Header().Set("Content-Type", "text/html; charset=utf-8")
		res.Write(indexHTML)
	}), nil
}
