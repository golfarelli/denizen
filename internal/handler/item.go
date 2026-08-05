package handler

import (
	"encoding/json"
	"net/http"

	"github.com/golfarelli/denizen/internal/apperr"
	"github.com/golfarelli/denizen/internal/httpio"
	"github.com/golfarelli/denizen/internal/middleware"
	"github.com/golfarelli/denizen/internal/model"
	"github.com/golfarelli/denizen/internal/service"
)

// ItemHandler exposes /api/v1/items* and /api/v1/trash*. Every route here
// is expected to run behind middleware.RequireAuth.
type ItemHandler struct {
	items *service.ItemService
}

func NewItemHandler(items *service.ItemService) *ItemHandler {
	return &ItemHandler{items: items}
}

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
}

func toItemResponse(item *model.Item) itemResponse {
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
	}
}

func toItemResponses(items []*model.Item) []itemResponse {
	out := make([]itemResponse, len(items))
	for i, item := range items {
		out[i] = toItemResponse(item)
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
	httpio.WriteJSON(res, http.StatusCreated, toItemResponse(item))
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
	httpio.WriteJSON(res, http.StatusOK, toItemResponses(items))
}

func (h *ItemHandler) Get(res http.ResponseWriter, req *http.Request) {
	item, err := h.items.Get(req.Context(), ownerID(req), req.PathValue("id"))
	if err != nil {
		httpio.WriteError(res, err)
		return
	}
	httpio.WriteJSON(res, http.StatusOK, toItemResponse(item))
}

// Content handles GET /api/v1/items/{id}/content — streams a file's bytes,
// with Range support (via http.ServeContent, so a paused download or a
// video/PDF preview seeking around doesn't need custom byte-range logic
// here).
func (h *ItemHandler) Content(res http.ResponseWriter, req *http.Request) {
	item, err := h.items.Get(req.Context(), ownerID(req), req.PathValue("id"))
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
	httpio.WriteJSON(res, http.StatusOK, toItemResponse(item))
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

func (h *ItemHandler) ListTrash(res http.ResponseWriter, req *http.Request) {
	items, err := h.items.ListTrash(req.Context(), ownerID(req))
	if err != nil {
		httpio.WriteError(res, err)
		return
	}
	httpio.WriteJSON(res, http.StatusOK, toItemResponses(items))
}

func (h *ItemHandler) Restore(res http.ResponseWriter, req *http.Request) {
	item, err := h.items.Restore(req.Context(), ownerID(req), req.PathValue("id"))
	if err != nil {
		httpio.WriteError(res, err)
		return
	}
	httpio.WriteJSON(res, http.StatusOK, toItemResponse(item))
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
