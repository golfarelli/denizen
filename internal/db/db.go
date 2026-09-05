// Package db opens Denizen's SQLite database and applies its schema via a
// small numbered-migration runner (internal/db/migrations/*.sql).
package db

import (
	stdsql "database/sql"
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Open opens (creating if needed) the SQLite database at dataDir/db/denizen.db,
// applies any pending migrations, and configures the pragmas the rest of
// the application relies on (WAL mode, foreign keys, a single connection so
// concurrent writers queue instead of hitting SQLITE_BUSY).
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

	if err := migrate(cn); err != nil {
		cn.Close()
		return nil, err
	}

	return cn, nil
}

// migration is one file from migrations/ — its version comes from the
// filename's leading number (0001_initial_schema.sql -> 1), not tracked
// separately, so the filename itself is the only source of truth for
// ordering.
type migration struct {
	version int
	name    string
	sql     string
}

// migrate applies every migration in migrations/ that isn't already
// recorded in schema_migrations, in version order. A database that already
// has a `users` table but no recorded migrations at all is an existing
// install from before this migration runner existed — migration 1
// (0001_initial_schema.sql) is exactly the schema it already has, verbatim,
// so it's recorded as applied without being re-run: CREATE TABLE would
// fail against tables that are already there. A genuinely fresh database
// has neither, so migration 1 actually runs and creates everything from
// scratch, same as every migration after it.
func migrate(cn *stdsql.DB) error {
	if _, err := cn.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		version    INTEGER PRIMARY KEY,
		name       TEXT NOT NULL,
		applied_at INTEGER NOT NULL
	)`); err != nil {
		return fmt.Errorf("db: create schema_migrations: %w", err)
	}

	migrations, err := loadMigrations()
	if err != nil {
		return err
	}

	var alreadyRecorded int
	if err := cn.QueryRow(`SELECT count(*) FROM schema_migrations`).Scan(&alreadyRecorded); err != nil {
		return fmt.Errorf("db: check schema_migrations: %w", err)
	}
	if alreadyRecorded == 0 {
		var usersExists int
		if err := cn.QueryRow(
			`SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = 'users'`,
		).Scan(&usersExists); err != nil {
			return fmt.Errorf("db: check for existing schema: %w", err)
		}
		if usersExists > 0 && len(migrations) > 0 {
			if err := recordApplied(cn, migrations[0]); err != nil {
				return err
			}
		}
	}

	for _, m := range migrations {
		var applied int
		if err := cn.QueryRow(`SELECT count(*) FROM schema_migrations WHERE version = ?`, m.version).Scan(&applied); err != nil {
			return fmt.Errorf("db: check migration %d: %w", m.version, err)
		}
		if applied > 0 {
			continue
		}

		tx, err := cn.Begin()
		if err != nil {
			return fmt.Errorf("db: begin migration %d: %w", m.version, err)
		}
		if _, err := tx.Exec(m.sql); err != nil {
			tx.Rollback()
			return fmt.Errorf("db: apply migration %d (%s): %w", m.version, m.name, err)
		}
		if _, err := tx.Exec(
			`INSERT INTO schema_migrations (version, name, applied_at) VALUES (?, ?, ?)`,
			m.version, m.name, time.Now().Unix(),
		); err != nil {
			tx.Rollback()
			return fmt.Errorf("db: record migration %d: %w", m.version, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("db: commit migration %d: %w", m.version, err)
		}
	}

	return nil
}

func recordApplied(cn *stdsql.DB, m migration) error {
	_, err := cn.Exec(
		`INSERT INTO schema_migrations (version, name, applied_at) VALUES (?, ?, ?)`,
		m.version, m.name, time.Now().Unix(),
	)
	if err != nil {
		return fmt.Errorf("db: record pre-existing migration %d: %w", m.version, err)
	}
	return nil
}

func loadMigrations() ([]migration, error) {
	entries, err := fs.ReadDir(migrationsFS, "migrations")
	if err != nil {
		return nil, fmt.Errorf("db: read migrations dir: %w", err)
	}

	migrations := make([]migration, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		version, err := migrationVersion(entry.Name())
		if err != nil {
			return nil, err
		}
		content, err := migrationsFS.ReadFile("migrations/" + entry.Name())
		if err != nil {
			return nil, fmt.Errorf("db: read %s: %w", entry.Name(), err)
		}
		migrations = append(migrations, migration{version: version, name: entry.Name(), sql: string(content)})
	}

	sort.Slice(migrations, func(i, j int) bool { return migrations[i].version < migrations[j].version })
	return migrations, nil
}

// migrationVersion parses the leading digits of a migration filename, e.g.
// "0002_user_shares.sql" -> 2.
func migrationVersion(filename string) (int, error) {
	underscore := strings.IndexByte(filename, '_')
	if underscore <= 0 {
		return 0, fmt.Errorf("db: migration filename %q has no leading version number", filename)
	}
	version, err := strconv.Atoi(filename[:underscore])
	if err != nil {
		return 0, fmt.Errorf("db: migration filename %q has no leading version number: %w", filename, err)
	}
	return version, nil
}
