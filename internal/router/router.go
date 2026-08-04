// Package router wires HTTP routes to handlers, using the standard
// library's pattern-matching ServeMux (Go 1.22+) — no router dependency
// needed for this.
package router

import (
	"net/http"

	"github.com/golfarelli/denizen/internal/handler"
)

// New builds Denizen's full HTTP handler.
func New(auth *handler.AuthHandler) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("POST /api/v1/auth/register", auth.Register)
	mux.HandleFunc("POST /api/v1/auth/login", auth.Login)
	mux.HandleFunc("POST /api/v1/auth/refresh", auth.Refresh)
	mux.HandleFunc("POST /api/v1/auth/logout", auth.Logout)

	return mux
}
