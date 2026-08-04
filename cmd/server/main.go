// Command server runs Denizen: a single process serving the API (and,
// later, the embedded frontend build) on one port.
package main

import (
	"context"
	"log"
	"net/http"

	"github.com/golfarelli/denizen/internal/app"
	"github.com/golfarelli/denizen/internal/config"
)

func main() {
	cfg := config.Load()

	a, err := app.New(cfg)
	if err != nil {
		log.Fatalf("denizen: %v", err)
	}
	defer a.DB.Close()

	code, created, err := a.Auth.EnsureBootstrapInvite(context.Background(), cfg.BootstrapInviteTTL)
	if err != nil {
		log.Fatalf("denizen: bootstrap invite: %v", err)
	}
	if created {
		log.Printf("First run: no accounts exist yet. Create the admin account with this invite code: %s", code)
	}

	log.Printf("denizen: listening on %s", cfg.ListenAddr)
	if err := http.ListenAndServe(cfg.ListenAddr, a.Handler); err != nil {
		log.Fatalf("denizen: %v", err)
	}
}
