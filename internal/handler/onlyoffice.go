package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
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

// editorMode maps a caller's write access to the Document Server's own
// mode string — "view" renders read-only with no save affordance at all,
// which is what a view-only share grant (or no grant, browsing as a
// public link) should get instead of a real editor.
func editorMode(canEdit bool) string {
	if canEdit {
		return "edit"
	}
	return "view"
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
// OnlyOffice editor config for one item, with real editing wired up (mode:
// "edit" plus a callbackUrl — see Callback below).
func (h *OnlyOfficeHandler) Config(res http.ResponseWriter, req *http.Request) {
	if !h.oo.Enabled() {
		httpio.WriteError(res, apperr.NotFound)
		return
	}

	// ResolveContentItem, not GetIncludingTrashed directly: a shortcut to a
	// file resolves (and re-verifies the caller's *current* access) to the
	// real target here — canEdit below, Key's cache-busting UpdatedAt, and
	// every URL built past this point all end up keyed on the real item's
	// own id, never the shortcut's. Using the shortcut's own row for any of
	// that would be wrong two ways at once: its own UpdatedAt never changes
	// when the real content does (a permanently-stale OnlyOffice cache key),
	// and — the more serious one — a shortcut is always "owned" by whoever
	// created it, so CanEdit against the shortcut's own row would read true
	// even for a shortcut to something only shared with them at view-only,
	// silently handing out edit access a direct share never granted.
	item, err := h.items.ResolveContentItem(req.Context(), ownerID(req), req.PathValue("id"))
	if err != nil {
		httpio.WriteError(res, err)
		return
	}
	id := item.ID
	if item.DeletedAt != nil {
		// GetIncludingTrashed (inside ResolveContentItem) lets the owner
		// preview a trashed item — never editing one, though, same as the
		// strict Get this used before.
		httpio.WriteError(res, apperr.NotFound)
		return
	}
	canEdit, err := h.items.CanEdit(req.Context(), ownerID(req), item)
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
			// Changes whenever the file's content does — including right
			// after Callback below saves an edit — so the Document Server
			// knows a previously-cached copy (its own or another open tab's)
			// is stale rather than reusing it.
			Key:   id + "-" + strconv.FormatInt(item.UpdatedAt, 10),
			Title: item.Name,
			URL:   h.oo.DocumentURL(id, contentToken),
			Permissions: onlyoffice.Permissions{
				Edit:     canEdit,
				Download: true,
				Print:    true,
			},
		},
		DocumentType: docType,
		EditorConfig: onlyoffice.EditorSettings{
			Mode:        editorMode(canEdit),
			CallbackURL: h.oo.CallbackURL(id),
			User:        onlyoffice.UserInfo{ID: ownerID(req), Name: ownerID(req)},
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

// onlyOfficeCallbackBody mirrors the fields this handler actually reads
// from OnlyOffice's own callback payload — see
// https://api.onlyoffice.com/docs/document-server/website/callback-handlers/.
type onlyOfficeCallbackBody struct {
	Status int    `json:"status"`
	URL    string `json:"url,omitempty"` // where to download the saved document from — present for status 2/6
}

// Per OnlyOffice's own callback status codes.
const (
	onlyOfficeStatusSave      = 2 // editing finished, document is ready to save
	onlyOfficeStatusForceSave = 6 // still being edited, but a force-save was requested
)

// maxOnlyOfficeCallbackBodyBytes bounds the callback JSON itself (small
// metadata — status, a URL, a key), not the document it might point at,
// which Callback fetches separately.
const maxOnlyOfficeCallbackBodyBytes = 1 << 16 // 64KiB

// Callback handles POST /api/v1/items/{id}/onlyoffice-callback — called by
// the Document Server itself, not a logged-in user, so it's mounted
// without RequireAuth (see router.New) and authenticated a different way:
// a JWT signed with the same shared secret used to sign the editor config
// in Config above (see onlyoffice.Client.VerifyCallback). id in the URL
// path was itself only ever handed out inside that signed config, as part
// of the document/callback URLs it contains.
func (h *OnlyOfficeHandler) Callback(res http.ResponseWriter, req *http.Request) {
	if !h.oo.Enabled() {
		httpio.WriteError(res, apperr.NotFound)
		return
	}
	id := req.PathValue("id")

	body, err := io.ReadAll(io.LimitReader(req.Body, maxOnlyOfficeCallbackBodyBytes+1))
	if err != nil {
		httpio.WriteError(res, apperr.Validation("could not read callback body"))
		return
	}
	if len(body) > maxOnlyOfficeCallbackBodyBytes {
		httpio.WriteError(res, apperr.Validation("callback body too large"))
		return
	}

	if err := h.oo.VerifyCallback(req.Header.Get("Authorization"), body); err != nil {
		httpio.WriteError(res, apperr.Unauthorized)
		return
	}

	var payload onlyOfficeCallbackBody
	if err := json.Unmarshal(body, &payload); err != nil {
		httpio.WriteError(res, apperr.Validation("invalid callback body"))
		return
	}

	// Every other status (still editing, closed with no changes, a save
	// error the Document Server is only informing us of) needs no action
	// here — {"error":0} just acknowledges receipt, which the switch below
	// falls through to.
	if payload.Status == onlyOfficeStatusSave || payload.Status == onlyOfficeStatusForceSave {
		if err := h.saveCallbackDocument(req.Context(), id, payload.URL); err != nil {
			// error:1 (any nonzero value) tells the Document Server the save
			// failed, so it keeps its own copy of the edit instead of
			// discarding it — see the API docs linked above.
			httpio.WriteJSON(res, http.StatusOK, map[string]int{"error": 1})
			return
		}
	}

	httpio.WriteJSON(res, http.StatusOK, map[string]int{"error": 0})
}

// saveCallbackDocument downloads the edited document from documentURL (a
// short-lived URL on the Document Server's own storage, provided in the
// callback payload — not one Denizen minted) and writes it over itemID's
// content. The item's real owner is looked up from the DB rather than
// trusted from anywhere in the request, since this whole path runs with no
// authenticated user at all.
func (h *OnlyOfficeHandler) saveCallbackDocument(ctx context.Context, itemID, documentURL string) error {
	if documentURL == "" {
		return fmt.Errorf("onlyoffice: save callback missing document url")
	}

	item, err := h.items.GetForShare(ctx, itemID)
	if err != nil {
		return err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, documentURL, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("onlyoffice: fetching saved document: unexpected status %d", resp.StatusCode)
	}

	_, err = h.items.ReplaceContent(ctx, item.OwnerID, itemID, resp.Body)
	return err
}
