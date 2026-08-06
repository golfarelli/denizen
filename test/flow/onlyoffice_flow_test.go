package flow

import (
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/golfarelli/denizen/internal/config"
)

type onlyOfficeStatusResp struct {
	Enabled  bool   `json:"enabled"`
	APIJSURL string `json:"api_js_url"`
}

type onlyOfficeConfigResp struct {
	Document struct {
		FileType    string `json:"fileType"`
		Key         string `json:"key"`
		Title       string `json:"title"`
		URL         string `json:"url"`
		Permissions struct {
			Edit     bool `json:"edit"`
			Download bool `json:"download"`
			Print    bool `json:"print"`
		} `json:"permissions"`
	} `json:"document"`
	DocumentType string `json:"documentType"`
	Type         string `json:"type"`
	Width        string `json:"width"`
	Height       string `json:"height"`
	Token        string `json:"token"`
}

func TestOnlyOfficeFlow_DisabledByDefault(t *testing.T) {
	ts := newTestServer(t) // no OnlyOffice config set — must behave as if the feature doesn't exist
	ctx := t.Context()

	code, created, err := ts.app.Auth.EnsureBootstrapInvite(ctx, time.Hour)
	if err != nil || !created {
		t.Fatalf("EnsureBootstrapInvite: code=%q created=%v err=%v", code, created, err)
	}
	fabio := registerAndLogin(t, ts, code, "fabio", "correct-horse-battery-staple")

	statusRes := authedRequest(t, http.MethodGet, ts.URL+"/api/v1/onlyoffice/status", fabio, nil)
	status := decodeJSON[onlyOfficeStatusResp](t, statusRes)
	if status.Enabled {
		t.Error("status.Enabled = true with no Document Server configured, want false")
	}

	uploaded := uploadFile(t, ts, fabio, nil, "report.docx", []byte("pretend docx bytes"))
	configRes := authedRequest(t, http.MethodGet, ts.URL+"/api/v1/items/"+uploaded.ID+"/onlyoffice-config", fabio, nil)
	if configRes.StatusCode != http.StatusNotFound {
		t.Errorf("config endpoint with OnlyOffice disabled: got status %d, want %d", configRes.StatusCode, http.StatusNotFound)
	}
}

func withOnlyOffice(url, jwtSecret, documentBaseURL string) func(*config.Config) {
	return func(cfg *config.Config) {
		cfg.OnlyOfficeURL = url
		cfg.OnlyOfficeJWTSecret = jwtSecret
		cfg.OnlyOfficeDocumentBaseURL = documentBaseURL
	}
}

func TestOnlyOfficeFlow_EnabledReportsStatusAndSignedConfig(t *testing.T) {
	ts := newTestServerWithConfig(t, withOnlyOffice(
		"http://onlyoffice.example.internal",
		"oo-jwt-secret",
		"http://denizen.example.internal",
	))
	ctx := t.Context()

	code, created, err := ts.app.Auth.EnsureBootstrapInvite(ctx, time.Hour)
	if err != nil || !created {
		t.Fatalf("EnsureBootstrapInvite: code=%q created=%v err=%v", code, created, err)
	}
	fabio := registerAndLogin(t, ts, code, "fabio", "correct-horse-battery-staple")

	statusRes := authedRequest(t, http.MethodGet, ts.URL+"/api/v1/onlyoffice/status", fabio, nil)
	status := decodeJSON[onlyOfficeStatusResp](t, statusRes)
	if !status.Enabled {
		t.Fatal("status.Enabled = false with a Document Server configured, want true")
	}
	wantAPIJSURL := "http://onlyoffice.example.internal/web-apps/apps/api/documents/api.js"
	if status.APIJSURL != wantAPIJSURL {
		t.Errorf("status.APIJSURL = %q, want %q", status.APIJSURL, wantAPIJSURL)
	}

	content := []byte("pretend docx bytes")
	uploaded := uploadFile(t, ts, fabio, nil, "Quarterly Report.docx", content)

	configRes := authedRequest(t, http.MethodGet, ts.URL+"/api/v1/items/"+uploaded.ID+"/onlyoffice-config", fabio, nil)
	if configRes.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(configRes.Body)
		t.Fatalf("onlyoffice-config: got status %d, body: %s", configRes.StatusCode, body)
	}
	cfg := decodeJSON[onlyOfficeConfigResp](t, configRes)

	if cfg.Document.FileType != "docx" {
		t.Errorf("Document.FileType = %q, want %q", cfg.Document.FileType, "docx")
	}
	if cfg.Document.Title != "Quarterly Report.docx" {
		t.Errorf("Document.Title = %q, want %q", cfg.Document.Title, "Quarterly Report.docx")
	}
	if cfg.DocumentType != "word" {
		t.Errorf("DocumentType = %q, want %q", cfg.DocumentType, "word")
	}
	if cfg.Document.Permissions.Edit {
		t.Error("Permissions.Edit = true — phase 1 is view-only, editing isn't wired up yet")
	}
	if cfg.Type != "desktop" {
		t.Errorf("Type = %q, want %q (no ?type= sent, so the default)", cfg.Type, "desktop")
	}
	if cfg.Width != "100%" || cfg.Height != "100%" {
		t.Errorf("Width/Height = %q/%q, want \"100%%\"/\"100%%\" — otherwise the editor iframe won't fill its container", cfg.Width, cfg.Height)
	}
	if !strings.HasPrefix(cfg.Document.URL, "http://denizen.example.internal/api/v1/items/"+uploaded.ID+"/content?token=") {
		t.Errorf("Document.URL = %q, want it built from the configured internal base URL with a content token", cfg.Document.URL)
	}

	// The real end-to-end check: the content token embedded in Document.URL
	// must actually work, the same way <video> relies on one — not just
	// look like a URL. Reusing it here against the real content endpoint
	// (with the internal base URL swapped for the test server's own,
	// since "denizen.example.internal" doesn't exist) confirms the token
	// this handler minted is genuinely valid, not just present.
	realURL := strings.Replace(cfg.Document.URL, "http://denizen.example.internal", ts.URL, 1)
	contentRes := getWithoutAuthHeader(t, realURL)
	if contentRes.StatusCode != http.StatusOK {
		t.Fatalf("fetching Document.URL: got status %d", contentRes.StatusCode)
	}
	body, err := io.ReadAll(contentRes.Body)
	if err != nil {
		t.Fatalf("read Document.URL response body: %v", err)
	}
	if string(body) != string(content) {
		t.Errorf("content fetched via Document.URL = %q, want %q", body, content)
	}

	// The signature actually verifies with the configured secret and
	// covers the config's own fields — not an empty/placeholder token.
	if cfg.Token == "" {
		t.Fatal("Token is empty despite a JWT secret being configured")
	}
	parsed, err := jwt.Parse(cfg.Token, func(token *jwt.Token) (interface{}, error) {
		return []byte("oo-jwt-secret"), nil
	})
	if err != nil || !parsed.Valid {
		t.Fatalf("config token did not verify with the configured secret: %v", err)
	}
	claims := parsed.Claims.(jwt.MapClaims)
	doc, ok := claims["document"].(map[string]interface{})
	if !ok || doc["title"] != "Quarterly Report.docx" {
		t.Errorf("signed token's own claims = %+v, want document.title = %q", claims, "Quarterly Report.docx")
	}
}

func TestOnlyOfficeFlow_EditorTypeFollowsQueryParam(t *testing.T) {
	ts := newTestServerWithConfig(t, withOnlyOffice("http://onlyoffice.example.internal", "", "http://denizen.example.internal"))
	ctx := t.Context()

	code, created, err := ts.app.Auth.EnsureBootstrapInvite(ctx, time.Hour)
	if err != nil || !created {
		t.Fatalf("EnsureBootstrapInvite: code=%q created=%v err=%v", code, created, err)
	}
	fabio := registerAndLogin(t, ts, code, "fabio", "correct-horse-battery-staple")
	uploaded := uploadFile(t, ts, fabio, nil, "notes.docx", []byte("notes"))

	// The client (OnlyOfficeViewer.svelte) is what actually decides this,
	// based on its own viewport — the server's only job is to trust and
	// sign whatever valid value it's asked for, or fall back to a sane
	// default for anything else.
	mobileRes := authedRequest(t, http.MethodGet, ts.URL+"/api/v1/items/"+uploaded.ID+"/onlyoffice-config?type=mobile", fabio, nil)
	mobileCfg := decodeJSON[onlyOfficeConfigResp](t, mobileRes)
	if mobileCfg.Type != "mobile" {
		t.Errorf("Type with ?type=mobile = %q, want %q", mobileCfg.Type, "mobile")
	}

	// An invalid value doesn't error the whole request — it just isn't
	// trusted, falling back to "desktop" the same as no ?type= at all.
	bogusRes := authedRequest(t, http.MethodGet, ts.URL+"/api/v1/items/"+uploaded.ID+"/onlyoffice-config?type=not-a-real-type", fabio, nil)
	bogusCfg := decodeJSON[onlyOfficeConfigResp](t, bogusRes)
	if bogusCfg.Type != "desktop" {
		t.Errorf("Type with an invalid ?type= = %q, want the %q fallback", bogusCfg.Type, "desktop")
	}
}

func TestOnlyOfficeFlow_RejectsUnsupportedExtension(t *testing.T) {
	ts := newTestServerWithConfig(t, withOnlyOffice("http://onlyoffice.example.internal", "", "http://denizen.example.internal"))
	ctx := t.Context()

	code, created, err := ts.app.Auth.EnsureBootstrapInvite(ctx, time.Hour)
	if err != nil || !created {
		t.Fatalf("EnsureBootstrapInvite: code=%q created=%v err=%v", code, created, err)
	}
	fabio := registerAndLogin(t, ts, code, "fabio", "correct-horse-battery-staple")

	uploaded := uploadFile(t, ts, fabio, nil, "archive.zip", []byte("PK\x03\x04"))
	res := authedRequest(t, http.MethodGet, ts.URL+"/api/v1/items/"+uploaded.ID+"/onlyoffice-config", fabio, nil)
	if res.StatusCode != http.StatusBadRequest {
		t.Errorf("config for .zip: got status %d, want %d", res.StatusCode, http.StatusBadRequest)
	}
}

func TestOnlyOfficeFlow_EnforcesOwnership(t *testing.T) {
	ts := newTestServerWithConfig(t, withOnlyOffice("http://onlyoffice.example.internal", "", "http://denizen.example.internal"))
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

	fabioFile := uploadFile(t, ts, fabio, nil, "private.xlsx", []byte("not yours"))
	res := authedRequest(t, http.MethodGet, ts.URL+"/api/v1/items/"+fabioFile.ID+"/onlyoffice-config", guest, nil)
	if res.StatusCode == http.StatusOK {
		t.Fatal("guest was able to get an OnlyOffice config for fabio's file")
	}
}
