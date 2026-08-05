// Package middleware holds cross-cutting HTTP concerns — right now just
// authentication — that wrap handlers instead of belonging to any single
// one of them.
package middleware

import (
	"context"
	"net/http"
	"strings"

	"github.com/golfarelli/denizen/internal/apperr"
	"github.com/golfarelli/denizen/internal/httpio"
	"github.com/golfarelli/denizen/internal/token"
)

type contextKey int

const claimsContextKey contextKey = iota

// RequireAuth rejects any request without a valid "Authorization: Bearer
// <access token>" header, and makes the token's claims available to the
// wrapped handler via ClaimsFromContext.
func RequireAuth(issuer *token.Issuer) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(res http.ResponseWriter, req *http.Request) {
			claims, ok := parseBearer(issuer, req)
			if !ok {
				httpio.WriteError(res, apperr.Unauthorized)
				return
			}

			ctx := context.WithValue(req.Context(), claimsContextKey, claims)
			next.ServeHTTP(res, req.WithContext(ctx))
		})
	}
}

// RequireAdmin rejects any request whose claims (already attached by
// RequireAuth, which must run first in the chain) don't belong to an admin.
// Nothing else about the request is resource-specific — an admin may act on
// any user — so unlike item ownership checks, this is the whole
// authorization decision, made once at the route level.
func RequireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(res http.ResponseWriter, req *http.Request) {
		claims, ok := ClaimsFromContext(req.Context())
		if !ok || !claims.IsAdmin {
			httpio.WriteError(res, apperr.Forbidden)
			return
		}
		next.ServeHTTP(res, req)
	})
}

// ClaimsFromContext retrieves the claims RequireAuth attached to the
// request context. Only meaningful on a route wrapped with RequireAuth —
// everywhere else, ok is false.
func ClaimsFromContext(ctx context.Context) (*token.Claims, bool) {
	claims, ok := ctx.Value(claimsContextKey).(*token.Claims)
	return claims, ok
}

// IsAuthenticated reports whether req carries a valid bearer access token,
// without rejecting the request if it doesn't. For public endpoints where
// being logged in is optional in general but required by a specific
// resource — e.g. a share link created with requires_auth — see the /s/
// routes in internal/handler/share.go.
func IsAuthenticated(issuer *token.Issuer, req *http.Request) bool {
	_, ok := parseBearer(issuer, req)
	return ok
}

func parseBearer(issuer *token.Issuer, req *http.Request) (*token.Claims, bool) {
	const prefix = "Bearer "
	authHeader := req.Header.Get("Authorization")
	if !strings.HasPrefix(authHeader, prefix) {
		return nil, false
	}
	claims, err := issuer.ParseAccessToken(strings.TrimPrefix(authHeader, prefix))
	if err != nil {
		return nil, false
	}
	return claims, true
}
