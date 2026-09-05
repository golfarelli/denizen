package flow

import (
	"io"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/tus/tusd/v2/pkg/handler"
)

func TestQuotaFlow_UploadRejectedOverQuota(t *testing.T) {
	ts := newTestServer(t)
	ctx := t.Context()

	bootstrapCode, created, err := ts.app.Auth.EnsureBootstrapInvite(ctx, time.Hour)
	if err != nil || !created {
		t.Fatalf("EnsureBootstrapInvite: code=%q created=%v err=%v", bootstrapCode, created, err)
	}
	alice := registerAndLogin(t, ts, bootstrapCode, "alice", "correct-horse-battery-staple")

	smallQuota := int64(1000) // bytes
	guestCode, _, err := ts.app.Auth.CreateInvite(ctx, alice.id, &smallQuota, time.Hour)
	if err != nil {
		t.Fatalf("CreateInvite with quota override: %v", err)
	}
	guest := registerAndLogin(t, ts, guestCode, "guest", "another-strong-password")

	var quotaBytes int64
	if err := ts.app.DB.QueryRowContext(ctx, `SELECT quota_bytes FROM users WHERE id = ?`, guest.id).Scan(&quotaBytes); err != nil {
		t.Fatalf("scan guest quota_bytes: %v", err)
	}
	if quotaBytes != smallQuota {
		t.Fatalf("guest.quota_bytes = %d, want %d (the invite's override, not the server default)", quotaBytes, smallQuota)
	}

	// --- a file within quota uploads fine ----------------------------------------
	small := make([]byte, 400)
	smallItem := uploadFile(t, ts, guest, nil, "small.bin", small)

	var storageUsed int64
	if err := ts.app.DB.QueryRowContext(ctx, `SELECT storage_used_bytes FROM users WHERE id = ?`, guest.id).Scan(&storageUsed); err != nil {
		t.Fatalf("scan storage_used_bytes: %v", err)
	}
	if storageUsed != int64(len(small)) {
		t.Fatalf("storage_used_bytes = %d after a %d-byte upload, want %d", storageUsed, len(small), len(small))
	}

	// --- a file that would push past the remaining quota is rejected up front,
	// before any bytes are even accepted --------------------------------------
	tooBig := make([]byte, 900) // 400 already used + 900 > 1000 quota
	createRes := tusRequest(t, http.MethodPost, ts.URL+"/api/v1/uploads/", guest, nil, map[string]string{
		"Upload-Length":   strconv.Itoa(len(tooBig)),
		"Upload-Metadata": handler.SerializeMetadataHeader(map[string]string{"filename": "toobig.bin"}),
	})
	if createRes.StatusCode != http.StatusRequestEntityTooLarge {
		body, _ := io.ReadAll(createRes.Body)
		t.Fatalf("create upload over quota: got status %d, want %d, body: %s",
			createRes.StatusCode, http.StatusRequestEntityTooLarge, body)
	}

	// --- rejected outright: no new item, no change to storage_used_bytes ---------
	listRes := authedRequest(t, http.MethodGet, ts.URL+"/api/v1/items", guest, nil)
	items := decodeJSON[[]apiItem](t, listRes)
	if len(items) != 1 || items[0].ID != smallItem.ID {
		t.Errorf("guest's items = %+v, want just small.bin — the rejected upload must not have been created", items)
	}

	var storageUsedAfter int64
	if err := ts.app.DB.QueryRowContext(ctx, `SELECT storage_used_bytes FROM users WHERE id = ?`, guest.id).Scan(&storageUsedAfter); err != nil {
		t.Fatalf("scan storage_used_bytes after rejection: %v", err)
	}
	if storageUsedAfter != storageUsed {
		t.Errorf("storage_used_bytes changed from %d to %d after a rejected upload, want unchanged", storageUsed, storageUsedAfter)
	}
}
