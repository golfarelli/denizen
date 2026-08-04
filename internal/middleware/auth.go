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
			const prefix = "Bearer "
			authHeader := req.Header.Get("Authorization")
			if !strings.HasPrefix(authHeader, prefix) {
				httpio.WriteError(res, apperr.Unauthorized)
				return
			}

			claims, err := issuer.ParseAccessToken(strings.TrimPrefix(authHeader, prefix))
			if err != nil {
				httpio.WriteError(res, apperr.Unauthorized)
				return
			}

			ctx := context.WithValue(req.Context(), claimsContextKey, claims)
			next.ServeHTTP(res, req.WithContext(ctx))
		})
	}
}

// ClaimsFromContext retrieves the claims RequireAuth attached to the
// request context. Only meaningful on a route wrapped with RequireAuth —
// everywhere else, ok is false.
func ClaimsFromContext(ctx context.Context) (*token.Claims, bool) {
	claims, ok := ctx.Value(claimsContextKey).(*token.Claims)
	return claims, ok
}
