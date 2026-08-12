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
	"github.com/golfarelli/denizen/internal/onlyoffice"
	"github.com/golfarelli/denizen/internal/repository"
	"github.com/golfarelli/denizen/internal/router"
	"github.com/golfarelli/denizen/internal/service"
	"github.com/golfarelli/denizen/internal/storage"
	"github.com/golfarelli/denizen/internal/token"
	"github.com/golfarelli/denizen/internal/upload"
)

// App is a fully wired Denizen instance: an HTTP handler plus the pieces
// callers need direct access to outside of HTTP (e.g. the DB handle to
// close on shutdown, or the auth service to create the bootstrap invite at
// startup).
type App struct {
	Handler    http.Handler
	Auth       *service.AuthService
	Items      *service.ItemService
	Shares     *service.ShareService
	UserShares *service.UserShareService
	Users      *service.UserService
	// Store gives cmd/server what it needs for the background upload-GC
	// sweep (StagingRoot) without exposing the whole storage layer more
	// broadly than that.
	Store *storage.Store
	DB    *stdsql.DB
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
	items := repository.NewItemRepository(cn)
	shares := repository.NewShareRepository(cn)
	userShares := repository.NewUserShareRepository(cn)
	search := repository.NewSearchRepository(cn)
	ocr := repository.NewOCRRepository(cn)
	tokens := token.NewIssuer(cfg.JWTSecret)
	store := storage.New(cfg.DataDir)

	authService := service.NewAuthService(users, invites, refreshTokens, tokens,
		cfg.DefaultQuotaBytes, cfg.AccessTokenTTL, cfg.RefreshTokenTTL)
	itemService := service.NewItemService(items, users, userShares, search, ocr, store)
	shareService := service.NewShareService(shares, itemService)
	userShareService := service.NewUserShareService(userShares, itemService, users)
	userService := service.NewUserService(users)

	authHandler := handler.NewAuthHandler(authService, cfg.InviteTTL)
	itemHandler := handler.NewItemHandler(itemService, tokens)
	shareHandler := handler.NewShareHandler(shareService, tokens)
	userShareHandler := handler.NewUserShareHandler(userShareService)
	userHandler := handler.NewUserHandler(userService)
	// oo.Enabled() is false whenever cfg.OnlyOfficeURL is unset (the
	// default) — see onlyoffice.Client's own doc comment — so this is
	// always safe to construct and wire in, feature-flagged or not.
	oo := onlyoffice.New(cfg.OnlyOfficeURL, cfg.OnlyOfficeJWTSecret, cfg.OnlyOfficeDocumentBaseURL)
	onlyOfficeHandler := handler.NewOnlyOfficeHandler(itemService, oo, tokens)

	uploadHandler, err := upload.NewHandler(store.StagingRoot(), itemService, cfg.MaxUploadSizeBytes)
	if err != nil {
		cn.Close()
		return nil, err
	}

	mux, err := router.New(authHandler, itemHandler, shareHandler, userShareHandler, userHandler, onlyOfficeHandler, uploadHandler, tokens)
	if err != nil {
		cn.Close()
		return nil, err
	}

	return &App{
		Handler:    mux,
		Auth:       authService,
		Items:      itemService,
		Shares:     shareService,
		UserShares: userShareService,
		Users:      userService,
		Store:      store,
		DB:         cn,
	}, nil
}
