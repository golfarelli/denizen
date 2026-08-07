package db

import (
	stdsql "database/sql"
	"os"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

// Simulates the real ZimaBlade scenario: a database that already has the
// old schema (pre-migration-runner) applied directly, with real data in
// it, then opened again by the new migration-aware Open().
func TestMigrate_ExistingDatabaseUpgradesCleanly(t *testing.T) {
	dir := t.TempDir()
	dbDir := filepath.Join(dir, "db")
	if err := os.MkdirAll(dbDir, 0o755); err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(dbDir, "denizen.db")

	// Step 1: apply the OLD schema directly (as if this were a database
	// from before the migration runner existed), then insert a real row.
	cn, err := stdsql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	oldSchema, err := os.ReadFile("migrations/0001_initial_schema.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cn.Exec(string(oldSchema)); err != nil {
		t.Fatalf("apply old schema: %v", err)
	}
	if _, err := cn.Exec(
		`INSERT INTO users (id, username, password_hash, is_admin, quota_bytes, storage_used_bytes, disabled, created_at)
		 VALUES ('u1', 'fabio', 'hash', 1, 1000, 0, 0, 1000)`,
	); err != nil {
		t.Fatalf("insert real data: %v", err)
	}
	cn.Close()

	// Step 2: open it via the real Open() — this must NOT fail (CREATE
	// TABLE users would error if migration 1 were re-run for real) and
	// must NOT lose the existing row, and must end up with user_shares
	// present (migration 2 applied for real).
	cn2, err := Open(dir)
	if err != nil {
		t.Fatalf("Open() on existing database: %v", err)
	}
	defer cn2.Close()

	var username string
	if err := cn2.QueryRow(`SELECT username FROM users WHERE id = 'u1'`).Scan(&username); err != nil {
		t.Fatalf("pre-existing row lost: %v", err)
	}
	if username != "fabio" {
		t.Fatalf("got username %q, want fabio", username)
	}

	var userSharesExists int
	if err := cn2.QueryRow(
		`SELECT count(*) FROM sqlite_master WHERE type='table' AND name='user_shares'`,
	).Scan(&userSharesExists); err != nil {
		t.Fatal(err)
	}
	if userSharesExists == 0 {
		t.Fatal("user_shares table was not created by the migration")
	}

	var recorded []int
	rows, err := cn2.Query(`SELECT version FROM schema_migrations ORDER BY version`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var v int
		rows.Scan(&v)
		recorded = append(recorded, v)
	}
	if len(recorded) != 2 || recorded[0] != 1 || recorded[1] != 2 {
		t.Fatalf("schema_migrations = %v, want [1 2]", recorded)
	}

	// Step 3: opening it a THIRD time must be a clean no-op (idempotent).
	cn2.Close()
	cn3, err := Open(dir)
	if err != nil {
		t.Fatalf("second re-open: %v", err)
	}
	defer cn3.Close()
	if err := cn3.QueryRow(`SELECT username FROM users WHERE id = 'u1'`).Scan(&username); err != nil {
		t.Fatalf("row lost on third open: %v", err)
	}
}
