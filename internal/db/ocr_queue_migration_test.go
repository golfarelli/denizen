package db

import (
	stdsql "database/sql"
	"path/filepath"
	"sort"
	"testing"

	_ "modernc.org/sqlite"
)

// Migration 0008 replaces OCRRepository.ListPending's per-sweep scan of the
// FTS index with a queue table, backfilled once from the exact condition
// the old query used. This checks that backfill picks precisely the files
// the old query would have returned — nothing waiting is dropped, and
// nothing that never needed OCR (a PDF with a real text layer, an already
// attempted file, a non-OCR type, a shortcut) sneaks in.
func TestMigration0008_BackfillMatchesTheOldListPendingCondition(t *testing.T) {
	cn, err := stdsql.Open("sqlite", filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer cn.Close()
	cn.SetMaxOpenConns(1)

	all, err := loadMigrations()
	if err != nil {
		t.Fatal(err)
	}
	var m8 *migration
	for i, m := range all {
		switch {
		case m.version < 8:
			if _, err := cn.Exec(m.sql); err != nil {
				t.Fatalf("apply migration %d: %v", m.version, err)
			}
		case m.version == 8:
			m8 = &all[i]
		}
	}
	if m8 == nil {
		t.Fatal("migration 0008 not found")
	}

	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := cn.Exec(q, args...); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	exec(`INSERT INTO users (id, username, password_hash, is_admin, quota_bytes, storage_used_bytes, disabled, created_at)
	      VALUES ('u1', 'alice', 'hash', 0, 0, 0, 0, 1)`)
	addFile := func(id, name string, created int64, deleted, target any) {
		exec(`INSERT INTO items (id, owner_id, name, type, created_at, updated_at, deleted_at, target_id)
		      VALUES (?, 'u1', ?, 'file', ?, ?, ?, ?)`, id, name, created, created, deleted, target)
	}
	addContent := func(id, content string) {
		exec(`INSERT INTO item_content_fts (item_id, content) VALUES (?, ?)`, id, content)
	}

	addFile("scan", "blank-scan.pdf", 1, nil, nil)
	addContent("scan", "\f\f\f") // pdftotext's output for a text-less scan: page breaks only
	addFile("text", "digital.pdf", 2, nil, nil)
	addContent("text", "a real text layer")
	addFile("photo", "PHOTO.JPG", 3, nil, nil) // no FTS row at all; also checks case-insensitivity
	addFile("done", "attempted.png", 4, nil, nil)
	exec(`INSERT INTO ocr_attempts (item_id, attempted_at) VALUES ('done', 5)`)
	addFile("doc", "notes.docx", 6, nil, nil) // not an OCR type
	addFile("trashed", "old.jpeg", 7, 100, nil)
	addFile("short", "shortcut.jpg", 8, nil, "photo") // a pointer, no content of its own

	if _, err := cn.Exec(m8.sql); err != nil {
		t.Fatalf("apply migration 8: %v", err)
	}

	rows, err := cn.Query(`SELECT item_id FROM ocr_queue`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var got []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		got = append(got, id)
	}
	sort.Strings(got)

	want := []string{"photo", "scan", "trashed"}
	if len(got) != len(want) {
		t.Fatalf("ocr_queue = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("ocr_queue = %v, want %v", got, want)
		}
	}
}
