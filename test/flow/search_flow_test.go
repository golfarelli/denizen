package flow

import (
	"net/http"
	"net/url"
	"testing"
	"time"
)

func search(t *testing.T, ts *testServer, user registeredUser, q string) []apiItem {
	t.Helper()
	res := authedRequest(t, http.MethodGet, ts.URL+"/api/v1/search?q="+url.QueryEscape(q), user, nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/v1/search?q=%q: got status %d", q, res.StatusCode)
	}
	return decodeJSON[[]apiItem](t, res)
}

func TestSearchFlow_FindsByNameAcrossTheWholeTreeNotJustOneFolder(t *testing.T) {
	ts := newTestServer(t)
	ctx := t.Context()

	code, created, err := ts.app.Auth.EnsureBootstrapInvite(ctx, time.Hour)
	if err != nil || !created {
		t.Fatalf("EnsureBootstrapInvite: code=%q created=%v err=%v", code, created, err)
	}
	fabio := registerAndLogin(t, ts, code, "fabio", "correct-horse-battery-staple")

	folderRes := authedRequest(t, http.MethodPost, ts.URL+"/api/v1/items", fabio, map[string]any{
		"type": "folder", "name": "Bollette", "parent_id": nil,
	})
	folder := decodeJSON[apiItem](t, folderRes)

	uploadFile(t, ts, fabio, nil, "ricetta pasta.txt", []byte("niente a che vedere"))
	nested := uploadFile(t, ts, fabio, &folder.ID, "bolletta luce agosto.txt", []byte("consumo elettrico"))

	results := search(t, ts, fabio, "bolletta")
	if len(results) != 1 || results[0].ID != nested.ID {
		t.Errorf("search(bolletta) = %+v, want exactly the nested file (whole-tree, not just root)", results)
	}
}

func TestSearchFlow_FindsByContentAndIsScopedToTheCallersOwnItems(t *testing.T) {
	ts := newTestServer(t)
	ctx := t.Context()

	code, created, err := ts.app.Auth.EnsureBootstrapInvite(ctx, time.Hour)
	if err != nil || !created {
		t.Fatalf("EnsureBootstrapInvite: code=%q created=%v err=%v", code, created, err)
	}
	fabio := registerAndLogin(t, ts, code, "fabio", "correct-horse-battery-staple")

	marioCode, _, err := ts.app.Auth.CreateInvite(ctx, fabio.id, nil, time.Hour)
	if err != nil {
		t.Fatalf("CreateInvite: %v", err)
	}
	mario := registerAndLogin(t, ts, marioCode, "mario", "another-strong-password")

	// A generic filename that gives no hint of the content — only a
	// content-index match can find this one.
	target := uploadFile(t, ts, fabio, nil, "note.txt", []byte("il verbale della riunione parla di un preventivo per il tetto"))
	uploadFile(t, ts, fabio, nil, "altro.txt", []byte("contenuto completamente estraneo"))
	// mario has a file mentioning the exact same word, but it must never
	// show up in fabio's results — search is scoped to the caller's own
	// items, same as everything else in this app.
	uploadFile(t, ts, mario, nil, "mario-note.txt", []byte("anche il mio preventivo per il tetto"))

	results := search(t, ts, fabio, "preventivo")
	if len(results) != 1 || results[0].ID != target.ID {
		t.Errorf("fabio's search(preventivo) = %+v, want exactly his own note.txt", results)
	}

	// Diacritic-insensitive: "perche" (no accent, as someone would likely
	// actually type it) still finds content containing "perché".
	uploadFile(t, ts, fabio, nil, "motivazione.txt", []byte("l'ho fatto perché era necessario"))
	accentResults := search(t, ts, fabio, "perche")
	found := false
	for _, r := range accentResults {
		if r.Name == "motivazione.txt" {
			found = true
		}
	}
	if !found {
		t.Errorf("fabio's search(perche) = %+v, want it to match content containing 'perché'", accentResults)
	}
}

func TestSearchFlow_ExcludesTrashedItems(t *testing.T) {
	ts := newTestServer(t)
	ctx := t.Context()

	code, created, err := ts.app.Auth.EnsureBootstrapInvite(ctx, time.Hour)
	if err != nil || !created {
		t.Fatalf("EnsureBootstrapInvite: code=%q created=%v err=%v", code, created, err)
	}
	fabio := registerAndLogin(t, ts, code, "fabio", "correct-horse-battery-staple")

	item := uploadFile(t, ts, fabio, nil, "vecchio contratto.txt", []byte("testo del contratto"))
	if res := authedRequest(t, http.MethodDelete, ts.URL+"/api/v1/items/"+item.ID, fabio, nil); res.StatusCode != http.StatusNoContent {
		t.Fatalf("trash the item: got status %d", res.StatusCode)
	}

	if results := search(t, ts, fabio, "contratto"); len(results) != 0 {
		t.Errorf("search(contratto) after trashing = %+v, want empty", results)
	}
}

func TestSearchFlow_EmptyQueryReturnsNoResultsNotAnError(t *testing.T) {
	ts := newTestServer(t)
	ctx := t.Context()

	code, created, err := ts.app.Auth.EnsureBootstrapInvite(ctx, time.Hour)
	if err != nil || !created {
		t.Fatalf("EnsureBootstrapInvite: code=%q created=%v err=%v", code, created, err)
	}
	fabio := registerAndLogin(t, ts, code, "fabio", "correct-horse-battery-staple")
	uploadFile(t, ts, fabio, nil, "anything.txt", []byte("anything"))

	if results := search(t, ts, fabio, ""); len(results) != 0 {
		t.Errorf("search('') = %+v, want empty", results)
	}
	if results := search(t, ts, fabio, "   "); len(results) != 0 {
		t.Errorf("search('   ') = %+v, want empty", results)
	}
}

// TestSearchFlow_FindsSharedItemsTooNotJustOwnedOnes covers the follow-up
// to the v1 cut documented on ItemService.Search: a folder shared with the
// caller is now searched too — both by name (SearchByNameInSubtree) and
// by content (the resolveGrant check in Search's content-match branch) —
// same reach as browsing into it, not just the top-level grant itself.
func TestSearchFlow_FindsSharedItemsTooNotJustOwnedOnes(t *testing.T) {
	ts := newTestServer(t)
	ctx := t.Context()

	code, created, err := ts.app.Auth.EnsureBootstrapInvite(ctx, time.Hour)
	if err != nil || !created {
		t.Fatalf("EnsureBootstrapInvite: code=%q created=%v err=%v", code, created, err)
	}
	fabio := registerAndLogin(t, ts, code, "fabio", "correct-horse-battery-staple")

	marioCode, _, err := ts.app.Auth.CreateInvite(ctx, fabio.id, nil, time.Hour)
	if err != nil {
		t.Fatalf("CreateInvite: %v", err)
	}
	mario := registerAndLogin(t, ts, marioCode, "mario", "another-strong-password")

	folderRes := authedRequest(t, http.MethodPost, ts.URL+"/api/v1/items", fabio, map[string]any{
		"type": "folder", "name": "Team", "parent_id": nil,
	})
	folder := decodeJSON[apiItem](t, folderRes)
	byName := uploadFile(t, ts, fabio, &folder.ID, "bolletta gas.txt", []byte("consumo del mese"))
	byContent := uploadFile(t, ts, fabio, &folder.ID, "nota.txt", []byte("il preventivo per il tetto è pronto"))
	// Not shared — must never show up in mario's results.
	uploadFile(t, ts, fabio, nil, "privato.txt", []byte("preventivo riservato, non condiviso"))

	if res := createUserShareWithPermission(t, ts, fabio, folder.ID, mario.id, "view"); res.StatusCode != http.StatusCreated {
		t.Fatalf("share folder: got status %d", res.StatusCode)
	}

	nameResults := search(t, ts, mario, "bolletta")
	if len(nameResults) != 1 || nameResults[0].ID != byName.ID || nameResults[0].Owned {
		t.Errorf("mario's search(bolletta) = %+v, want exactly [%s], owned=false", nameResults, byName.ID)
	}
	if nameResults[0].CanEdit {
		t.Errorf("mario's search(bolletta) can_edit = true, want false (view-only grant)")
	}

	contentResults := search(t, ts, mario, "preventivo")
	if len(contentResults) != 1 || contentResults[0].ID != byContent.ID {
		t.Errorf("mario's search(preventivo) = %+v, want exactly fabio's shared nota.txt (not the unshared privato.txt)", contentResults)
	}
}
