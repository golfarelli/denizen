// Package config reads Denizen's runtime configuration from the
// environment, applying sane defaults so the server can start with none of
// it set.
package config

import (
	"os"
	"strconv"
	"time"
)

// Config is Denizen's full runtime configuration.
type Config struct {
	ListenAddr         string
	DataDir            string // everything lives under here: db/, users/, staging/
	JWTSecret          string
	AccessTokenTTL     time.Duration
	RefreshTokenTTL    time.Duration
	DefaultQuotaBytes  int64
	BootstrapInviteTTL time.Duration
	// InviteTTL is how long an admin-issued invite (as opposed to the
	// one-off bootstrap invite) stays redeemable.
	InviteTTL time.Duration
	// MaxUploadSizeBytes caps a single upload independently of the
	// uploading user's remaining quota (which is separately enforced —
	// see internal/service.ItemService.CheckQuota) — a sanity backstop
	// against a single absurdly large upload regardless of quota math.
	MaxUploadSizeBytes int64
	// UploadGCInterval/UploadGCAfter control the background sweep that
	// removes abandoned tus uploads left in the staging directory (started
	// but never finished, and untouched for UploadGCAfter) — see
	// internal/upload.CollectGarbage.
	UploadGCInterval time.Duration
	UploadGCAfter    time.Duration
	// TrashPurgeInterval/TrashRetention control the background sweep that
	// permanently deletes trashed items older than TrashRetention — the
	// 30-day auto-purge policy from docs/ARCHITECTURE.md — see
	// internal/service.ItemService.PurgeExpiredTrash.
	TrashPurgeInterval time.Duration
	TrashRetention     time.Duration
	// OnlyOfficeURL, if set, is the base URL of an OnlyOffice Document
	// Server that unlocks real in-browser Word/Excel/PowerPoint editing
	// (see internal/onlyoffice) — a genuinely heavy separate service, so
	// it's entirely optional: unset (the default) means the client-side
	// preview (docx-preview/xlsx) is all that's available, exactly as
	// before this existed. Denizen itself never bundles or requires it.
	OnlyOfficeURL string
	// OnlyOfficeJWTSecret signs the editor config and validates the
	// Document Server's save-back callback — required by OnlyOffice's own
	// security model whenever OnlyOfficeURL is set (an unsigned config is
	// something the Document Server will refuse by default in any sane
	// deployment).
	OnlyOfficeJWTSecret string
	// OnlyOfficeDocumentBaseURL is how the *Document Server itself* (not a
	// browser) reaches Denizen to fetch a file's bytes — typically a
	// same-Docker-network address like http://denizen:8080, different from
	// whatever public URL browsers use. Required whenever OnlyOfficeURL is
	// set; there's no way to safely guess it (a request's own Host header
	// reflects the browser's address, not necessarily one the Document
	// Server container can resolve at all).
	OnlyOfficeDocumentBaseURL string
}

// Load builds a Config from environment variables, falling back to defaults
// meant for local development — DENIZEN_JWT_SECRET in particular should
// always be set explicitly in any real deployment.
func Load() Config {
	return Config{
		ListenAddr:         getEnv("DENIZEN_LISTEN_ADDR", ":8080"),
		DataDir:            getEnv("DENIZEN_DATA_DIR", "./data"),
		JWTSecret:          getEnv("DENIZEN_JWT_SECRET", "dev-secret-change-me"),
		AccessTokenTTL:     getEnvDuration("DENIZEN_ACCESS_TOKEN_TTL", 15*time.Minute),
		RefreshTokenTTL:    getEnvDuration("DENIZEN_REFRESH_TOKEN_TTL", 30*24*time.Hour),
		DefaultQuotaBytes:  getEnvInt64("DENIZEN_DEFAULT_QUOTA_BYTES", 10<<30), // 10 GiB
		BootstrapInviteTTL: 24 * time.Hour,
		InviteTTL:          getEnvDuration("DENIZEN_INVITE_TTL", 7*24*time.Hour),
		MaxUploadSizeBytes: getEnvInt64("DENIZEN_MAX_UPLOAD_SIZE_BYTES", 10<<30), // 10 GiB
		UploadGCInterval:   getEnvDuration("DENIZEN_UPLOAD_GC_INTERVAL", time.Hour),
		UploadGCAfter:      getEnvDuration("DENIZEN_UPLOAD_GC_AFTER", 24*time.Hour),
		TrashPurgeInterval: getEnvDuration("DENIZEN_TRASH_PURGE_INTERVAL", time.Hour),
		TrashRetention:     getEnvDuration("DENIZEN_TRASH_RETENTION", 30*24*time.Hour),

		OnlyOfficeURL:             getEnv("DENIZEN_ONLYOFFICE_URL", ""),
		OnlyOfficeJWTSecret:       getEnv("DENIZEN_ONLYOFFICE_JWT_SECRET", ""),
		OnlyOfficeDocumentBaseURL: getEnv("DENIZEN_ONLYOFFICE_DOCUMENT_BASE_URL", ""),
	}
}

func getEnv(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}

func getEnvInt64(key string, fallback int64) int64 {
	v, ok := os.LookupEnv(key)
	if !ok {
		return fallback
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return fallback
	}
	return n
}

func getEnvDuration(key string, fallback time.Duration) time.Duration {
	v, ok := os.LookupEnv(key)
	if !ok {
		return fallback
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return fallback
	}
	return d
}
