package flow

import (
	"io"
	"net/http"
	"testing"
	"time"
)

type contentTokenResp struct {
	Token     string `json:"token"`
	ExpiresAt int64  `json:"expires_at"`
}

// getWithoutAuthHeader issues a plain GET with no Authorization header at
// all — the point of a content token is that it works precisely when a
// caller (a <video>/<audio> element) can't attach one, so every test here
// deliberately never sets it, unlike authedRequest.
func getWithoutAuthHeader(t *testing.T, url string) *http.Response {
	t.Helper()
	res, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	return res
}

func TestContentTokenFlow_GrantsAccessWithoutTheBearerHeader(t *testing.T) {
	ts := newTestServer(t)
	ctx := t.Context()

	code, created, err := ts.app.Auth.EnsureBootstrapInvite(ctx, time.Hour)
	if err != nil || !created {
		t.Fatalf("EnsureBootstrapInvite: code=%q created=%v err=%v", code, created, err)
	}
	fabio := registerAndLogin(t, ts, code, "fabio", "correct-horse-battery-staple")

	content := []byte("pretend this is video bytes")
	uploaded := uploadFile(t, ts, fabio, nil, "clip.mp4", content)

	mintRes := authedRequest(t, http.MethodPost, ts.URL+"/api/v1/items/"+uploaded.ID+"/content-token", fabio, nil)
	if mintRes.StatusCode != http.StatusOK {
		t.Fatalf("mint content token: got status %d", mintRes.StatusCode)
	}
	minted := decodeJSON[contentTokenResp](t, mintRes)
	if minted.Token == "" {
		t.Fatal("minted an empty token")
	}
	if minted.ExpiresAt <= time.Now().Unix() {
		t.Errorf("ExpiresAt = %d, want a time in the future", minted.ExpiresAt)
	}

	// The whole point: no Authorization header at all, auth rides entirely
	// on the query parameter — exactly what a <video src="..."> can do.
	contentRes := getWithoutAuthHeader(t, ts.URL+"/api/v1/items/"+uploaded.ID+"/content?token="+minted.Token)
	if contentRes.StatusCode != http.StatusOK {
		t.Fatalf("GET content with token: got status %d", contentRes.StatusCode)
	}
	body, err := io.ReadAll(contentRes.Body)
	if err != nil {
		t.Fatalf("read content response body: %v", err)
	}
	if string(body) != string(content) {
		t.Errorf("content = %q, want %q", body, content)
	}
}

func TestContentTokenFlow_RejectsTokenMintedForADifferentItem(t *testing.T) {
	ts := newTestServer(t)
	ctx := t.Context()

	code, created, err := ts.app.Auth.EnsureBootstrapInvite(ctx, time.Hour)
	if err != nil || !created {
		t.Fatalf("EnsureBootstrapInvite: code=%q created=%v err=%v", code, created, err)
	}
	fabio := registerAndLogin(t, ts, code, "fabio", "correct-horse-battery-staple")

	fileA := uploadFile(t, ts, fabio, nil, "a.mp4", []byte("file a"))
	fileB := uploadFile(t, ts, fabio, nil, "b.mp4", []byte("file b"))

	mintRes := authedRequest(t, http.MethodPost, ts.URL+"/api/v1/items/"+fileA.ID+"/content-token", fabio, nil)
	tokenForA := decodeJSON[contentTokenResp](t, mintRes).Token

	// A token minted for A must not open B, even though both belong to the
	// same authenticated user — the whole safety property of scoping it to
	// one item id would otherwise be pointless.
	res := getWithoutAuthHeader(t, ts.URL+"/api/v1/items/"+fileB.ID+"/content?token="+tokenForA)
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("content of B with A's token: got status %d, want %d", res.StatusCode, http.StatusUnauthorized)
	}
}

func TestContentTokenFlow_ContentStillRejectsRequestsWithNeitherCredential(t *testing.T) {
	ts := newTestServer(t)
	ctx := t.Context()

	code, created, err := ts.app.Auth.EnsureBootstrapInvite(ctx, time.Hour)
	if err != nil || !created {
		t.Fatalf("EnsureBootstrapInvite: code=%q created=%v err=%v", code, created, err)
	}
	fabio := registerAndLogin(t, ts, code, "fabio", "correct-horse-battery-staple")
	uploaded := uploadFile(t, ts, fabio, nil, "private.txt", []byte("secret"))

	// Neither an Authorization header nor a ?token= — this is the
	// pre-existing behavior RequireAuthOrContentToken must not have loosened.
	res := getWithoutAuthHeader(t, ts.URL+"/api/v1/items/"+uploaded.ID+"/content")
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("content with no credential at all: got status %d, want %d", res.StatusCode, http.StatusUnauthorized)
	}
}

func TestContentTokenFlow_CannotMintForAnotherUsersItem(t *testing.T) {
	ts := newTestServer(t)
	ctx := t.Context()

	bootstrapCode, created, err := ts.app.Auth.EnsureBootstrapInvite(ctx, time.Hour)
	if err != nil || !created {
		t.Fatalf("EnsureBootstrapInvite: code=%q created=%v err=%v", bootstrapCode, created, err)
	}
	fabio := registerAndLogin(t, ts, bootstrapCode, "fabio", "correct-horse-battery-staple")

	guestCode, _, err := ts.app.Auth.CreateInvite(ctx, fabio.id, nil, time.Hour)
	if err != nil {
		t.Fatalf("CreateInvite: %v", err)
	}
	guest := registerAndLogin(t, ts, guestCode, "guest", "another-strong-password")

	fabioFile := uploadFile(t, ts, fabio, nil, "fabio-only.txt", []byte("not yours"))

	// The same ownership check every other item route already enforces —
	// this endpoint isn't a way around it.
	res := authedRequest(t, http.MethodPost, ts.URL+"/api/v1/items/"+fabioFile.ID+"/content-token", guest, nil)
	if res.StatusCode == http.StatusOK {
		t.Fatal("guest was able to mint a content token for fabio's file")
	}
}
