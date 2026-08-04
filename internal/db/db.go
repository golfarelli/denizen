// Package db opens Denizen's SQLite database and applies its schema.
package db

import (
	stdsql "database/sql"
	"embed"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

//go:embed schema.sql
var schemaFS embed.FS

// Open opens (creating if needed) the SQLite database at dataDir/db/denizen.db,
// applies the schema on a fresh database, and configures the pragmas the
// rest of the application relies on (WAL mode, foreign keys, a single
// connection so concurrent writers queue instead of hitting SQLITE_BUSY).
func Open(dataDir string) (*stdsql.DB, error) {
	dbDir := filepath.Join(dataDir, "db")
	if err := os.MkdirAll(dbDir, 0o755); err != nil {
		return nil, fmt.Errorf("db: create data dir: %w", err)
	}
	dbPath := filepath.Join(dbDir, "denizen.db")

	cn, err := stdsql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("db: open: %w", err)
	}

	// SQLite only really tolerates one writer at a time; keeping the pool at
	// a single connection turns "many goroutines write concurrently" into
	// "many goroutines queue on the same connection" (see
	// docs/ARCHITECTURE.md's single-writer discipline), which is what we
	// want instead of SQLITE_BUSY errors under load.
	cn.SetMaxOpenConns(1)

	for _, pragma := range []string{
		"PRAGMA busy_timeout = 5000",
		"PRAGMA journal_mode = WAL",
		"PRAGMA foreign_keys = ON",
		"PRAGMA synchronous = NORMAL",
	} {
		if _, err := cn.Exec(pragma); err != nil {
			cn.Close()
			return nil, fmt.Errorf("db: %s: %w", pragma, err)
		}
	}

	if err := applySchema(cn); err != nil {
		cn.Close()
		return nil, err
	}

	return cn, nil
}

func applySchema(cn *stdsql.DB) error {
	var count int
	sql := `SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = 'users'`
	if err := cn.QueryRow(sql).Scan(&count); err != nil {
		return fmt.Errorf("db: check schema: %w", err)
	}
	if count > 0 {
		return nil // already applied
	}
	schema, err := schemaFS.ReadFile("schema.sql")
	if err != nil {
		return fmt.Errorf("db: read embedded schema: %w", err)
	}
	if _, err := cn.Exec(string(schema)); err != nil {
		return fmt.Errorf("db: apply schema: %w", err)
	}
	return nil
}
