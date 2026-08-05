package handler

import (
	"encoding/json"
	"net/http"

	"github.com/golfarelli/denizen/internal/apperr"
	"github.com/golfarelli/denizen/internal/httpio"
	"github.com/golfarelli/denizen/internal/middleware"
	"github.com/golfarelli/denizen/internal/model"
	"github.com/golfarelli/denizen/internal/service"
	"github.com/golfarelli/denizen/internal/token"
)

// ShareHandler exposes the authenticated /api/v1/items/{id}/shares and
// /api/v1/shares* routes, plus the public (no auth required) /s/{token}
// routes a visitor actually opens.
type ShareHandler struct {
	shares *service.ShareService
	tokens *token.Issuer // only for IsAuthenticated on the public routes
}

func NewShareHandler(shares *service.ShareService, tokens *token.Issuer) *ShareHandler {
	return &ShareHandler{shares: shares, tokens: tokens}
}

type createShareRequest struct {
	RequiresAuth bool   `json:"requires_auth"`
	ExpiresAt    *int64 `json:"expires_at"`
}

type shareResponse struct {
	ID           string  `json:"id"`
	ItemID       string  `json:"item_id"`
	RequiresAuth bool    `json:"requires_auth"`
	ExpiresAt    *int64  `json:"expires_at,omitempty"`
	CreatedAt    int64   `json:"created_at"`
	Token        *string `json:"token,omitempty"` // only ever present in the Create response — see ShareService.Create
	URL          *string `json:"url,omitempty"`
}

func toShareResponse(item *model.Share) shareResponse {
	return shareResponse{
		ID:           item.ID,
		ItemID:       item.ItemID,
		RequiresAuth: item.RequiresAuth,
		ExpiresAt:    item.ExpiresAt,
		CreatedAt:    item.CreatedAt,
	}
}

// Create handles POST /api/v1/items/{id}/shares.
func (h *ShareHandler) Create(res http.ResponseWriter, req *http.Request) {
	var in createShareRequest
	if err := json.NewDecoder(req.Body).Decode(&in); err != nil {
		httpio.WriteError(res, apperr.Validation("invalid JSON body"))
		return
	}

	item, raw, err := h.shares.Create(req.Context(), ownerID(req), service.CreateInput{
		ItemID:       req.PathValue("id"),
		RequiresAuth: in.RequiresAuth,
		ExpiresAt:    in.ExpiresAt,
	})
	if err != nil {
		httpio.WriteError(res, err)
		return
	}

	out := toShareResponse(item)
	out.Token = &raw
	url := "/s/" + raw
	out.URL = &url
	httpio.WriteJSON(res, http.StatusCreated, out)
}

// ListMine handles GET /api/v1/shares.
func (h *ShareHandler) ListMine(res http.ResponseWriter, req *http.Request) {
	items, err := h.shares.ListMine(req.Context(), ownerID(req))
	if err != nil {
		httpio.WriteError(res, err)
		return
	}
	out := make([]shareResponse, len(items))
	for i, item := range items {
		out[i] = toShareResponse(item)
	}
	httpio.WriteJSON(res, http.StatusOK, out)
}

// Revoke handles DELETE /api/v1/shares/{id} — by the share's own id, not
// its token, which this server never retains past the Create response (see
// ShareService.Create).
func (h *ShareHandler) Revoke(res http.ResponseWriter, req *http.Request) {
	if err := h.shares.Revoke(req.Context(), ownerID(req), req.PathValue("id")); err != nil {
		httpio.WriteError(res, err)
		return
	}
	res.WriteHeader(http.StatusNoContent)
}

type publicItemResponse struct {
	Name      string `json:"name"`
	Type      string `json:"type"`
	SizeBytes int64  `json:"size_bytes"`
}

// PublicMetadata handles GET /s/{token} — deliberately returns far less
// than the authenticated item endpoints (just enough for a share landing
// page to render something), since the visitor isn't necessarily anyone
// with an account.
func (h *ShareHandler) PublicMetadata(res http.ResponseWriter, req *http.Request) {
	item, err := h.shares.Resolve(req.Context(), req.PathValue("token"), middleware.IsAuthenticated(h.tokens, req))
	if err != nil {
		httpio.WriteError(res, err)
		return
	}
	httpio.WriteJSON(res, http.StatusOK, publicItemResponse{
		Name: item.Name, Type: string(item.Type), SizeBytes: item.SizeBytes,
	})
}

// PublicContent handles GET /s/{token}/content. Folders aren't downloadable
// this way (no zip-on-the-fly in this pass) — only a shared file's actual
// bytes are.
func (h *ShareHandler) PublicContent(res http.ResponseWriter, req *http.Request) {
	item, err := h.shares.Resolve(req.Context(), req.PathValue("token"), middleware.IsAuthenticated(h.tokens, req))
	if err != nil {
		httpio.WriteError(res, err)
		return
	}
	if item.Type != model.ItemTypeFile {
		httpio.WriteError(res, apperr.Validation("this share points to a folder, not a file"))
		return
	}
	path, err := h.shares.FilePath(req.Context(), item)
	if err != nil {
		httpio.WriteError(res, err)
		return
	}
	serveFileContent(res, req, item, path)
}
