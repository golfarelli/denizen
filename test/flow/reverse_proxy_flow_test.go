package flow

import (
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/tus/tusd/v2/pkg/handler"
)

// TestUploadFlow_LocationHonorsForwardedHeaders is the regression test for a
// real bug hit behind a TLS-terminating reverse proxy (Tailscale's own
// `tailscale serve`, used to expose Denizen over HTTPS for PWA testing):
// tusd builds the absolute Location URL it returns on upload creation from
// the scheme/host of the raw connection it itself receives, which behind
// any such proxy is plain HTTP to a backend address, not the HTTPS URL the
// actual client connected to. Every subsequent PATCH/HEAD tus-js-client
// makes then targets that wrong (http://) URL, breaking the upload right
// after it starts — confirmed live: Denizen installed as an HTTPS PWA,
// upload failing with "tus: failed to resume upload ... url: http://...".
// RespectForwardedHeaders (internal/upload.NewHandler) is the fix; this
// locks in that tusd actually honors X-Forwarded-Proto/-Host, not just that
// the config flag is set.
func TestUploadFlow_LocationHonorsForwardedHeaders(t *testing.T) {
	ts := newTestServer(t)
	ctx := t.Context()

	code, created, err := ts.app.Auth.EnsureBootstrapInvite(ctx, time.Hour)
	if err != nil || !created {
		t.Fatalf("EnsureBootstrapInvite: code=%q created=%v err=%v", code, created, err)
	}
	fabio := registerAndLogin(t, ts, code, "fabio", "correct-horse-battery-staple")

	content := []byte("hello from behind a reverse proxy")
	createRes := tusRequest(t, http.MethodPost, ts.URL+"/api/v1/uploads/", fabio, nil, map[string]string{
		"Upload-Length":     strconv.Itoa(len(content)),
		"Upload-Metadata":   handler.SerializeMetadataHeader(map[string]string{"filename": "via-proxy.txt"}),
		"X-Forwarded-Proto": "https",
		"X-Forwarded-Host":  "zimablade.example.ts.net",
	})
	if createRes.StatusCode != http.StatusCreated {
		t.Fatalf("create upload: got status %d", createRes.StatusCode)
	}

	location := createRes.Header.Get("Location")
	const wantPrefix = "https://zimablade.example.ts.net/api/v1/uploads/"
	if len(location) < len(wantPrefix) || location[:len(wantPrefix)] != wantPrefix {
		t.Fatalf("Location = %q, want it to start with %q (the client-facing HTTPS URL, not tusd's own raw plain-HTTP view of the connection)", location, wantPrefix)
	}
}
