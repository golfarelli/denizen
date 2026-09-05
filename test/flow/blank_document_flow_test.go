package flow

import (
	"io"
	"net/http"
	"testing"
	"time"
)

func TestBlankDocumentFlow_CreatesARealDownloadableFile(t *testing.T) {
	ts := newTestServer(t)
	ctx := t.Context()

	code, created, err := ts.app.Auth.EnsureBootstrapInvite(ctx, time.Hour)
	if err != nil || !created {
		t.Fatalf("EnsureBootstrapInvite: code=%q created=%v err=%v", code, created, err)
	}
	alice := registerAndLogin(t, ts, code, "alice", "correct-horse-battery-staple")

	cases := []struct {
		ext, name, wantMime string
	}{
		{"docx", "Untitled document.docx", "application/vnd.openxmlformats-officedocument.wordprocessingml.document"},
		{"xlsx", "Untitled spreadsheet.xlsx", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"},
		{"pptx", "Untitled presentation.pptx", "application/vnd.openxmlformats-officedocument.presentationml.presentation"},
	}
	for _, c := range cases {
		t.Run(c.ext, func(t *testing.T) {
			createRes := authedRequest(t, http.MethodPost, ts.URL+"/api/v1/items", alice, map[string]any{
				"type": c.ext, "name": c.name, "parent_id": nil,
			})
			if createRes.StatusCode != http.StatusCreated {
				body, _ := io.ReadAll(createRes.Body)
				t.Fatalf("create %s: got status %d, body: %s", c.ext, createRes.StatusCode, body)
			}
			item := decodeJSON[apiItem](t, createRes)
			if item.Type != "file" {
				t.Errorf("Type = %q, want file", item.Type)
			}
			if item.SizeBytes <= 0 {
				t.Errorf("SizeBytes = %d, want > 0 — a real template, not an empty stub", item.SizeBytes)
			}
			if item.MimeType == nil || *item.MimeType != c.wantMime {
				t.Errorf("MimeType = %v, want %q", item.MimeType, c.wantMime)
			}

			// Real, downloadable bytes — not just a DB row with no content.
			contentRes := authedRequest(t, http.MethodGet, ts.URL+"/api/v1/items/"+item.ID+"/content", alice, nil)
			if contentRes.StatusCode != http.StatusOK {
				t.Fatalf("download: got status %d", contentRes.StatusCode)
			}
			got, err := io.ReadAll(contentRes.Body)
			if err != nil {
				t.Fatal(err)
			}
			if int64(len(got)) != item.SizeBytes {
				t.Errorf("downloaded %d bytes, want %d (item.SizeBytes)", len(got), item.SizeBytes)
			}
			// A real zip (every OOXML file is one) starts with "PK".
			if len(got) < 2 || got[0] != 'P' || got[1] != 'K' {
				t.Error("downloaded content doesn't look like a zip (OOXML) file at all")
			}
		})
	}

	// Quota actually moved — these aren't free.
	var storageUsed int64
	if err := ts.app.DB.QueryRowContext(ctx, `SELECT storage_used_bytes FROM users WHERE id = ?`, alice.id).Scan(&storageUsed); err != nil {
		t.Fatalf("scan storage_used_bytes: %v", err)
	}
	if storageUsed <= 0 {
		t.Errorf("storage_used_bytes = %d, want > 0 after creating 3 blank documents", storageUsed)
	}
}

func TestBlankDocumentFlow_RejectsUnsupportedTypeAndMismatchedExtension(t *testing.T) {
	ts := newTestServer(t)
	ctx := t.Context()

	code, created, err := ts.app.Auth.EnsureBootstrapInvite(ctx, time.Hour)
	if err != nil || !created {
		t.Fatalf("EnsureBootstrapInvite: code=%q created=%v err=%v", code, created, err)
	}
	alice := registerAndLogin(t, ts, code, "alice", "correct-horse-battery-staple")

	// Not folder, not docx/xlsx/pptx — every other file needs real bytes,
	// created via upload instead.
	badTypeRes := authedRequest(t, http.MethodPost, ts.URL+"/api/v1/items", alice, map[string]any{
		"type": "pdf", "name": "Something.pdf", "parent_id": nil,
	})
	if badTypeRes.StatusCode != http.StatusBadRequest {
		t.Errorf("create with type=pdf: got status %d, want 400", badTypeRes.StatusCode)
	}

	// A name that doesn't end in .docx would silently break OnlyOffice
	// later (see CreateBlankDocument's own comment) — rejected up front
	// instead.
	mismatchRes := authedRequest(t, http.MethodPost, ts.URL+"/api/v1/items", alice, map[string]any{
		"type": "docx", "name": "Something.txt", "parent_id": nil,
	})
	if mismatchRes.StatusCode != http.StatusBadRequest {
		t.Errorf("create docx named .txt: got status %d, want 400", mismatchRes.StatusCode)
	}
}
