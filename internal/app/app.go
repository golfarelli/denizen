// Package app wires Denizen's dependencies together into an http.Handler.
// Kept separate from cmd/server so tests can build the exact same server
// wiring against a temporary database instead of duplicating it.
package app

import (
	stdsql "database/sql"
	"net/http"

	"github.com/golfarelli/denizen/internal/config"
	"github.com/golfarelli/denizen/internal/db"
	"github.com/golfarelli/denizen/internal/handler"
	"github.com/golfarelli/denizen/internal/repository"
	"github.com/golfarelli/denizen/internal/router"
	"github.com/golfarelli/denizen/internal/service"
	"github.com/golfarelli/denizen/internal/token"
)

// App is a fully wired Denizen instance: an HTTP handler plus the pieces
// callers need direct access to outside of HTTP (e.g. the DB handle to
// close on shutdown, or the auth service to create the bootstrap invite at
// startup).
type App struct {
	Handler http.Handler
	Auth    *service.AuthService
	DB      *stdsql.DB
}

// New builds a fully wired App from cfg, opening (and, on a fresh install,
// initializing) the database.
func New(cfg config.Config) (*App, error) {
	cn, err := db.Open(cfg.DataDir)
	if err != nil {
		return nil, err
	}

	users := repository.NewUserRepository(cn)
	invites := repository.NewInviteRepository(cn)
	refreshTokens := repository.NewRefreshTokenRepository(cn)
	tokens := token.NewIssuer(cfg.JWTSecret)

	authService := service.NewAuthService(users, invites, refreshTokens, tokens,
		cfg.DefaultQuotaBytes, cfg.AccessTokenTTL, cfg.RefreshTokenTTL)

	authHandler := handler.NewAuthHandler(authService)

	return &App{
		Handler: router.New(authHandler),
		Auth:    authService,
		DB:      cn,
	}, nil
}
