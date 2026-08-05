package service

import (
	"context"
	"time"

	"github.com/golfarelli/denizen/internal/apperr"
	"github.com/golfarelli/denizen/internal/idgen"
	"github.com/golfarelli/denizen/internal/model"
	"github.com/golfarelli/denizen/internal/repository"
	"github.com/golfarelli/denizen/internal/token"
)

// ShareService covers creating, listing and revoking share links, plus
// resolving one for the public (unauthenticated, unless RequiresAuth)
// visitor-facing endpoints.
type ShareService struct {
	shares *repository.ShareRepository
	items  *ItemService
	now    func() time.Time
}

func NewShareService(shares *repository.ShareRepository, items *ItemService) *ShareService {
	return &ShareService{shares: shares, items: items, now: time.Now}
}

// CreateInput is what the owner supplies when sharing an item.
type CreateInput struct {
	ItemID       string
	RequiresAuth bool
	ExpiresAt    *int64 // nil = never expires
}

// Create shares itemID, owned by ownerID. The returned raw token is shown
// to the caller exactly once — only its hash is persisted (see
// internal/db/schema.sql) — so it must be handed back in the response now;
// there is no way to retrieve it again later.
func (s *ShareService) Create(ctx context.Context, ownerID string, in CreateInput) (share *model.Share, rawToken string, err error) {
	// Get enforces ownership + "not currently trashed" — sharing a deleted
	// item makes no sense, and this also means existence of someone else's
	// item never leaks through this endpoint either.
	if _, err := s.items.Get(ctx, ownerID, in.ItemID); err != nil {
		return nil, "", err
	}

	raw, hash, err := token.NewOpaque()
	if err != nil {
		return nil, "", err
	}

	item := &model.Share{
		ID:           idgen.New(),
		ItemID:       in.ItemID,
		TokenHash:    hash,
		CreatedBy:    ownerID,
		RequiresAuth: in.RequiresAuth,
		ExpiresAt:    in.ExpiresAt,
		CreatedAt:    s.now().Unix(),
	}
	if err := s.shares.Create(ctx, item); err != nil {
		return nil, "", err
	}
	return item, raw, nil
}

// ListMine lists ownerID's own share links. The raw token isn't among what
// comes back — only what was shown once at creation could ever reveal it,
// by design (see Create).
func (s *ShareService) ListMine(ctx context.Context, ownerID string) ([]*model.Share, error) {
	return s.shares.ListByOwner(ctx, ownerID)
}

// Revoke deletes a share by its own id (not its token, which this server
// never retains after creation — see Create). Ownership-checked the same
// way as everything else: a mismatch reads as apperr.NotFound, not Forbidden.
func (s *ShareService) Revoke(ctx context.Context, ownerID, id string) error {
	item, err := s.shares.GetByID(ctx, id)
	if err != nil {
		if err == repository.ErrNotFound {
			return apperr.NotFound
		}
		return err
	}
	if item.CreatedBy != ownerID {
		return apperr.NotFound
	}
	return s.shares.Delete(ctx, id)
}

// Resolve looks up the item a raw share token grants access to, for the
// public /s/{token} endpoints. authenticated reflects only whether the
// visitor presented *some* valid access token of their own — not that they
// own anything — which is all RequiresAuth ever asks for (see
// docs/ARCHITECTURE.md: link-based sharing, not per-user ACLs).
func (s *ShareService) Resolve(ctx context.Context, rawToken string, authenticated bool) (*model.Item, error) {
	share, err := s.shares.GetByTokenHash(ctx, token.HashOpaque(rawToken))
	if err != nil {
		if err == repository.ErrNotFound {
			return nil, apperr.NotFound
		}
		return nil, err
	}
	if share.ExpiresAt != nil && *share.ExpiresAt < s.now().Unix() {
		// An expired share looks exactly like "never existed" to a visitor —
		// no reason to distinguish the two from the outside.
		return nil, apperr.NotFound
	}
	if share.RequiresAuth && !authenticated {
		return nil, apperr.Unauthorized
	}
	return s.items.GetForShare(ctx, share.ItemID)
}

// FilePath resolves the on-disk path of an item already returned by
// Resolve — a thin passthrough to ItemService.FilePath, kept here so
// ShareHandler only needs to depend on ShareService, not both services.
func (s *ShareService) FilePath(ctx context.Context, item *model.Item) (string, error) {
	return s.items.FilePath(ctx, item)
}
