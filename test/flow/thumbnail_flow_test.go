package flow

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"net/http"
	"testing"
	"time"
)

// pngBytes builds a small real PNG — a thumbnail test needs an actually
// decodable image, not arbitrary random bytes like content_flow_test.go's
// range-download test gets away with.
func pngBytes(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: 200, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestThumbnailFlow_ImageIsGeneratedCachedAndScopedToOwner(t *testing.T) {
	ts := newTestServer(t)
	ctx := t.Context()

	code, created, err := ts.app.Auth.EnsureBootstrapInvite(ctx, time.Hour)
	if err != nil || !created {
		t.Fatalf("EnsureBootstrapInvite: code=%q created=%v err=%v", code, created, err)
	}
	fabio := registerAndLogin(t, ts, code, "fabio", "correct-horse-battery-staple")

	item := uploadFile(t, ts, fabio, nil, "photo.png", pngBytes(t, 600, 300))

	// --- first request generates it on the fly ---------------------------------
	res := authedRequest(t, http.MethodGet, ts.URL+"/api/v1/items/"+item.ID+"/thumbnail", fabio, nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("thumbnail: got status %d", res.StatusCode)
	}
	if ct := res.Header.Get("Content-Type"); ct != "image/jpeg" {
		t.Errorf("Content-Type = %q, want image/jpeg", ct)
	}
	first, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("read thumbnail body: %v", err)
	}
	img, err := jpeg.Decode(bytes.NewReader(first))
	if err != nil {
		t.Fatalf("decode thumbnail: %v", err)
	}
	if b := img.Bounds(); b.Dx() > 320 || b.Dy() > 320 {
		t.Errorf("thumbnail is %dx%d, expected downscaled to fit 320", b.Dx(), b.Dy())
	}

	// --- second request serves the same bytes back from cache ------------------
	res2 := authedRequest(t, http.MethodGet, ts.URL+"/api/v1/items/"+item.ID+"/thumbnail", fabio, nil)
	if res2.StatusCode != http.StatusOK {
		t.Fatalf("second thumbnail request: got status %d", res2.StatusCode)
	}
	second, err := io.ReadAll(res2.Body)
	if err != nil {
		t.Fatalf("read second thumbnail body: %v", err)
	}
	if !bytes.Equal(first, second) {
		t.Error("second thumbnail request returned different bytes than the first — cache not being served")
	}

	// --- someone else can't fetch it ---------------------------------------------
	secondCode, _, err := ts.app.Auth.CreateInvite(ctx, fabio.id, nil, time.Hour)
	if err != nil {
		t.Fatalf("CreateInvite: %v", err)
	}
	mario := registerAndLogin(t, ts, secondCode, "mario", "another-strong-password")
	marioRes := authedRequest(t, http.MethodGet, ts.URL+"/api/v1/items/"+item.ID+"/thumbnail", mario, nil)
	if marioRes.StatusCode != http.StatusNotFound {
		t.Errorf("mario fetching fabio's thumbnail: got status %d, want %d", marioRes.StatusCode, http.StatusNotFound)
	}
}

func TestThumbnailFlow_UnsupportedTypeIs404(t *testing.T) {
	ts := newTestServer(t)
	ctx := t.Context()

	code, created, err := ts.app.Auth.EnsureBootstrapInvite(ctx, time.Hour)
	if err != nil || !created {
		t.Fatalf("EnsureBootstrapInvite: code=%q created=%v err=%v", code, created, err)
	}
	fabio := registerAndLogin(t, ts, code, "fabio", "correct-horse-battery-staple")

	item := uploadFile(t, ts, fabio, nil, "notes.txt", []byte("nothing to thumbnail here"))

	res := authedRequest(t, http.MethodGet, ts.URL+"/api/v1/items/"+item.ID+"/thumbnail", fabio, nil)
	if res.StatusCode != http.StatusNotFound {
		t.Errorf("thumbnail for .txt: got status %d, want %d", res.StatusCode, http.StatusNotFound)
	}
}

// TestThumbnailFlow_ReuploadInvalidatesCache covers storage.ThumbnailPath's
// own cache-key design: a file replaced with different content (bumping
// UpdatedAt) must get a freshly generated thumbnail, not the stale cached
// one keyed by the old UpdatedAt.
func TestThumbnailFlow_ReuploadInvalidatesCache(t *testing.T) {
	ts := newTestServer(t)
	ctx := t.Context()

	code, created, err := ts.app.Auth.EnsureBootstrapInvite(ctx, time.Hour)
	if err != nil || !created {
		t.Fatalf("EnsureBootstrapInvite: code=%q created=%v err=%v", code, created, err)
	}
	fabio := registerAndLogin(t, ts, code, "fabio", "correct-horse-battery-staple")

	item := uploadFile(t, ts, fabio, nil, "photo.png", pngBytes(t, 100, 100))
	res1 := authedRequest(t, http.MethodGet, ts.URL+"/api/v1/items/"+item.ID+"/thumbnail", fabio, nil)
	if res1.StatusCode != http.StatusOK {
		t.Fatalf("first thumbnail: got status %d", res1.StatusCode)
	}
	before, _ := io.ReadAll(res1.Body)

	// No HTTP route replaces a file's content directly — only the OnlyOffice
	// save callback calls ItemService.ReplaceContent (see internal/handler/
	// onlyoffice.go) — so this test drives it the same white-box way
	// EnsureBootstrapInvite/CreateInvite above already do, straight against
	// ts.app.
	if _, err := ts.app.Items.ReplaceContent(ctx, fabio.id, item.ID, bytes.NewReader(pngBytes(t, 100, 50))); err != nil {
		t.Fatalf("ReplaceContent: %v", err)
	}
	// updated_at is second-resolution (see ItemService's now().Unix() —
	// same granularity ReplaceContent itself uses) — this whole test runs
	// well within one wall-clock second, so without this the "new" row
	// would land on the exact same updated_at as the original upload and
	// the assertion below would be testing nothing. Same DB-back-dating
	// approach trash_purge_flow_test.go already uses since the service's
	// clock isn't reachable from this package.
	if _, err := ts.app.DB.ExecContext(ctx, `UPDATE items SET updated_at = updated_at + 1 WHERE id = ?`, item.ID); err != nil {
		t.Fatalf("bump updated_at: %v", err)
	}

	res2 := authedRequest(t, http.MethodGet, ts.URL+"/api/v1/items/"+item.ID+"/thumbnail", fabio, nil)
	if res2.StatusCode != http.StatusOK {
		t.Fatalf("thumbnail after replace: got status %d", res2.StatusCode)
	}
	after, err := io.ReadAll(res2.Body)
	if err != nil {
		t.Fatalf("read post-replace thumbnail: %v", err)
	}
	if bytes.Equal(before, after) {
		t.Error("thumbnail after content replace is byte-identical to before — stale cache was served")
	}
	img, err := jpeg.Decode(bytes.NewReader(after))
	if err != nil {
		t.Fatalf("decode post-replace thumbnail: %v", err)
	}
	if b := img.Bounds(); b.Dx() != 100 || b.Dy() != 50 {
		t.Errorf("post-replace thumbnail is %dx%d, want 100x50 (the new content, not the old 100x100)", b.Dx(), b.Dy())
	}
}
