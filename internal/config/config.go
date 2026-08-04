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
	// MaxUploadSizeBytes caps a single upload independently of the uploading
	// user's remaining quota — a sanity backstop against filling the disk,
	// since real per-user quota *enforcement* on upload is still a TODO
	// (see docs/ARCHITECTURE.md).
	MaxUploadSizeBytes int64
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
		MaxUploadSizeBytes: getEnvInt64("DENIZEN_MAX_UPLOAD_SIZE_BYTES", 10<<30), // 10 GiB
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
