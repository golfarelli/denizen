package flow

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type shareCreateResponse struct {
	ID           string `json:"id"`
	ItemID       string `json:"item_id"`
	RequiresAuth bool   `json:"requires_auth"`
	ExpiresAt    *int64 `json:"expires_at,omitempty"`
	Token        string `json:"token"`
	URL          string `json:"url"`
}

func createShare(t *testing.T, ts *testServer, user registeredUser, itemID string, requiresAuth bool, expiresAt *int64) shareCreateResponse {
	t.Helper()
	res := authedRequest(t, http.MethodPost, ts.URL+"/api/v1/items/"+itemID+"/shares", user, map[string]any{
		"requires_auth": requiresAuth, "expires_at": expiresAt,
	})
	if res.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(res.Body)
		t.Fatalf("create share: got status %d, body: %s", res.StatusCode, body)
	}
	return decodeJSON[shareCreateResponse](t, res)
}

func TestShareFlow_PublicLinkGrantsAccessAndCanBeRevoked(t *testing.T) {
	ts := newTestServer(t)
	ctx := t.Context()

	code, created, err := ts.app.Auth.EnsureBootstrapInvite(ctx, time.Hour)
	if err != nil || !created {
		t.Fatalf("EnsureBootstrapInvite: code=%q created=%v err=%v", code, created, err)
	}
	fabio := registerAndLogin(t, ts, code, "fabio", "correct-horse-battery-staple")

	content := []byte("these are the contents of a shared file, nothing fancy")
	item := uploadFile(t, ts, fabio, nil, "recipe.txt", content)

	share := createShare(t, ts, fabio, item.ID, false, nil)
	if share.Token == "" || share.URL == "" {
		t.Fatalf("share response missing token/url: %+v", share)
	}

	// --- the bare share URL (what a visitor actually opens) reaches the SPA,
	// not this raw JSON endpoint — the whole reason /meta exists as a
	// separate path. A regression here would mean a visitor's browser gets
	// a JSON blob instead of Denizen's own landing page, exactly the bug
	// this split was built to fix.
	bareRes, err := http.Get(ts.URL + share.URL)
	if err != nil {
		t.Fatalf("GET %s: %v", share.URL, err)
	}
	if bareRes.StatusCode != http.StatusOK {
		t.Fatalf("bare share URL: got status %d, want %d (the SPA's index.html)", bareRes.StatusCode, http.StatusOK)
	}
	if ct := bareRes.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("bare share URL Content-Type = %q, want text/html (index.html) — got JSON here means the frontend route regressed", ct)
	}

	// --- a visitor with no account can read the metadata and download it -------
	metaRes, err := http.Get(ts.URL + "/s/" + share.Token + "/meta")
	if err != nil {
		t.Fatalf("GET /s/token/meta: %v", err)
	}
	if metaRes.StatusCode != http.StatusOK {
		t.Fatalf("public metadata: got status %d", metaRes.StatusCode)
	}
	var meta struct {
		Name         string `json:"name"`
		Type         string `json:"type"`
		SizeBytes    int64  `json:"size_bytes"`
		MimeType     string `json:"mime_type"`
		RequiresAuth bool   `json:"requires_auth"`
	}
	if err := json.NewDecoder(metaRes.Body).Decode(&meta); err != nil {
		t.Fatalf("decode public metadata: %v", err)
	}
	if meta.Name != "recipe.txt" || meta.SizeBytes != int64(len(content)) {
		t.Errorf("public metadata = %+v, want name=recipe.txt size=%d", meta, len(content))
	}
	// mime_type/requires_auth — the landing page (routes/s/[token]/
	// +page.svelte) needs both: mime_type to pick a viewer the same way
	// the private preview page's own previewKind does, requires_auth to
	// decide whether a direct <video src> (no way to attach an
	// Authorization header) is safe to use for this particular share.
	if meta.MimeType != "text/plain; charset=utf-8" {
		t.Errorf("public metadata MimeType = %q, want a real sniffed type, not empty", meta.MimeType)
	}
	if meta.RequiresAuth != false {
		t.Errorf("public metadata RequiresAuth = %v, want false for this share", meta.RequiresAuth)
	}

	contentRes, err := http.Get(ts.URL + "/s/" + share.Token + "/content")
	if err != nil {
		t.Fatalf("GET /s/token/content: %v", err)
	}
	if contentRes.StatusCode != http.StatusOK {
		t.Fatalf("public download: got status %d", contentRes.StatusCode)
	}
	body, err := io.ReadAll(contentRes.Body)
	if err != nil {
		t.Fatalf("read public download body: %v", err)
	}
	if !bytes.Equal(body, content) {
		t.Error("publicly downloaded content does not match the original file")
	}

	// --- an unknown token looks exactly like a wrong/expired one: 404 -----------
	unknownRes, err := http.Get(ts.URL + "/s/not-a-real-token/meta")
	if err != nil {
		t.Fatalf("GET /s/unknown: %v", err)
	}
	if unknownRes.StatusCode != http.StatusNotFound {
		t.Errorf("unknown token: got status %d, want %d", unknownRes.StatusCode, http.StatusNotFound)
	}

	// --- the share shows up in the owner's own listing --------------------------
	listRes := authedRequest(t, http.MethodGet, ts.URL+"/api/v1/shares", fabio, nil)
	shares := decodeJSON[[]shareCreateResponse](t, listRes)
	if len(shares) != 1 || shares[0].ID != share.ID {
		t.Fatalf("shares listing = %+v, want just the one share", shares)
	}
	if shares[0].Token != "" {
		t.Error("the listing must not include the raw token again — it's only ever shown once, at creation")
	}

	// --- revoking it makes the link stop working ---------------------------------
	revokeRes := authedRequest(t, http.MethodDelete, ts.URL+"/api/v1/shares/"+share.ID, fabio, nil)
	if revokeRes.StatusCode != http.StatusNoContent {
		t.Fatalf("revoke share: got status %d", revokeRes.StatusCode)
	}
	afterRevokeRes, err := http.Get(ts.URL + "/s/" + share.Token + "/meta")
	if err != nil {
		t.Fatalf("GET /s/token/meta after revoke: %v", err)
	}
	if afterRevokeRes.StatusCode != http.StatusNotFound {
		t.Errorf("revoked share: got status %d, want %d", afterRevokeRes.StatusCode, http.StatusNotFound)
	}
}

func TestShareFlow_RequiresAuthAndExpiry(t *testing.T) {
	ts := newTestServer(t)
	ctx := t.Context()

	code, created, err := ts.app.Auth.EnsureBootstrapInvite(ctx, time.Hour)
	if err != nil || !created {
		t.Fatalf("EnsureBootstrapInvite: code=%q created=%v err=%v", code, created, err)
	}
	fabio := registerAndLogin(t, ts, code, "fabio", "correct-horse-battery-staple")
	item := uploadFile(t, ts, fabio, nil, "private-ish.txt", []byte("only for logged-in visitors"))

	// --- requires_auth: an anonymous visitor is turned away, a logged-in one isn't
	protectedShare := createShare(t, ts, fabio, item.ID, true, nil)

	anonRes, err := http.Get(ts.URL + "/s/" + protectedShare.Token + "/meta")
	if err != nil {
		t.Fatalf("GET /s/token/meta anonymously: %v", err)
	}
	if anonRes.StatusCode != http.StatusUnauthorized {
		t.Errorf("anonymous visitor on a requires_auth share: got status %d, want %d", anonRes.StatusCode, http.StatusUnauthorized)
	}

	secondCode, _, err := ts.app.Auth.CreateInvite(ctx, fabio.id, nil, time.Hour)
	if err != nil {
		t.Fatalf("CreateInvite: %v", err)
	}
	mario := registerAndLogin(t, ts, secondCode, "mario", "another-strong-password")

	req, err := http.NewRequest(http.MethodGet, ts.URL+"/s/"+protectedShare.Token+"/meta", nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+mario.accessToken)
	authedRes, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /s/token/meta as mario: %v", err)
	}
	if authedRes.StatusCode != http.StatusOK {
		t.Errorf("mario (logged in, not the owner) on a requires_auth share: got status %d, want %d — being logged in to *some* account is all requires_auth asks for",
			authedRes.StatusCode, http.StatusOK)
	}

	// --- an already-expired share is indistinguishable from a nonexistent one ----
	past := time.Now().Add(-time.Hour).Unix()
	expiredShare := createShare(t, ts, fabio, item.ID, false, &past)
	expiredRes, err := http.Get(ts.URL + "/s/" + expiredShare.Token + "/meta")
	if err != nil {
		t.Fatalf("GET /s/token/meta (expired): %v", err)
	}
	if expiredRes.StatusCode != http.StatusNotFound {
		t.Errorf("expired share: got status %d, want %d", expiredRes.StatusCode, http.StatusNotFound)
	}
}

func TestShareFlow_CannotShareSomeoneElsesItem(t *testing.T) {
	ts := newTestServer(t)
	ctx := t.Context()

	code, created, err := ts.app.Auth.EnsureBootstrapInvite(ctx, time.Hour)
	if err != nil || !created {
		t.Fatalf("EnsureBootstrapInvite: code=%q created=%v err=%v", code, created, err)
	}
	fabio := registerAndLogin(t, ts, code, "fabio", "correct-horse-battery-staple")
	item := uploadFile(t, ts, fabio, nil, "mine.txt", []byte("fabio's file"))

	secondCode, _, err := ts.app.Auth.CreateInvite(ctx, fabio.id, nil, time.Hour)
	if err != nil {
		t.Fatalf("CreateInvite: %v", err)
	}
	mario := registerAndLogin(t, ts, secondCode, "mario", "another-strong-password")

	res := authedRequest(t, http.MethodPost, ts.URL+"/api/v1/items/"+item.ID+"/shares", mario, map[string]any{
		"requires_auth": false,
	})
	if res.StatusCode != http.StatusNotFound {
		t.Errorf("mario sharing fabio's file: got status %d, want %d", res.StatusCode, http.StatusNotFound)
	}
}
