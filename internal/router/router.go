// Package router wires HTTP routes to handlers, using the standard
// library's pattern-matching ServeMux (Go 1.22+) — no router dependency
// needed for this.
package router

import (
	"net/http"

	"github.com/golfarelli/denizen/internal/handler"
	"github.com/golfarelli/denizen/internal/middleware"
	"github.com/golfarelli/denizen/internal/token"
	"github.com/golfarelli/denizen/internal/upload"
	"github.com/golfarelli/denizen/internal/webui"
)

// New builds Denizen's full HTTP handler. Routes under /api/v1/items,
// /api/v1/trash, /api/v1/uploads, /api/v1/shares and /api/v1/me require a
// valid access token (middleware.RequireAuth); /api/v1/invites and
// /api/v1/users additionally require an admin (middleware.RequireAdmin).
// The /api/v1/auth/* routes, the public /s/{token} routes (for whoever
// opens a share link), and the frontend itself require neither.
func New(auth *handler.AuthHandler, items *handler.ItemHandler, shares *handler.ShareHandler, users *handler.UserHandler, onlyOffice *handler.OnlyOfficeHandler, uploads http.Handler, tokens *token.Issuer) (http.Handler, error) {
	mux := http.NewServeMux()

	mux.HandleFunc("POST /api/v1/auth/register", auth.Register)
	mux.HandleFunc("POST /api/v1/auth/login", auth.Login)
	mux.HandleFunc("POST /api/v1/auth/refresh", auth.Refresh)
	mux.HandleFunc("POST /api/v1/auth/logout", auth.Logout)

	requireAuth := middleware.RequireAuth(tokens)
	requireAuthOrContentToken := middleware.RequireAuthOrContentToken(tokens)

	mux.Handle("POST /api/v1/invites", requireAuth(middleware.RequireAdmin(http.HandlerFunc(auth.CreateInvite))))

	mux.Handle("POST /api/v1/items", requireAuth(http.HandlerFunc(items.Create)))
	mux.Handle("GET /api/v1/items", requireAuth(http.HandlerFunc(items.List)))
	mux.Handle("GET /api/v1/items/{id}", requireAuth(http.HandlerFunc(items.Get)))
	// The only route on a content token as well as a normal bearer token —
	// see middleware.RequireAuthOrContentToken and ContentToken's own doc
	// comment for why <video>/<audio> need this and nothing else does.
	mux.Handle("GET /api/v1/items/{id}/content", requireAuthOrContentToken(http.HandlerFunc(items.Content)))
	mux.Handle("POST /api/v1/items/{id}/content-token", requireAuth(http.HandlerFunc(items.ContentToken)))
	mux.Handle("PATCH /api/v1/items/{id}", requireAuth(http.HandlerFunc(items.Move)))
	mux.Handle("DELETE /api/v1/items/{id}", requireAuth(http.HandlerFunc(items.Delete)))
	mux.Handle("POST /api/v1/items/{id}/restore", requireAuth(http.HandlerFunc(items.Restore)))
	mux.Handle("POST /api/v1/items/{id}/copy", requireAuth(http.HandlerFunc(items.Copy)))

	mux.Handle("GET /api/v1/trash", requireAuth(http.HandlerFunc(items.ListTrash)))
	mux.Handle("DELETE /api/v1/trash/{id}", requireAuth(http.HandlerFunc(items.DeletePermanently)))

	mux.Handle("POST /api/v1/items/{id}/shares", requireAuth(http.HandlerFunc(shares.Create)))
	mux.Handle("GET /api/v1/shares", requireAuth(http.HandlerFunc(shares.ListMine)))
	mux.Handle("DELETE /api/v1/shares/{id}", requireAuth(http.HandlerFunc(shares.Revoke)))

	// Both routes report "disabled" rather than 404/error when no Document
	// Server is configured — see OnlyOfficeHandler's own doc comment — so
	// these are always safe to mount, whether or not the feature is used.
	mux.Handle("GET /api/v1/onlyoffice/status", requireAuth(http.HandlerFunc(onlyOffice.Status)))
	mux.Handle("GET /api/v1/items/{id}/onlyoffice-config", requireAuth(http.HandlerFunc(onlyOffice.Config)))
	// No RequireAuth: this is the Document Server itself calling back to
	// report a save, not a logged-in user — it authenticates via a JWT
	// signed with the shared secret instead (see
	// onlyoffice.Client.VerifyCallback, called inside Callback itself).
	mux.HandleFunc("POST /api/v1/items/{id}/onlyoffice-callback", onlyOffice.Callback)

	mux.Handle("GET /api/v1/me", requireAuth(http.HandlerFunc(users.Me)))
	mux.Handle("GET /api/v1/users", requireAuth(middleware.RequireAdmin(http.HandlerFunc(users.List))))
	mux.Handle("PATCH /api/v1/users/{id}", requireAuth(middleware.RequireAdmin(http.HandlerFunc(users.Update))))

	// Public: no RequireAuth wrapper. Whoever opens a share link may not
	// have (or need) a Denizen account at all — see ShareService.Resolve
	// for how requires_auth is still honored per-share.
	mux.HandleFunc("GET /s/{token}", shares.PublicMetadata)
	mux.HandleFunc("GET /s/{token}/content", shares.PublicContent)

	// tusd routes every verb (POST/PATCH/HEAD/GET/DELETE) itself once past
	// this prefix — wrapping the whole subtree in requireAuth is what covers
	// all of them, since tusd's own hooks only run on create/finish/
	// terminate, not on the PATCH/HEAD requests that continue an upload.
	mux.Handle(upload.BasePath, requireAuth(http.StripPrefix(upload.BasePath, uploads)))

	// Catch-all: the built frontend. Matches only what nothing more
	// specific above already claimed, per net/http.ServeMux's precedence
	// rules — every /api/v1/* and /s/* path is handled well before this.
	webHandler, err := webui.Handler()
	if err != nil {
		return nil, err
	}
	mux.Handle("/", webHandler)

	return mux, nil
}
