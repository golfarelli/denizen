// Command server runs Denizen: a single process serving the API (and,
// later, the embedded frontend build) on one port, plus the background
// sweeps that keep storage honest (abandoned uploads, expired trash).
package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"github.com/golfarelli/denizen/internal/app"
	"github.com/golfarelli/denizen/internal/config"
	"github.com/golfarelli/denizen/internal/upload"
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

	// ctx is cancelled on SIGINT/SIGTERM — both the background sweeps below
	// and the HTTP server's shutdown wait on it.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go runPeriodically(ctx, cfg.UploadGCInterval, func() {
		removed, err := upload.CollectGarbage(a.Store.StagingRoot(), cfg.UploadGCAfter)
		if err != nil {
			log.Printf("upload GC: %v", err)
		} else if removed > 0 {
			log.Printf("upload GC: removed %d abandoned upload(s)", removed)
		}
	})

	go runPeriodically(ctx, cfg.TrashPurgeInterval, func() {
		purged, err := a.Items.PurgeExpiredTrash(ctx, cfg.TrashRetention)
		if purged > 0 {
			log.Printf("trash purge: permanently deleted %d expired item(s)", purged)
		}
		if err != nil {
			log.Printf("trash purge: %v", err)
		}
	})

	srv := &http.Server{Addr: cfg.ListenAddr, Handler: a.Handler}
	go func() {
		<-ctx.Done()
		log.Println("denizen: shutting down...")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			log.Printf("denizen: shutdown: %v", err)
		}
	}()

	log.Printf("denizen: listening on %s", cfg.ListenAddr)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("denizen: %v", err)
	}
	log.Println("denizen: shut down cleanly")
}

// runPeriodically calls fn every interval until ctx is cancelled. The first
// call happens after the first interval, not immediately — a fresh server
// has nothing to collect yet.
func runPeriodically(ctx context.Context, interval time.Duration, fn func()) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			fn()
		}
	}
}
