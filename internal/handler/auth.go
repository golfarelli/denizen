package handler

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/golfarelli/denizen/internal/apperr"
	"github.com/golfarelli/denizen/internal/httpio"
	"github.com/golfarelli/denizen/internal/service"
)

// AuthHandler exposes /api/v1/auth/* and /api/v1/invites.
type AuthHandler struct {
	auth      *service.AuthService
	inviteTTL time.Duration
}

func NewAuthHandler(auth *service.AuthService, inviteTTL time.Duration) *AuthHandler {
	return &AuthHandler{auth: auth, inviteTTL: inviteTTL}
}

type registerRequest struct {
	InviteCode string `json:"invite_code"`
	Username   string `json:"username"`
	Password   string `json:"password"`
}

// registeredUserResponse is deliberately smaller than the full userResponse
// (see handler/user.go) — a fresh registration doesn't need to echo back
// quota/storage/disabled details the caller already knows.
type registeredUserResponse struct {
	ID       string `json:"id"`
	Username string `json:"username"`
	IsAdmin  bool   `json:"is_admin"`
}

func (h *AuthHandler) Register(res http.ResponseWriter, req *http.Request) {
	var in registerRequest
	if err := json.NewDecoder(req.Body).Decode(&in); err != nil {
		httpio.WriteError(res, apperr.Validation("invalid JSON body"))
		return
	}

	item, err := h.auth.Register(req.Context(), service.RegisterInput{
		InviteCode: in.InviteCode,
		Username:   in.Username,
		Password:   in.Password,
	})
	if err != nil {
		httpio.WriteError(res, err)
		return
	}

	httpio.WriteJSON(res, http.StatusCreated, registeredUserResponse{ID: item.ID, Username: item.Username, IsAdmin: item.IsAdmin})
}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
}

func (h *AuthHandler) Login(res http.ResponseWriter, req *http.Request) {
	var in loginRequest
	if err := json.NewDecoder(req.Body).Decode(&in); err != nil {
		httpio.WriteError(res, apperr.Validation("invalid JSON body"))
		return
	}

	pair, err := h.auth.Login(req.Context(), in.Username, in.Password)
	if err != nil {
		httpio.WriteError(res, err)
		return
	}

	httpio.WriteJSON(res, http.StatusOK, tokenResponse{AccessToken: pair.AccessToken, RefreshToken: pair.RefreshToken})
}

type refreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

func (h *AuthHandler) Refresh(res http.ResponseWriter, req *http.Request) {
	var in refreshRequest
	if err := json.NewDecoder(req.Body).Decode(&in); err != nil {
		httpio.WriteError(res, apperr.Validation("invalid JSON body"))
		return
	}

	pair, err := h.auth.Refresh(req.Context(), in.RefreshToken)
	if err != nil {
		httpio.WriteError(res, err)
		return
	}

	httpio.WriteJSON(res, http.StatusOK, tokenResponse{AccessToken: pair.AccessToken, RefreshToken: pair.RefreshToken})
}

func (h *AuthHandler) Logout(res http.ResponseWriter, req *http.Request) {
	var in refreshRequest
	if err := json.NewDecoder(req.Body).Decode(&in); err != nil {
		httpio.WriteError(res, apperr.Validation("invalid JSON body"))
		return
	}

	if err := h.auth.Logout(req.Context(), in.RefreshToken); err != nil {
		httpio.WriteError(res, err)
		return
	}

	res.WriteHeader(http.StatusNoContent)
}

type createInviteRequest struct {
	QuotaBytes *int64 `json:"quota_bytes"` // nil = the server's default quota
}

type inviteResponse struct {
	Code      string `json:"code"`
	ExpiresAt int64  `json:"expires_at"`
}

// CreateInvite handles POST /api/v1/invites — admin only (enforced by
// middleware.RequireAdmin in internal/router). Note this issues a regular
// invite, never one that grants admin — only the one-off bootstrap invite
// created at first startup does that (see AuthService.EnsureBootstrapInvite).
func (h *AuthHandler) CreateInvite(res http.ResponseWriter, req *http.Request) {
	var in createInviteRequest
	if err := json.NewDecoder(req.Body).Decode(&in); err != nil {
		httpio.WriteError(res, apperr.Validation("invalid JSON body"))
		return
	}

	code, expiresAt, err := h.auth.CreateInvite(req.Context(), ownerID(req), in.QuotaBytes, h.inviteTTL)
	if err != nil {
		httpio.WriteError(res, err)
		return
	}

	httpio.WriteJSON(res, http.StatusCreated, inviteResponse{Code: code, ExpiresAt: expiresAt})
}
