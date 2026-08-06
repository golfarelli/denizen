package handler

import (
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/golfarelli/denizen/internal/apperr"
	"github.com/golfarelli/denizen/internal/httpio"
	"github.com/golfarelli/denizen/internal/onlyoffice"
	"github.com/golfarelli/denizen/internal/service"
	"github.com/golfarelli/denizen/internal/token"
)

// OnlyOfficeHandler exposes /api/v1/onlyoffice/status and
// /api/v1/items/{id}/onlyoffice-config. Both routes work — and report
// "disabled" gracefully rather than erroring — even when no Document
// Server is configured at all (onlyoffice.Client.Enabled reports false on
// a nil/unconfigured *Client), so wiring this handler in is always safe.
type OnlyOfficeHandler struct {
	items  *service.ItemService
	oo     *onlyoffice.Client
	tokens *token.Issuer
}

func NewOnlyOfficeHandler(items *service.ItemService, oo *onlyoffice.Client, tokens *token.Issuer) *OnlyOfficeHandler {
	return &OnlyOfficeHandler{items: items, oo: oo, tokens: tokens}
}

// onlyOfficeContentTokenTTL doesn't need to be as generous as video's own
// (internal/handler/item.go) — the Document Server fetches the document
// once up front, not continuously the way a long video stream does.
const onlyOfficeContentTokenTTL = 10 * time.Minute

type onlyOfficeStatusResponse struct {
	Enabled  bool   `json:"enabled"`
	APIJSURL string `json:"api_js_url,omitempty"`
}

// Status handles GET /api/v1/onlyoffice/status — how the frontend decides
// whether to offer the OnlyOffice editor at all before ever calling Config.
func (h *OnlyOfficeHandler) Status(res http.ResponseWriter, req *http.Request) {
	if !h.oo.Enabled() {
		httpio.WriteJSON(res, http.StatusOK, onlyOfficeStatusResponse{Enabled: false})
		return
	}
	httpio.WriteJSON(res, http.StatusOK, onlyOfficeStatusResponse{Enabled: true, APIJSURL: h.oo.APIJSURL()})
}

// Config handles GET /api/v1/items/{id}/onlyoffice-config — mints a signed
// OnlyOffice editor config for one item, view-only (see package onlyoffice's
// own doc comment on why editing isn't wired up yet).
func (h *OnlyOfficeHandler) Config(res http.ResponseWriter, req *http.Request) {
	if !h.oo.Enabled() {
		httpio.WriteError(res, apperr.NotFound)
		return
	}

	id := req.PathValue("id")
	item, err := h.items.Get(req.Context(), ownerID(req), id)
	if err != nil {
		httpio.WriteError(res, err)
		return
	}

	ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(item.Name)), ".")
	docType, ok := onlyoffice.DocumentType(ext)
	if !ok {
		httpio.WriteError(res, apperr.Validation("unsupported document type for OnlyOffice"))
		return
	}

	// The caller's own viewport decides this (see
	// OnlyOfficeViewer.svelte) — the "desktop" ribbon UI, OnlyOffice's
	// own default, renders tiny and cramped squeezed into a phone-width
	// screen (confirmed live); "mobile" is its purpose-built alternative.
	// Read here, not overridden client-side after the fact, because it
	// has to be part of what Sign below actually signs.
	editorType := req.URL.Query().Get("type")
	if !onlyoffice.ValidEditorType(editorType) {
		editorType = "desktop"
	}

	// The Document Server — not the browser — fetches this URL itself, so
	// it rides on the same content-token mechanism <video> uses
	// (internal/token.ContentClaims) rather than the Document Server ever
	// seeing a Denizen session's own bearer token.
	contentToken, err := h.tokens.NewContentToken(ownerID(req), id, onlyOfficeContentTokenTTL)
	if err != nil {
		httpio.WriteError(res, apperr.Internal)
		return
	}

	cfg := onlyoffice.EditorConfig{
		Document: onlyoffice.DocumentConfig{
			FileType: ext,
			// Changes whenever the file's content does (once saving exists —
			// see package onlyoffice — this is what tells the Document
			// Server a previously-cached copy is stale).
			Key:   id + "-" + strconv.FormatInt(item.UpdatedAt, 10),
			Title: item.Name,
			URL:   h.oo.DocumentURL(id, contentToken),
			Permissions: onlyoffice.Permissions{
				Edit:     false,
				Download: true,
				Print:    true,
			},
		},
		DocumentType: docType,
		EditorConfig: onlyoffice.EditorSettings{
			Mode: "view",
			User: onlyoffice.UserInfo{ID: ownerID(req), Name: ownerID(req)},
		},
		Type:   editorType,
		Width:  "100%",
		Height: "100%",
	}

	signed, err := h.oo.Sign(cfg)
	if err != nil {
		httpio.WriteError(res, apperr.Internal)
		return
	}
	cfg.Token = signed

	httpio.WriteJSON(res, http.StatusOK, cfg)
}
