// Package onlyoffice builds signed editor configs for an OnlyOffice
// Document Server — the optional, genuinely heavy separate service that
// unlocks real in-browser Word/Excel/PowerPoint editing, the same way
// Nextcloud's own "Nextcloud Office" integration works. Denizen never
// bundles or requires it: this package's own Client is nil-safe by design
// (Enabled() reports false, and every other method is simply never called
// by a caller that checks it first — see internal/handler/onlyoffice.go),
// so the whole feature compiles down to nothing when unconfigured.
//
// This is phase 1 (docs/ARCHITECTURE.md's "OnlyOffice integration"): open
// a document for viewing. Editing (mode: "edit", the save-back callback,
// and validating it) is deliberately not built yet — a real second phase,
// not a corner cut here.
package onlyoffice

import (
	"encoding/json"

	"github.com/golang-jwt/jwt/v5"
)

// Client knows how to reach a configured Document Server and sign the
// configs it needs. The zero value (via New("", "", "")) is a valid,
// disabled client — Enabled() is what every caller checks first.
type Client struct {
	url             string
	jwtSecret       []byte
	documentBaseURL string
}

func New(url, jwtSecret, documentBaseURL string) *Client {
	return &Client{url: url, jwtSecret: []byte(jwtSecret), documentBaseURL: documentBaseURL}
}

// Enabled reports whether an OnlyOffice Document Server is actually
// configured. Safe to call on a nil *Client (a handler that never got one
// wired in behaves the same as one explicitly configured off).
func (c *Client) Enabled() bool {
	return c != nil && c.url != ""
}

// APIJSURL is the Document Server's own editor bootstrap script — loaded
// directly by the browser (not proxied through Denizen), since it must
// match whatever Document Server version is actually running.
func (c *Client) APIJSURL() string {
	return c.url + "/web-apps/apps/api/documents/api.js"
}

// DocumentURL builds the address the Document Server itself (not a
// browser) uses to fetch itemID's content, carrying contentToken as its
// auth (see internal/token.ContentClaims) — built from
// DENIZEN_ONLYOFFICE_DOCUMENT_BASE_URL, not whatever host a browser
// reached this app on, since the two are commonly different addresses on
// a Docker network (see Config's own field doc comment).
func (c *Client) DocumentURL(itemID, contentToken string) string {
	return c.documentBaseURL + "/api/v1/items/" + itemID + "/content?token=" + contentToken
}

// DocumentType maps a file extension to the three editor families
// OnlyOffice has — required by EditorConfig.DocumentType, which picks the
// editor UI/toolset shown, distinct from the file's own format.
func DocumentType(ext string) (string, bool) {
	switch ext {
	case "docx":
		return "word", true
	case "xlsx", "xls":
		return "cell", true
	case "pptx":
		return "slide", true
	default:
		return "", false
	}
}

// ValidEditorType reports whether t is one of OnlyOffice's own accepted
// values for EditorConfig.Type — anything else (including empty/absent,
// the common case of a caller not sending ?type= at all) falls back to
// "desktop" rather than erroring; a caller getting this wrong shouldn't
// break document access, just pick the least-surprising default.
func ValidEditorType(t string) bool {
	return t == "desktop" || t == "mobile" || t == "embedded"
}

// EditorConfig is the JSON object OnlyOffice's DocsAPI.DocEditor expects —
// see https://api.onlyoffice.com/docs/docs-api/get-started/config/.
type EditorConfig struct {
	Document     DocumentConfig `json:"document"`
	DocumentType string         `json:"documentType"`
	EditorConfig EditorSettings `json:"editorConfig"`
	// Type picks OnlyOffice's own UI: "desktop" is the full ribbon
	// interface (real toolbar buttons, sized for a mouse) — squeezed into
	// a phone-width viewport it renders tiny and cramped, confirmed live.
	// "mobile" is the touch-sized alternative purpose-built for this,
	// which the client requests via ?type= on the config endpoint (see
	// ItemHandler's caller — handler/onlyoffice.go) based on its own
	// viewport width, not decided here.
	Type string `json:"type"`
	// Width/Height as percentages make the mounted iframe actually fill
	// its container — without these, DocsAPI.DocEditor doesn't reliably
	// stretch to fit the parent element's own size.
	Width  string `json:"width,omitempty"`
	Height string `json:"height,omitempty"`
	// Token is populated by Sign, never set directly — it's a signature
	// *over* the rest of this struct, so it has to be computed after
	// everything else is final.
	Token string `json:"token,omitempty"`
}

type DocumentConfig struct {
	FileType    string      `json:"fileType"`
	Key         string      `json:"key"`
	Title       string      `json:"title"`
	URL         string      `json:"url"`
	Permissions Permissions `json:"permissions"`
}

type Permissions struct {
	Edit     bool `json:"edit"`
	Download bool `json:"download"`
	Print    bool `json:"print"`
}

type EditorSettings struct {
	Mode string   `json:"mode"` // "view" — always, for now; see package doc
	User UserInfo `json:"user"`
}

type UserInfo struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Sign returns a JWT over cfg's own fields, the way OnlyOffice's security
// model requires whenever JWT is enabled Document Server-side (which any
// real deployment should — an unsigned config lets anyone who can reach
// the Document Server point it at an arbitrary URL). Returns "" without
// error if no secret is configured, since JWT signing is recommended but
// not strictly required by OnlyOffice itself — callers just pass that
// through as EditorConfig.Token unchanged.
func (c *Client) Sign(cfg EditorConfig) (string, error) {
	if len(c.jwtSecret) == 0 {
		return "", nil
	}

	// The payload is the config object itself — not a purpose-built claims
	// struct — because that's what the Document Server verifies the
	// signature against. Round-tripping through JSON into a MapClaims is
	// simpler and less error-prone than hand-duplicating EditorConfig's
	// shape as jwt.MapClaims entries.
	raw, err := json.Marshal(cfg)
	if err != nil {
		return "", err
	}
	var claims jwt.MapClaims
	if err := json.Unmarshal(raw, &claims); err != nil {
		return "", err
	}

	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(c.jwtSecret)
}
