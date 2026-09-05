package handler

import (
	"encoding/json"
	"net/http"

	"github.com/golfarelli/denizen/internal/apperr"
	"github.com/golfarelli/denizen/internal/httpio"
	"github.com/golfarelli/denizen/internal/model"
	"github.com/golfarelli/denizen/internal/service"
)

// UserShareHandler exposes the direct, per-user file-sharing routes — see
// model.UserShare's own doc comment for how this differs from
// ShareHandler's token-based links.
type UserShareHandler struct {
	shares *service.UserShareService
}

func NewUserShareHandler(shares *service.UserShareService) *UserShareHandler {
	return &UserShareHandler{shares: shares}
}

// grantedShareResponse is the owner's own view of a grant — who it's
// shared with, not with whom (there's only ever one owner: the caller).
type grantedShareResponse struct {
	ID                 string `json:"id"`
	ItemID             string `json:"item_id"`
	SharedWithUsername string `json:"shared_with_username"`
	Permission         string `json:"permission"`
	CreatedAt          int64  `json:"created_at"`
}

func toGrantedShareResponse(g service.GrantedShare) grantedShareResponse {
	return grantedShareResponse{
		ID: g.Grant.ID, ItemID: g.Grant.ItemID,
		SharedWithUsername: g.SharedWithUsername, Permission: string(g.Grant.Permission),
		CreatedAt: g.Grant.CreatedAt,
	}
}

// receivedShareResponse is the recipient's own view of a grant — who
// shared it with them, not who they shared it with.
type receivedShareResponse struct {
	ID            string `json:"id"`
	ItemID        string `json:"item_id"`
	OwnerUsername string `json:"owner_username"`
	Permission    string `json:"permission"`
	CreatedAt     int64  `json:"created_at"`
}

func toReceivedShareResponse(g service.ReceivedShare) receivedShareResponse {
	return receivedShareResponse{
		ID: g.Grant.ID, ItemID: g.Grant.ItemID,
		OwnerUsername: g.OwnerUsername, Permission: string(g.Grant.Permission),
		CreatedAt: g.Grant.CreatedAt,
	}
}

type createUserShareRequest struct {
	UserID     string `json:"user_id"`
	Permission string `json:"permission"`
}

// Create handles POST /api/v1/items/{id}/user-shares. Permission defaults
// to "view" when omitted, so older clients (or a bare {"user_id": "..."}
// call) keep getting today's behavior rather than a validation error.
func (h *UserShareHandler) Create(res http.ResponseWriter, req *http.Request) {
	var in createUserShareRequest
	if err := json.NewDecoder(req.Body).Decode(&in); err != nil {
		httpio.WriteError(res, apperr.Validation("invalid JSON body"))
		return
	}
	permission := model.SharePermission(in.Permission)
	if permission == "" {
		permission = model.SharePermissionView
	}
	grant, err := h.shares.Create(req.Context(), ownerID(req), req.PathValue("id"), in.UserID, permission)
	if err != nil {
		httpio.WriteError(res, err)
		return
	}
	httpio.WriteJSON(res, http.StatusCreated, toGrantedShareResponse(*grant))
}

type updateUserShareRequest struct {
	Permission string `json:"permission"`
}

// UpdatePermission handles PATCH /api/v1/user-shares/{id} — flips an
// existing grant between view and edit without revoking and re-sharing.
func (h *UserShareHandler) UpdatePermission(res http.ResponseWriter, req *http.Request) {
	var in updateUserShareRequest
	if err := json.NewDecoder(req.Body).Decode(&in); err != nil {
		httpio.WriteError(res, apperr.Validation("invalid JSON body"))
		return
	}
	grant, err := h.shares.UpdatePermission(req.Context(), ownerID(req), req.PathValue("id"), model.SharePermission(in.Permission))
	if err != nil {
		httpio.WriteError(res, err)
		return
	}
	httpio.WriteJSON(res, http.StatusOK, toGrantedShareResponse(*grant))
}

// ListForItem handles GET /api/v1/items/{id}/user-shares — the owner's own
// "who has access to this" listing.
func (h *UserShareHandler) ListForItem(res http.ResponseWriter, req *http.Request) {
	grants, err := h.shares.ListForItem(req.Context(), ownerID(req), req.PathValue("id"))
	if err != nil {
		httpio.WriteError(res, err)
		return
	}
	out := make([]grantedShareResponse, len(grants))
	for i, g := range grants {
		out[i] = toGrantedShareResponse(g)
	}
	httpio.WriteJSON(res, http.StatusOK, out)
}

// ListMine handles GET /api/v1/user-shares — every grant the caller has
// made, across all of their items ("My shares" page, alongside the
// token-link listing at GET /api/v1/shares).
func (h *UserShareHandler) ListMine(res http.ResponseWriter, req *http.Request) {
	grants, err := h.shares.ListMine(req.Context(), ownerID(req))
	if err != nil {
		httpio.WriteError(res, err)
		return
	}
	out := make([]grantedShareResponse, len(grants))
	for i, g := range grants {
		out[i] = toGrantedShareResponse(g)
	}
	httpio.WriteJSON(res, http.StatusOK, out)
}

// ListReceived handles GET /api/v1/shared-with-me.
func (h *UserShareHandler) ListReceived(res http.ResponseWriter, req *http.Request) {
	grants, err := h.shares.ListReceived(req.Context(), ownerID(req))
	if err != nil {
		httpio.WriteError(res, err)
		return
	}
	out := make([]receivedShareResponse, len(grants))
	for i, g := range grants {
		out[i] = toReceivedShareResponse(g)
	}
	httpio.WriteJSON(res, http.StatusOK, out)
}

// Revoke handles DELETE /api/v1/user-shares/{id} — only the owner who
// granted it may revoke it (see UserShareService.Revoke).
func (h *UserShareHandler) Revoke(res http.ResponseWriter, req *http.Request) {
	if err := h.shares.Revoke(req.Context(), ownerID(req), req.PathValue("id")); err != nil {
		httpio.WriteError(res, err)
		return
	}
	res.WriteHeader(http.StatusNoContent)
}
