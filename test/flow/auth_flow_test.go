// Package flow holds Denizen's flow tests: end-to-end, no mocks. Each one
// starts a real server on a real port, drives it with real HTTP requests,
// and asserts on the real result — the database row after the operation,
// not just the HTTP response — per CONTRIBUTING.md.
package flow

import (
	"bytes"
	stdsql "database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golfarelli/denizen/internal/app"
	"github.com/golfarelli/denizen/internal/config"
	"github.com/golfarelli/denizen/internal/token"
)

// testServer is everything a flow test needs: a live HTTP server, the App
// behind it (for DB/service access the test wouldn't otherwise have — e.g.
// the bootstrap invite code, which is normally only logged to stdout), the
// JWT secret used to verify access tokens issued during the test, and the
// data directory a test can check the real filesystem state under.
type testServer struct {
	*httptest.Server
	app       *app.App
	jwtSecret string
	dataDir   string
}

func newTestServer(t *testing.T) *testServer {
	return newTestServerWithConfig(t, nil)
}

// newTestServerWithConfig is newTestServer plus a hook to override fields
// on top of the same base config — for tests of behavior gated by config
// that isn't on by default (e.g. OnlyOffice — see onlyoffice_flow_test.go).
func newTestServerWithConfig(t *testing.T, configure func(*config.Config)) *testServer {
	t.Helper()

	cfg := config.Load()
	cfg.DataDir = t.TempDir()
	cfg.JWTSecret = "flow-test-secret"
	cfg.AccessTokenTTL = time.Minute
	cfg.RefreshTokenTTL = time.Hour
	if configure != nil {
		configure(&cfg)
	}

	a, err := app.New(cfg)
	if err != nil {
		t.Fatalf("app.New: %v", err)
	}
	t.Cleanup(func() { a.DB.Close() })

	srv := httptest.NewServer(a.Handler)
	t.Cleanup(srv.Close)

	return &testServer{Server: srv, app: a, jwtSecret: cfg.JWTSecret, dataDir: cfg.DataDir}
}

func postJSON(t *testing.T, url string, body any) *http.Response {
	t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal request body: %v", err)
	}
	res, err := http.Post(url, "application/json", bytes.NewReader(b))
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
	}
	return res
}

func decodeJSON[T any](t *testing.T, res *http.Response) T {
	t.Helper()
	defer res.Body.Close()
	var out T
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		t.Fatalf("decode response body: %v", err)
	}
	return out
}

type registerResponse struct {
	ID       string `json:"id"`
	Username string `json:"username"`
	IsAdmin  bool   `json:"is_admin"`
}

type tokenPairResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
}

func TestAuthFlow_RegisterLoginRefreshLogout(t *testing.T) {
	ts := newTestServer(t)
	ctx := t.Context()

	code, created, err := ts.app.Auth.EnsureBootstrapInvite(ctx, time.Hour)
	if err != nil || !created {
		t.Fatalf("EnsureBootstrapInvite: code=%q created=%v err=%v", code, created, err)
	}

	// --- register: the bootstrap invite must grant admin -------------------
	registerRes := postJSON(t, ts.URL+"/api/v1/auth/register", map[string]string{
		"invite_code": code,
		"username":    "fabio",
		"password":    "correct-horse-battery-staple",
	})
	if registerRes.StatusCode != http.StatusCreated {
		t.Fatalf("register: got status %d, want %d", registerRes.StatusCode, http.StatusCreated)
	}
	registered := decodeJSON[registerResponse](t, registerRes)
	if registered.Username != "fabio" {
		t.Errorf("registered.Username = %q, want %q", registered.Username, "fabio")
	}
	if !registered.IsAdmin {
		t.Error("registered.IsAdmin = false, want true (bootstrap invite grants admin)")
	}

	// The real assertion: the DB row itself, field by field — a bug that
	// only shows up in what actually got persisted wouldn't be caught by
	// checking the HTTP response alone.
	var (
		username         string
		isAdmin          bool
		quotaBytes       int64
		storageUsedBytes int64
		disabled         bool
	)
	err = ts.app.DB.QueryRowContext(ctx,
		`SELECT username, is_admin, quota_bytes, storage_used_bytes, disabled FROM users WHERE id = ?`,
		registered.ID,
	).Scan(&username, &isAdmin, &quotaBytes, &storageUsedBytes, &disabled)
	if err != nil {
		t.Fatalf("scan user row: %v", err)
	}
	if username != "fabio" || !isAdmin || storageUsedBytes != 0 || disabled {
		t.Errorf("user row = {username:%q isAdmin:%v storageUsedBytes:%d disabled:%v}, want {fabio true 0 false}",
			username, isAdmin, storageUsedBytes, disabled)
	}
	if quotaBytes <= 0 {
		t.Errorf("user row quotaBytes = %d, want > 0 (the server's default quota)", quotaBytes)
	}

	// the invite row itself must now show it was redeemed by this user
	var usedBy stdsql.NullString
	err = ts.app.DB.QueryRowContext(ctx, `SELECT used_by FROM invites WHERE code = ?`, code).Scan(&usedBy)
	if err != nil {
		t.Fatalf("scan invite row: %v", err)
	}
	if !usedBy.Valid || usedBy.String != registered.ID {
		t.Errorf("invite used_by = %+v, want %q", usedBy, registered.ID)
	}

	// a used invite code must not work a second time
	reuseRes := postJSON(t, ts.URL+"/api/v1/auth/register", map[string]string{
		"invite_code": code,
		"username":    "someoneelse",
		"password":    "correct-horse-battery-staple",
	})
	if reuseRes.StatusCode != http.StatusBadRequest {
		t.Errorf("re-registering with an already-used invite: got status %d, want %d",
			reuseRes.StatusCode, http.StatusBadRequest)
	}

	// --- login ---------------------------------------------------------------
	loginRes := postJSON(t, ts.URL+"/api/v1/auth/login", map[string]string{
		"username": "fabio",
		"password": "correct-horse-battery-staple",
	})
	if loginRes.StatusCode != http.StatusOK {
		t.Fatalf("login: got status %d, want %d", loginRes.StatusCode, http.StatusOK)
	}
	tokens := decodeJSON[tokenPairResponse](t, loginRes)
	if tokens.AccessToken == "" || tokens.RefreshToken == "" {
		t.Fatalf("login: got empty tokens: %+v", tokens)
	}

	issuer := token.NewIssuer(ts.jwtSecret)
	claims, err := issuer.ParseAccessToken(tokens.AccessToken)
	if err != nil {
		t.Fatalf("parse access token: %v", err)
	}
	if claims.UserID != registered.ID || !claims.IsAdmin {
		t.Errorf("access token claims = %+v, want UserID=%q IsAdmin=true", claims, registered.ID)
	}

	badLoginRes := postJSON(t, ts.URL+"/api/v1/auth/login", map[string]string{
		"username": "fabio",
		"password": "wrong-password",
	})
	if badLoginRes.StatusCode != http.StatusUnauthorized {
		t.Errorf("login with wrong password: got status %d, want %d",
			badLoginRes.StatusCode, http.StatusUnauthorized)
	}

	// --- refresh: rotates the token -------------------------------------------
	refreshRes := postJSON(t, ts.URL+"/api/v1/auth/refresh", map[string]string{
		"refresh_token": tokens.RefreshToken,
	})
	if refreshRes.StatusCode != http.StatusOK {
		t.Fatalf("refresh: got status %d, want %d", refreshRes.StatusCode, http.StatusOK)
	}
	rotated := decodeJSON[tokenPairResponse](t, refreshRes)
	if rotated.RefreshToken == tokens.RefreshToken {
		t.Error("refresh: got the same refresh token back, want a freshly rotated one")
	}

	// the old (rotated-away) refresh token must now be rejected
	reuseRefreshRes := postJSON(t, ts.URL+"/api/v1/auth/refresh", map[string]string{
		"refresh_token": tokens.RefreshToken,
	})
	if reuseRefreshRes.StatusCode != http.StatusUnauthorized {
		t.Errorf("reusing a rotated-away refresh token: got status %d, want %d",
			reuseRefreshRes.StatusCode, http.StatusUnauthorized)
	}

	// --- logout ----------------------------------------------------------------
	logoutRes := postJSON(t, ts.URL+"/api/v1/auth/logout", map[string]string{
		"refresh_token": rotated.RefreshToken,
	})
	if logoutRes.StatusCode != http.StatusNoContent {
		t.Fatalf("logout: got status %d, want %d", logoutRes.StatusCode, http.StatusNoContent)
	}

	postLogoutRefreshRes := postJSON(t, ts.URL+"/api/v1/auth/refresh", map[string]string{
		"refresh_token": rotated.RefreshToken,
	})
	if postLogoutRefreshRes.StatusCode != http.StatusUnauthorized {
		t.Errorf("refreshing after logout: got status %d, want %d",
			postLogoutRefreshRes.StatusCode, http.StatusUnauthorized)
	}
}

func TestAuthFlow_RegisterRejectsBadInput(t *testing.T) {
	ts := newTestServer(t)
	ctx := t.Context()

	code, created, err := ts.app.Auth.EnsureBootstrapInvite(ctx, time.Hour)
	if err != nil || !created {
		t.Fatalf("EnsureBootstrapInvite: code=%q created=%v err=%v", code, created, err)
	}

	cases := []struct {
		name       string
		inviteCode string
		username   string
		password   string
	}{
		{"unknown invite code", "does-not-exist", "someone", "correct-horse-battery-staple"},
		{"short username", code, "ab", "correct-horse-battery-staple"},
		{"short password", code, "someone", "short"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			res := postJSON(t, ts.URL+"/api/v1/auth/register", map[string]string{
				"invite_code": c.inviteCode,
				"username":    c.username,
				"password":    c.password,
			})
			if res.StatusCode != http.StatusBadRequest {
				t.Errorf("got status %d, want %d", res.StatusCode, http.StatusBadRequest)
			}
		})
	}
}
