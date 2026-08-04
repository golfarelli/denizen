package handler

import (
	"encoding/json"
	"net/http"

	"github.com/golfarelli/denizen/internal/apperr"
	"github.com/golfarelli/denizen/internal/service"
)

// AuthHandler exposes /api/v1/auth/*.
type AuthHandler struct {
	auth *service.AuthService
}

func NewAuthHandler(auth *service.AuthService) *AuthHandler {
	return &AuthHandler{auth: auth}
}

type registerRequest struct {
	InviteCode string `json:"invite_code"`
	Username   string `json:"username"`
	Password   string `json:"password"`
}

type userResponse struct {
	ID       string `json:"id"`
	Username string `json:"username"`
	IsAdmin  bool   `json:"is_admin"`
}

func (h *AuthHandler) Register(res http.ResponseWriter, req *http.Request) {
	var in registerRequest
	if err := json.NewDecoder(req.Body).Decode(&in); err != nil {
		writeError(res, apperr.Validation("invalid JSON body"))
		return
	}

	item, err := h.auth.Register(req.Context(), service.RegisterInput{
		InviteCode: in.InviteCode,
		Username:   in.Username,
		Password:   in.Password,
	})
	if err != nil {
		writeError(res, err)
		return
	}

	writeJSON(res, http.StatusCreated, userResponse{ID: item.ID, Username: item.Username, IsAdmin: item.IsAdmin})
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
		writeError(res, apperr.Validation("invalid JSON body"))
		return
	}

	pair, err := h.auth.Login(req.Context(), in.Username, in.Password)
	if err != nil {
		writeError(res, err)
		return
	}

	writeJSON(res, http.StatusOK, tokenResponse{AccessToken: pair.AccessToken, RefreshToken: pair.RefreshToken})
}

type refreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

func (h *AuthHandler) Refresh(res http.ResponseWriter, req *http.Request) {
	var in refreshRequest
	if err := json.NewDecoder(req.Body).Decode(&in); err != nil {
		writeError(res, apperr.Validation("invalid JSON body"))
		return
	}

	pair, err := h.auth.Refresh(req.Context(), in.RefreshToken)
	if err != nil {
		writeError(res, err)
		return
	}

	writeJSON(res, http.StatusOK, tokenResponse{AccessToken: pair.AccessToken, RefreshToken: pair.RefreshToken})
}

func (h *AuthHandler) Logout(res http.ResponseWriter, req *http.Request) {
	var in refreshRequest
	if err := json.NewDecoder(req.Body).Decode(&in); err != nil {
		writeError(res, apperr.Validation("invalid JSON body"))
		return
	}

	if err := h.auth.Logout(req.Context(), in.RefreshToken); err != nil {
		writeError(res, err)
		return
	}

	res.WriteHeader(http.StatusNoContent)
}
