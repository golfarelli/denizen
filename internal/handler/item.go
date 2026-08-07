package handler

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/golfarelli/denizen/internal/apperr"
	"github.com/golfarelli/denizen/internal/httpio"
	"github.com/golfarelli/denizen/internal/middleware"
	"github.com/golfarelli/denizen/internal/model"
	"github.com/golfarelli/denizen/internal/service"
	"github.com/golfarelli/denizen/internal/token"
)

// ItemHandler exposes /api/v1/items* and /api/v1/trash*. Every route here
// is expected to run behind middleware.RequireAuth, except .../content —
// see middleware.RequireAuthOrContentToken and ContentToken below.
type ItemHandler struct {
	items  *service.ItemService
	tokens *token.Issuer // only for ContentToken
}

func NewItemHandler(items *service.ItemService, tokens *token.Issuer) *ItemHandler {
	return &ItemHandler{items: items, tokens: tokens}
}

// contentTokenTTL is generous relative to the access token's own default
// (15 minutes) on purpose: it's scoped to exactly one item's content (see
// middleware.RequireAuthOrContentToken), so the blast radius of a leaked
// one is far narrower — and cutting a video's playback short mid-watch
// because the token it's streaming through expired would be a worse
// tradeoff than that narrow extra exposure window.
const contentTokenTTL = time.Hour

type itemResponse struct {
	ID        string  `json:"id"`
	ParentID  *string `json:"parent_id"`
	Name      string  `json:"name"`
	Type      string  `json:"type"`
	SizeBytes int64   `json:"size_bytes"`
	MimeType  *string `json:"mime_type,omitempty"`
	CreatedAt int64   `json:"created_at"`
	UpdatedAt int64   `json:"updated_at"`
	DeletedAt *int64  `json:"deleted_at,omitempty"`
	// Owned is false only when callerID reached this item through a direct
	// share grant (model.UserShare), not ownership — every other call site
	// below only ever deals in the caller's own items, where it's always
	// true. The frontend's file preview page uses this to hide the
	// mutating actions (Rename/Move/Delete/...) a grant recipient has no
	// right to anyway — see routes/file/[id]/+page.svelte.
	Owned bool `json:"owned"`
}

func toItemResponse(item *model.Item, callerID string) itemResponse {
	return itemResponse{
		ID:        item.ID,
		ParentID:  item.ParentID,
		Name:      item.Name,
		Type:      string(item.Type),
		SizeBytes: item.SizeBytes,
		MimeType:  item.MimeType,
		CreatedAt: item.CreatedAt,
		UpdatedAt: item.UpdatedAt,
		DeletedAt: item.DeletedAt,
		Owned:     item.OwnerID == callerID,
	}
}

func toItemResponses(items []*model.Item, callerID string) []itemResponse {
	out := make([]itemResponse, len(items))
	for i, item := range items {
		out[i] = toItemResponse(item, callerID)
	}
	return out
}

// ownerID reads the authenticated user's ID out of the request context —
// always present on a route wrapped with middleware.RequireAuth.
func ownerID(req *http.Request) string {
	claims, _ := middleware.ClaimsFromContext(req.Context())
	return claims.UserID
}

type createItemRequest struct {
	Type     string  `json:"type"`
	Name     string  `json:"name"`
	ParentID *string `json:"parent_id"`
}

// Create currently only supports creating folders — a file needs bytes, not
// just a name, so files are created via the (upcoming) upload endpoint
// instead. The request still carries "type" so that endpoint's shape isn't
// a surprise later.
func (h *ItemHandler) Create(res http.ResponseWriter, req *http.Request) {
	var in createItemRequest
	if err := json.NewDecoder(req.Body).Decode(&in); err != nil {
		httpio.WriteError(res, apperr.Validation("invalid JSON body"))
		return
	}
	if in.Type != string(model.ItemTypeFolder) {
		httpio.WriteError(res, apperr.Validation(`type must be "folder" (files are created via upload)`))
		return
	}

	item, err := h.items.CreateFolder(req.Context(), ownerID(req), in.ParentID, in.Name)
	if err != nil {
		httpio.WriteError(res, err)
		return
	}
	httpio.WriteJSON(res, http.StatusCreated, toItemResponse(item, ownerID(req)))
}

// List handles GET /api/v1/items?parent_id=... — parent_id omitted or empty
// means the user's root folder.
func (h *ItemHandler) List(res http.ResponseWriter, req *http.Request) {
	var parentID *string
	if v := req.URL.Query().Get("parent_id"); v != "" {
		parentID = &v
	}

	items, err := h.items.ListChildren(req.Context(), ownerID(req), parentID)
	if err != nil {
		httpio.WriteError(res, err)
		return
	}
	httpio.WriteJSON(res, http.StatusOK, toItemResponses(items, ownerID(req)))
}

// Get handles GET /api/v1/items/{id} — deliberately allows a trashed item
// too (GetIncludingTrashed, not Get): previewing something in the trash
// before deciding whether to restore or delete it forever is exactly what
// Trash's own "open to view" needs, and this is the same read a normal
// preview does. Mutating routes (Move, Copy, ...) all check via Get
// directly, in internal/service, and keep rejecting a trashed item.
func (h *ItemHandler) Get(res http.ResponseWriter, req *http.Request) {
	item, err := h.items.GetIncludingTrashed(req.Context(), ownerID(req), req.PathValue("id"))
	if err != nil {
		httpio.WriteError(res, err)
		return
	}
	httpio.WriteJSON(res, http.StatusOK, toItemResponse(item, ownerID(req)))
}

// Content handles GET /api/v1/items/{id}/content — streams a file's bytes,
// with Range support (via http.ServeContent, so a paused download or a
// video/PDF preview seeking around doesn't need custom byte-range logic
// here). Trashed items are readable here too — see Get's own comment.
func (h *ItemHandler) Content(res http.ResponseWriter, req *http.Request) {
	item, err := h.items.GetIncludingTrashed(req.Context(), ownerID(req), req.PathValue("id"))
	if err != nil {
		httpio.WriteError(res, err)
		return
	}
	path, err := h.items.FilePath(req.Context(), item)
	if err != nil {
		httpio.WriteError(res, err)
		return
	}
	serveFileContent(res, req, item, path)
}

type contentTokenResponse struct {
	Token     string `json:"token"`
	ExpiresAt int64  `json:"expires_at"`
}

// ContentToken handles POST /api/v1/items/{id}/content-token — mints a
// short-lived token good only for GETting this one item's content via a
// query parameter (?token=...), for <video>/<audio> elements that can't
// attach the Authorization header this app's own fetch() calls use
// instead (see lib/api.ts's downloadContent, used by everything else).
// Trashed items included, same as Get/Content above — a trashed video
// still needs to be previewable via <video>, which is what this token is
// for.
func (h *ItemHandler) ContentToken(res http.ResponseWriter, req *http.Request) {
	id := req.PathValue("id")
	// Same ownership check every other item route already goes through —
	// confirms the item exists and actually belongs to the caller before
	// minting anything for it.
	if _, err := h.items.GetIncludingTrashed(req.Context(), ownerID(req), id); err != nil {
		httpio.WriteError(res, err)
		return
	}

	raw, err := h.tokens.NewContentToken(ownerID(req), id, contentTokenTTL)
	if err != nil {
		httpio.WriteError(res, apperr.Internal)
		return
	}
	httpio.WriteJSON(res, http.StatusOK, contentTokenResponse{
		Token:     raw,
		ExpiresAt: time.Now().Add(contentTokenTTL).Unix(),
	})
}

type moveRequest struct {
	Name     string  `json:"name"`
	ParentID *string `json:"parent_id"`
}

// Move handles PATCH /api/v1/items/{id} — a full replacement of the item's
// name and location, like this codebase's other PATCH endpoints (both
// fields are always supplied, never merged in partially).
func (h *ItemHandler) Move(res http.ResponseWriter, req *http.Request) {
	var in moveRequest
	if err := json.NewDecoder(req.Body).Decode(&in); err != nil {
		httpio.WriteError(res, apperr.Validation("invalid JSON body"))
		return
	}

	item, err := h.items.Move(req.Context(), ownerID(req), req.PathValue("id"), service.MoveInput{
		Name:     in.Name,
		ParentID: in.ParentID,
	})
	if err != nil {
		httpio.WriteError(res, err)
		return
	}
	httpio.WriteJSON(res, http.StatusOK, toItemResponse(item, ownerID(req)))
}

// Delete handles DELETE /api/v1/items/{id} — moves the item to trash (soft
// delete), it isn't gone for good until DeletePermanently.
func (h *ItemHandler) Delete(res http.ResponseWriter, req *http.Request) {
	if err := h.items.Delete(req.Context(), ownerID(req), req.PathValue("id")); err != nil {
		httpio.WriteError(res, err)
		return
	}
	res.WriteHeader(http.StatusNoContent)
}

type copyRequest struct {
	ParentID *string `json:"parent_id"`
}

// Copy handles POST /api/v1/items/{id}/copy. parent_id nil/absent means
// root, consistently with every other endpoint that takes one — it is the
// caller's job to pass the item's own current parent for a same-folder
// "make a copy", not this endpoint's.
func (h *ItemHandler) Copy(res http.ResponseWriter, req *http.Request) {
	var in copyRequest
	if err := json.NewDecoder(req.Body).Decode(&in); err != nil {
		httpio.WriteError(res, apperr.Validation("invalid JSON body"))
		return
	}

	item, err := h.items.Copy(req.Context(), ownerID(req), req.PathValue("id"), in.ParentID)
	if err != nil {
		httpio.WriteError(res, err)
		return
	}
	httpio.WriteJSON(res, http.StatusCreated, toItemResponse(item, ownerID(req)))
}

func (h *ItemHandler) ListTrash(res http.ResponseWriter, req *http.Request) {
	items, err := h.items.ListTrash(req.Context(), ownerID(req))
	if err != nil {
		httpio.WriteError(res, err)
		return
	}
	httpio.WriteJSON(res, http.StatusOK, toItemResponses(items, ownerID(req)))
}

func (h *ItemHandler) Restore(res http.ResponseWriter, req *http.Request) {
	item, err := h.items.Restore(req.Context(), ownerID(req), req.PathValue("id"))
	if err != nil {
		httpio.WriteError(res, err)
		return
	}
	httpio.WriteJSON(res, http.StatusOK, toItemResponse(item, ownerID(req)))
}

// DeletePermanently handles DELETE /api/v1/trash/{id} — unlike Delete, this
// one is not reversible.
func (h *ItemHandler) DeletePermanently(res http.ResponseWriter, req *http.Request) {
	if err := h.items.PermanentlyDelete(req.Context(), ownerID(req), req.PathValue("id")); err != nil {
		httpio.WriteError(res, err)
		return
	}
	res.WriteHeader(http.StatusNoContent)
}
