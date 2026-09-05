package flow

import (
	"net/http"
	"testing"
	"time"
)

type meResponse struct {
	ID               string `json:"id"`
	Username         string `json:"username"`
	IsAdmin          bool   `json:"is_admin"`
	QuotaBytes       int64  `json:"quota_bytes"`
	StorageUsedBytes int64  `json:"storage_used_bytes"`
	Disabled         bool   `json:"disabled"`
}

func TestUserFlow_MeAndAdminManagement(t *testing.T) {
	ts := newTestServer(t)
	ctx := t.Context()

	code, created, err := ts.app.Auth.EnsureBootstrapInvite(ctx, time.Hour)
	if err != nil || !created {
		t.Fatalf("EnsureBootstrapInvite: code=%q created=%v err=%v", code, created, err)
	}
	alice := registerAndLogin(t, ts, code, "alice", "correct-horse-battery-staple")

	// --- /me reflects the caller's own account, nothing more --------------------
	meRes := authedRequest(t, http.MethodGet, ts.URL+"/api/v1/me", alice, nil)
	if meRes.StatusCode != http.StatusOK {
		t.Fatalf("GET /me: got status %d", meRes.StatusCode)
	}
	me := decodeJSON[meResponse](t, meRes)
	if me.Username != "alice" || !me.IsAdmin || me.Disabled || me.StorageUsedBytes != 0 {
		t.Errorf("/me = %+v, want username=alice is_admin=true disabled=false storage_used_bytes=0", me)
	}

	secondCode, _, err := ts.app.Auth.CreateInvite(ctx, alice.id, nil, time.Hour)
	if err != nil {
		t.Fatalf("CreateInvite: %v", err)
	}
	mario := registerAndLogin(t, ts, secondCode, "mario", "another-strong-password")

	marioMeRes := authedRequest(t, http.MethodGet, ts.URL+"/api/v1/me", mario, nil)
	marioMe := decodeJSON[meResponse](t, marioMeRes)
	if marioMe.IsAdmin {
		t.Error("mario (regular invite, not the bootstrap one) came back as admin")
	}

	// --- a non-admin can't list or manage users ----------------------------------
	forbiddenListRes := authedRequest(t, http.MethodGet, ts.URL+"/api/v1/users", mario, nil)
	if forbiddenListRes.StatusCode != http.StatusForbidden {
		t.Errorf("mario listing users: got status %d, want %d", forbiddenListRes.StatusCode, http.StatusForbidden)
	}
	forbiddenPatchRes := authedRequest(t, http.MethodPatch, ts.URL+"/api/v1/users/"+alice.id, mario, map[string]any{"disabled": true})
	if forbiddenPatchRes.StatusCode != http.StatusForbidden {
		t.Errorf("mario patching a user: got status %d, want %d", forbiddenPatchRes.StatusCode, http.StatusForbidden)
	}

	// --- the admin sees everyone -------------------------------------------------
	listRes := authedRequest(t, http.MethodGet, ts.URL+"/api/v1/users", alice, nil)
	users := decodeJSON[[]meResponse](t, listRes)
	if len(users) != 2 {
		t.Fatalf("admin's user listing has %d entries, want 2", len(users))
	}

	// --- PATCH is a genuine partial update: one field at a time, the other
	// field's earlier value survives untouched ----------------------------------
	newQuota := int64(12345)
	quotaPatchRes := authedRequest(t, http.MethodPatch, ts.URL+"/api/v1/users/"+mario.id, alice, map[string]any{"quota_bytes": newQuota})
	if quotaPatchRes.StatusCode != http.StatusOK {
		t.Fatalf("patch quota_bytes: got status %d", quotaPatchRes.StatusCode)
	}
	afterQuotaPatch := decodeJSON[meResponse](t, quotaPatchRes)
	if afterQuotaPatch.QuotaBytes != newQuota || afterQuotaPatch.Disabled {
		t.Errorf("after quota patch = %+v, want quota_bytes=%d disabled=false", afterQuotaPatch, newQuota)
	}

	disablePatchRes := authedRequest(t, http.MethodPatch, ts.URL+"/api/v1/users/"+mario.id, alice, map[string]any{"disabled": true})
	if disablePatchRes.StatusCode != http.StatusOK {
		t.Fatalf("patch disabled: got status %d", disablePatchRes.StatusCode)
	}
	afterDisablePatch := decodeJSON[meResponse](t, disablePatchRes)
	if !afterDisablePatch.Disabled || afterDisablePatch.QuotaBytes != newQuota {
		t.Errorf("after disable patch = %+v, want disabled=true quota_bytes=%d (untouched by this patch)", afterDisablePatch, newQuota)
	}

	// --- disabling actually takes effect: mario can no longer log in ------------
	loginRes := postJSON(t, ts.URL+"/api/v1/auth/login", map[string]string{
		"username": "mario", "password": "another-strong-password",
	})
	if loginRes.StatusCode != http.StatusForbidden {
		t.Errorf("disabled user logging in: got status %d, want %d", loginRes.StatusCode, http.StatusForbidden)
	}
}

func TestInviteFlow_AdminCreatesInviteViaAPI(t *testing.T) {
	ts := newTestServer(t)
	ctx := t.Context()

	bootstrapCode, created, err := ts.app.Auth.EnsureBootstrapInvite(ctx, time.Hour)
	if err != nil || !created {
		t.Fatalf("EnsureBootstrapInvite: code=%q created=%v err=%v", bootstrapCode, created, err)
	}
	alice := registerAndLogin(t, ts, bootstrapCode, "alice", "correct-horse-battery-staple")

	// a non-admin can't mint invites
	secondCode, _, err := ts.app.Auth.CreateInvite(ctx, alice.id, nil, time.Hour)
	if err != nil {
		t.Fatalf("CreateInvite (setup): %v", err)
	}
	mario := registerAndLogin(t, ts, secondCode, "mario", "another-strong-password")
	forbiddenRes := authedRequest(t, http.MethodPost, ts.URL+"/api/v1/invites", mario, map[string]any{})
	if forbiddenRes.StatusCode != http.StatusForbidden {
		t.Errorf("mario creating an invite: got status %d, want %d", forbiddenRes.StatusCode, http.StatusForbidden)
	}

	// the admin can, through the real HTTP endpoint — not the Go method the
	// other flow tests use as a setup shortcut
	quota := int64(999999)
	createRes := authedRequest(t, http.MethodPost, ts.URL+"/api/v1/invites", alice, map[string]any{"quota_bytes": quota})
	if createRes.StatusCode != http.StatusCreated {
		t.Fatalf("create invite: got status %d", createRes.StatusCode)
	}
	invite := decodeJSON[struct {
		Code      string `json:"code"`
		ExpiresAt int64  `json:"expires_at"`
	}](t, createRes)
	if invite.Code == "" || invite.ExpiresAt <= time.Now().Unix() {
		t.Fatalf("invite response = %+v, want a non-empty code and a future expiry", invite)
	}

	// and it actually works, quota override included
	guest := registerAndLogin(t, ts, invite.Code, "guest", "yet-another-strong-password")
	var quotaBytes int64
	if err := ts.app.DB.QueryRowContext(ctx, `SELECT quota_bytes FROM users WHERE id = ?`, guest.id).Scan(&quotaBytes); err != nil {
		t.Fatalf("scan guest quota_bytes: %v", err)
	}
	if quotaBytes != quota {
		t.Errorf("guest.quota_bytes = %d, want %d (the invite's override)", quotaBytes, quota)
	}
}
