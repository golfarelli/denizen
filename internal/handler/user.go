package handler

import (
	"encoding/json"
	"net/http"

	"github.com/golfarelli/denizen/internal/apperr"
	"github.com/golfarelli/denizen/internal/httpio"
	"github.com/golfarelli/denizen/internal/model"
	"github.com/golfarelli/denizen/internal/service"
)

// UserHandler exposes GET /api/v1/me, GET /api/v1/users/directory, and the
// admin-only /api/v1/users routes.
type UserHandler struct {
	users *service.UserService
}

func NewUserHandler(users *service.UserService) *UserHandler {
	return &UserHandler{users: users}
}

type userResponse struct {
	ID               string `json:"id"`
	Username         string `json:"username"`
	IsAdmin          bool   `json:"is_admin"`
	QuotaBytes       int64  `json:"quota_bytes"`
	StorageUsedBytes int64  `json:"storage_used_bytes"`
	Disabled         bool   `json:"disabled"`
	CreatedAt        int64  `json:"created_at"`
}

// toUserResponse deliberately excludes PasswordHash — model.User carries it,
// this DTO never does.
func toUserResponse(item *model.User) userResponse {
	return userResponse{
		ID:               item.ID,
		Username:         item.Username,
		IsAdmin:          item.IsAdmin,
		QuotaBytes:       item.QuotaBytes,
		StorageUsedBytes: item.StorageUsedBytes,
		Disabled:         item.Disabled,
		CreatedAt:        item.CreatedAt,
	}
}

// Me handles GET /api/v1/me.
func (h *UserHandler) Me(res http.ResponseWriter, req *http.Request) {
	user, err := h.users.Get(req.Context(), ownerID(req))
	if err != nil {
		httpio.WriteError(res, err)
		return
	}
	httpio.WriteJSON(res, http.StatusOK, toUserResponse(user))
}

// List handles GET /api/v1/users — admin only (enforced by
// middleware.RequireAdmin in internal/router, not re-checked here).
func (h *UserHandler) List(res http.ResponseWriter, req *http.Request) {
	items, err := h.users.ListAll(req.Context())
	if err != nil {
		httpio.WriteError(res, err)
		return
	}
	out := make([]userResponse, len(items))
	for i, item := range items {
		out[i] = toUserResponse(item)
	}
	httpio.WriteJSON(res, http.StatusOK, out)
}

type directoryUserResponse struct {
	ID       string `json:"id"`
	Username string `json:"username"`
}

// Directory handles GET /api/v1/users/directory — unlike List above, this
// is available to *any* authenticated user (not just an admin), which is
// why it returns just enough to pick a person to share a file with (id +
// username) rather than reusing userResponse's admin-facing fields
// (quota, storage used, disabled, is_admin). The caller and any disabled
// account are both excluded — sharing with yourself is meaningless
// (UserShareService.Create rejects it anyway) and a disabled account
// can't log in to see anything shared with it.
func (h *UserHandler) Directory(res http.ResponseWriter, req *http.Request) {
	items, err := h.users.ListAll(req.Context())
	if err != nil {
		httpio.WriteError(res, err)
		return
	}
	callerID := ownerID(req)
	out := make([]directoryUserResponse, 0, len(items))
	for _, item := range items {
		if item.ID == callerID || item.Disabled {
			continue
		}
		out = append(out, directoryUserResponse{ID: item.ID, Username: item.Username})
	}
	httpio.WriteJSON(res, http.StatusOK, out)
}

type updateUserRequest struct {
	QuotaBytes *int64 `json:"quota_bytes"`
	Disabled   *bool  `json:"disabled"`
}

// Update handles PATCH /api/v1/users/{id} — admin only. Unlike items'
// PATCH, this is a genuine partial update: an omitted field is left
// unchanged (see service.UpdateInput).
func (h *UserHandler) Update(res http.ResponseWriter, req *http.Request) {
	var in updateUserRequest
	if err := json.NewDecoder(req.Body).Decode(&in); err != nil {
		httpio.WriteError(res, apperr.Validation("invalid JSON body"))
		return
	}

	user, err := h.users.Update(req.Context(), req.PathValue("id"), service.UpdateInput{
		QuotaBytes: in.QuotaBytes,
		Disabled:   in.Disabled,
	})
	if err != nil {
		httpio.WriteError(res, err)
		return
	}
	httpio.WriteJSON(res, http.StatusOK, toUserResponse(user))
}
