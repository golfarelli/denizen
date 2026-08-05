package flow

import (
	"bytes"
	"crypto/rand"
	"io"
	"net/http"
	"testing"
	"time"
)

func TestContentFlow_DownloadAndRange(t *testing.T) {
	ts := newTestServer(t)
	ctx := t.Context()

	code, created, err := ts.app.Auth.EnsureBootstrapInvite(ctx, time.Hour)
	if err != nil || !created {
		t.Fatalf("EnsureBootstrapInvite: code=%q created=%v err=%v", code, created, err)
	}
	fabio := registerAndLogin(t, ts, code, "fabio", "correct-horse-battery-staple")

	content := make([]byte, 10_000)
	if _, err := rand.Read(content); err != nil {
		t.Fatalf("generate random content: %v", err)
	}
	item := uploadFile(t, ts, fabio, nil, "report.pdf", content)

	// --- full download matches the original bytes exactly -----------------------
	fullRes := authedRequest(t, http.MethodGet, ts.URL+"/api/v1/items/"+item.ID+"/content", fabio, nil)
	if fullRes.StatusCode != http.StatusOK {
		t.Fatalf("download: got status %d", fullRes.StatusCode)
	}
	fullBody, err := io.ReadAll(fullRes.Body)
	if err != nil {
		t.Fatalf("read download body: %v", err)
	}
	if !bytes.Equal(fullBody, content) {
		t.Errorf("downloaded content (%d bytes) does not match the uploaded content (%d bytes)", len(fullBody), len(content))
	}

	// --- a byte-range request gets back exactly that slice -----------------------
	req, err := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/items/"+item.ID+"/content", nil)
	if err != nil {
		t.Fatalf("build range request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+fabio.accessToken)
	req.Header.Set("Range", "bytes=100-199")
	rangeRes, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("range request: %v", err)
	}
	if rangeRes.StatusCode != http.StatusPartialContent {
		t.Fatalf("range download: got status %d, want %d", rangeRes.StatusCode, http.StatusPartialContent)
	}
	rangeBody, err := io.ReadAll(rangeRes.Body)
	if err != nil {
		t.Fatalf("read range body: %v", err)
	}
	if !bytes.Equal(rangeBody, content[100:200]) {
		t.Error("range response body does not match content[100:200]")
	}
	if got := rangeRes.Header.Get("Content-Range"); got != "bytes 100-199/10000" {
		t.Errorf("Content-Range = %q, want %q", got, "bytes 100-199/10000")
	}

	// --- someone else can't download it -----------------------------------------
	secondCode, err := ts.app.Auth.CreateInvite(ctx, fabio.id, nil, time.Hour)
	if err != nil {
		t.Fatalf("CreateInvite: %v", err)
	}
	mario := registerAndLogin(t, ts, secondCode, "mario", "another-strong-password")
	marioRes := authedRequest(t, http.MethodGet, ts.URL+"/api/v1/items/"+item.ID+"/content", mario, nil)
	if marioRes.StatusCode != http.StatusNotFound {
		t.Errorf("mario downloading fabio's file: got status %d, want %d", marioRes.StatusCode, http.StatusNotFound)
	}

	// --- no token at all -----------------------------------------------------------
	anonRes, err := http.Get(ts.URL + "/api/v1/items/" + item.ID + "/content")
	if err != nil {
		t.Fatalf("anonymous request: %v", err)
	}
	if anonRes.StatusCode != http.StatusUnauthorized {
		t.Errorf("anonymous download: got status %d, want %d", anonRes.StatusCode, http.StatusUnauthorized)
	}
}
