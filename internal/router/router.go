// Package router wires HTTP routes to handlers, using the standard
// library's pattern-matching ServeMux (Go 1.22+) — no router dependency
// needed for this.
package router

import (
	"net/http"

	"github.com/golfarelli/denizen/internal/handler"
	"github.com/golfarelli/denizen/internal/middleware"
	"github.com/golfarelli/denizen/internal/token"
)

// New builds Denizen's full HTTP handler. Routes under /api/v1/items and
// /api/v1/trash require a valid access token (middleware.RequireAuth); auth
// routes themselves obviously don't.
func New(auth *handler.AuthHandler, items *handler.ItemHandler, tokens *token.Issuer) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("POST /api/v1/auth/register", auth.Register)
	mux.HandleFunc("POST /api/v1/auth/login", auth.Login)
	mux.HandleFunc("POST /api/v1/auth/refresh", auth.Refresh)
	mux.HandleFunc("POST /api/v1/auth/logout", auth.Logout)

	requireAuth := middleware.RequireAuth(tokens)

	mux.Handle("POST /api/v1/items", requireAuth(http.HandlerFunc(items.Create)))
	mux.Handle("GET /api/v1/items", requireAuth(http.HandlerFunc(items.List)))
	mux.Handle("GET /api/v1/items/{id}", requireAuth(http.HandlerFunc(items.Get)))
	mux.Handle("PATCH /api/v1/items/{id}", requireAuth(http.HandlerFunc(items.Move)))
	mux.Handle("DELETE /api/v1/items/{id}", requireAuth(http.HandlerFunc(items.Delete)))
	mux.Handle("POST /api/v1/items/{id}/restore", requireAuth(http.HandlerFunc(items.Restore)))

	mux.Handle("GET /api/v1/trash", requireAuth(http.HandlerFunc(items.ListTrash)))
	mux.Handle("DELETE /api/v1/trash/{id}", requireAuth(http.HandlerFunc(items.DeletePermanently)))

	return mux
}
