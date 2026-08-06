package flow

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
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
	EditorConfig struct {
		Mode        string `json:"mode"`
		CallbackURL string `json:"callbackUrl"`
	} `json:"editorConfig"`
	Type   string `json:"type"`
	Width  string `json:"width"`
	Height string `json:"height"`
	Token  string `json:"token"`
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
	if !cfg.Document.Permissions.Edit {
		t.Error("Permissions.Edit = false, want true — phase 2 wires real editing up")
	}
	if cfg.EditorConfig.Mode != "edit" {
		t.Errorf("EditorConfig.Mode = %q, want %q", cfg.EditorConfig.Mode, "edit")
	}
	wantCallbackURL := "http://denizen.example.internal/api/v1/items/" + uploaded.ID + "/onlyoffice-callback"
	if cfg.EditorConfig.CallbackURL != wantCallbackURL {
		t.Errorf("EditorConfig.CallbackURL = %q, want %q", cfg.EditorConfig.CallbackURL, wantCallbackURL)
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

// TestOnlyOfficeFlow_PDF covers the extension OnlyOffice's PDF support
// added (onlyoffice.DocumentType) — everything else about the config
// (signing, permissions, callback URL) is already exercised generically by
// TestOnlyOfficeFlow_EnabledReportsStatusAndSignedConfig for docx, so this
// only checks what's actually different for a PDF: the DocumentType value
// itself. Deliberately doesn't cover images — OnlyOffice has no image
// support at all, see onlyoffice.DocumentType's own doc comment.
func TestOnlyOfficeFlow_PDF(t *testing.T) {
	ts := newTestServerWithConfig(t, withOnlyOffice("http://onlyoffice.example.internal", "", "http://denizen.example.internal"))
	ctx := t.Context()

	code, created, err := ts.app.Auth.EnsureBootstrapInvite(ctx, time.Hour)
	if err != nil || !created {
		t.Fatalf("EnsureBootstrapInvite: code=%q created=%v err=%v", code, created, err)
	}
	fabio := registerAndLogin(t, ts, code, "fabio", "correct-horse-battery-staple")

	uploaded := uploadFile(t, ts, fabio, nil, "Contract.pdf", []byte("%PDF-1.4 pretend bytes"))
	res := authedRequest(t, http.MethodGet, ts.URL+"/api/v1/items/"+uploaded.ID+"/onlyoffice-config", fabio, nil)
	if res.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(res.Body)
		t.Fatalf("config for .pdf: got status %d, body: %s", res.StatusCode, body)
	}
	cfg := decodeJSON[onlyOfficeConfigResp](t, res)
	if cfg.DocumentType != "pdf" {
		t.Errorf("DocumentType = %q, want %q", cfg.DocumentType, "pdf")
	}
	if cfg.Document.FileType != "pdf" {
		t.Errorf("Document.FileType = %q, want %q", cfg.Document.FileType, "pdf")
	}
	if !cfg.Document.Permissions.Edit {
		t.Error("Permissions.Edit = false for a PDF, want true — same edit-enabled config as every other supported type")
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

// onlyOfficeCallbackBody mirrors handler.onlyOfficeCallbackBody — this is a
// different package (a real Document Server has no idea this codebase's
// internal types exist either), so it gets its own copy rather than
// importing internal/handler.
type onlyOfficeCallbackBody struct {
	Status int    `json:"status"`
	URL    string `json:"url,omitempty"`
}

// signOnlyOfficeCallback signs body the same way onlyoffice.Client.Sign
// signs an editor config — the payload itself becomes the JWT's claims —
// since that's what a real Document Server does for its own callback
// requests too.
func signOnlyOfficeCallback(t *testing.T, secret string, body onlyOfficeCallbackBody) string {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal callback body: %v", err)
	}
	var claims jwt.MapClaims
	if err := json.Unmarshal(raw, &claims); err != nil {
		t.Fatalf("callback body to claims: %v", err)
	}
	tok, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(secret))
	if err != nil {
		t.Fatalf("sign callback JWT: %v", err)
	}
	return tok
}

// postOnlyOfficeCallback POSTs body to callbackURL, JWT-signed with secret
// exactly as a real Document Server would (Authorization: Bearer header —
// see onlyoffice.Client.VerifyCallback).
func postOnlyOfficeCallback(t *testing.T, callbackURL, secret string, body onlyOfficeCallbackBody) *http.Response {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal callback body: %v", err)
	}
	req, err := http.NewRequest(http.MethodPost, callbackURL, bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("build callback request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if secret != "" {
		req.Header.Set("Authorization", "Bearer "+signOnlyOfficeCallback(t, secret, body))
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST callback: %v", err)
	}
	return res
}

func TestOnlyOfficeFlow_CallbackSavesDocument(t *testing.T) {
	const secret = "oo-jwt-secret"
	ts := newTestServerWithConfig(t, withOnlyOffice(
		"http://onlyoffice.example.internal",
		secret,
		"http://denizen.example.internal",
	))
	ctx := t.Context()

	code, created, err := ts.app.Auth.EnsureBootstrapInvite(ctx, time.Hour)
	if err != nil || !created {
		t.Fatalf("EnsureBootstrapInvite: code=%q created=%v err=%v", code, created, err)
	}
	fabio := registerAndLogin(t, ts, code, "fabio", "correct-horse-battery-staple")

	original := []byte("original spreadsheet bytes")
	item := uploadFile(t, ts, fabio, nil, "Budget.xlsx", original)

	configRes := authedRequest(t, http.MethodGet, ts.URL+"/api/v1/items/"+item.ID+"/onlyoffice-config", fabio, nil)
	cfg := decodeJSON[onlyOfficeConfigResp](t, configRes)
	callbackURL := strings.Replace(cfg.EditorConfig.CallbackURL, "http://denizen.example.internal", ts.URL, 1)

	// Stands in for the Document Server's own storage, which is where a
	// real callback's "url" field points — Denizen has to fetch the edited
	// bytes from there, it isn't handed them directly in the callback body.
	edited := []byte("edited spreadsheet bytes, now longer than the original")
	editedServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(edited)
	}))
	t.Cleanup(editedServer.Close)

	callbackRes := postOnlyOfficeCallback(t, callbackURL, secret, onlyOfficeCallbackBody{
		Status: 2, // ready to save
		URL:    editedServer.URL,
	})
	if callbackRes.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(callbackRes.Body)
		t.Fatalf("callback: got status %d, body: %s", callbackRes.StatusCode, body)
	}
	var callbackBody struct {
		Error int `json:"error"`
	}
	if err := json.NewDecoder(callbackRes.Body).Decode(&callbackBody); err != nil {
		t.Fatalf("decode callback response: %v", err)
	}
	if callbackBody.Error != 0 {
		t.Errorf("callback response error = %d, want 0", callbackBody.Error)
	}

	// The real assertion: the item's actual content on disk changed, not
	// just that the callback returned success.
	contentRes := authedRequest(t, http.MethodGet, ts.URL+"/api/v1/items/"+item.ID+"/content", fabio, nil)
	contentBody, err := io.ReadAll(contentRes.Body)
	if err != nil {
		t.Fatalf("read content: %v", err)
	}
	if !bytes.Equal(contentBody, edited) {
		t.Errorf("content after callback = %q, want %q", contentBody, edited)
	}

	updated := decodeJSON[apiItem](t, authedRequest(t, http.MethodGet, ts.URL+"/api/v1/items/"+item.ID, fabio, nil))
	if updated.SizeBytes != int64(len(edited)) {
		t.Errorf("SizeBytes after callback = %d, want %d", updated.SizeBytes, len(edited))
	}
	// >=, not >: UpdatedAt has one-second resolution (time.Time.Unix()), and
	// upload+callback both landing in the same wall-clock second is a real,
	// non-buggy possibility in a fast-running test — SizeBytes above is
	// already the assertion that the row's content metadata really updated.
	if updated.UpdatedAt < item.UpdatedAt {
		t.Errorf("UpdatedAt after callback = %d, want it not to have gone backwards from the original %d", updated.UpdatedAt, item.UpdatedAt)
	}
}

func TestOnlyOfficeFlow_CallbackRejectsInvalidSignature(t *testing.T) {
	const secret = "oo-jwt-secret"
	ts := newTestServerWithConfig(t, withOnlyOffice(
		"http://onlyoffice.example.internal",
		secret,
		"http://denizen.example.internal",
	))
	ctx := t.Context()

	code, created, err := ts.app.Auth.EnsureBootstrapInvite(ctx, time.Hour)
	if err != nil || !created {
		t.Fatalf("EnsureBootstrapInvite: code=%q created=%v err=%v", code, created, err)
	}
	fabio := registerAndLogin(t, ts, code, "fabio", "correct-horse-battery-staple")

	original := []byte("do not touch me")
	item := uploadFile(t, ts, fabio, nil, "Contract.docx", original)

	editedServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("forged edit"))
	}))
	t.Cleanup(editedServer.Close)

	callbackURL := ts.URL + "/api/v1/items/" + item.ID + "/onlyoffice-callback"

	// No Authorization header at all — the most basic forgery attempt, and
	// exactly what a plain POST from anywhere on the internet would send.
	res := postOnlyOfficeCallback(t, callbackURL, "" /* no secret => no header */, onlyOfficeCallbackBody{
		Status: 2,
		URL:    editedServer.URL,
	})
	if res.StatusCode == http.StatusOK {
		t.Fatal("callback with no Authorization header was accepted")
	}

	// And the content genuinely wasn't touched — not just that the HTTP
	// response looked like a rejection.
	contentRes := authedRequest(t, http.MethodGet, ts.URL+"/api/v1/items/"+item.ID+"/content", fabio, nil)
	contentBody, err := io.ReadAll(contentRes.Body)
	if err != nil {
		t.Fatalf("read content: %v", err)
	}
	if !bytes.Equal(contentBody, original) {
		t.Errorf("content changed despite an unauthenticated callback: got %q, want original %q", contentBody, original)
	}
}
