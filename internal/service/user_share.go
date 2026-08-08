package service

import (
	"context"
	"time"

	"github.com/golfarelli/denizen/internal/apperr"
	"github.com/golfarelli/denizen/internal/idgen"
	"github.com/golfarelli/denizen/internal/model"
	"github.com/golfarelli/denizen/internal/repository"
)

// UserShareService covers creating, listing and revoking direct per-user
// grants — see model.UserShare's own doc comment for how this differs
// from ShareService's token-based links.
type UserShareService struct {
	grants *repository.UserShareRepository
	items  *ItemService
	users  *repository.UserRepository
	now    func() time.Time
}

func NewUserShareService(grants *repository.UserShareRepository, items *ItemService, users *repository.UserRepository) *UserShareService {
	return &UserShareService{grants: grants, items: items, users: users, now: time.Now}
}

// GrantedShare pairs a grant with the recipient's username — the owner's
// own "who has access to this item" view (ShareDialog's people section)
// needs a name to show, not just an opaque shared_with_id, and this saves
// the frontend a second round trip per row to get it.
type GrantedShare struct {
	Grant              *model.UserShare
	SharedWithUsername string
}

// ReceivedShare pairs a grant with the sharer's username — the recipient's
// own "Shared with me" list's counterpart to GrantedShare above.
type ReceivedShare struct {
	Grant         *model.UserShare
	OwnerUsername string
}

// ValidatePermission rejects anything but "view"/"edit" — shared here
// since both Create and UpdatePermission below need the exact same check.
func ValidatePermission(permission model.SharePermission) error {
	if permission != model.SharePermissionView && permission != model.SharePermissionEdit {
		return apperr.Validation("permission must be \"view\" or \"edit\"")
	}
	return nil
}

// Create grants sharedWithID access to itemID (file or folder, owned by
// ownerID) at the given permission level. A folder grant is inherited by
// everything nested inside it — see ItemService.resolveGrant — so sharing
// a folder is enough to give someone real browse (and, at "edit", real
// upload/organize) access to its whole subtree without sharing each item
// in it one by one.
func (s *UserShareService) Create(ctx context.Context, ownerID, itemID, sharedWithID string, permission model.SharePermission) (*GrantedShare, error) {
	if err := ValidatePermission(permission); err != nil {
		return nil, err
	}
	// Get enforces ownership + "not currently trashed" — same precondition
	// ShareService.Create uses for token-based links.
	_, err := s.items.Get(ctx, ownerID, itemID)
	if err != nil {
		return nil, err
	}
	if sharedWithID == ownerID {
		return nil, apperr.Validation("cannot share an item with yourself")
	}
	target, err := s.users.GetByID(ctx, sharedWithID)
	if err != nil {
		if err == repository.ErrNotFound {
			return nil, apperr.Validation("that person doesn't exist")
		}
		return nil, err
	}
	if target.Disabled {
		return nil, apperr.Validation("that account is disabled")
	}
	existing, err := s.grants.FindGrant(ctx, itemID, sharedWithID)
	if err != nil && err != repository.ErrNotFound {
		return nil, err
	}
	if existing != nil {
		return nil, apperr.Conflict("already shared with this person")
	}

	grant := &model.UserShare{
		ID:           idgen.New(),
		ItemID:       itemID,
		OwnerID:      ownerID,
		SharedWithID: sharedWithID,
		Permission:   permission,
		CreatedAt:    s.now().Unix(),
	}
	if err := s.grants.Create(ctx, grant); err != nil {
		return nil, err
	}
	return &GrantedShare{Grant: grant, SharedWithUsername: target.Username}, nil
}

// UpdatePermission changes an existing grant's view/edit level — only the
// owner who created it may change it (mirrors Revoke).
func (s *UserShareService) UpdatePermission(ctx context.Context, ownerID, id string, permission model.SharePermission) (*GrantedShare, error) {
	if err := ValidatePermission(permission); err != nil {
		return nil, err
	}
	grant, err := s.grants.GetByID(ctx, id)
	if err != nil {
		if err == repository.ErrNotFound {
			return nil, apperr.NotFound
		}
		return nil, err
	}
	if grant.OwnerID != ownerID {
		return nil, apperr.NotFound
	}
	if err := s.grants.UpdatePermission(ctx, id, permission); err != nil {
		return nil, err
	}
	grant.Permission = permission
	username := "?"
	if u, err := s.users.GetByID(ctx, grant.SharedWithID); err == nil {
		username = u.Username
	}
	return &GrantedShare{Grant: grant, SharedWithUsername: username}, nil
}

// ListForItem lists everyone itemID is directly shared with — ownership
// checked the same way every other per-item read is. A username lookup
// failing for one grant (shouldn't happen — there's no user-deletion
// feature, only disable) degrades that one row instead of failing the
// whole list.
func (s *UserShareService) ListForItem(ctx context.Context, ownerID, itemID string) ([]GrantedShare, error) {
	if _, err := s.items.Get(ctx, ownerID, itemID); err != nil {
		return nil, err
	}
	grants, err := s.grants.ListByItem(ctx, itemID)
	if err != nil {
		return nil, err
	}
	out := make([]GrantedShare, len(grants))
	for i, grant := range grants {
		username := "?"
		if u, err := s.users.GetByID(ctx, grant.SharedWithID); err == nil {
			username = u.Username
		}
		out[i] = GrantedShare{Grant: grant, SharedWithUsername: username}
	}
	return out, nil
}

// ListReceived lists every item directly shared with userID — the "Shared
// with me" page's own data source.
func (s *UserShareService) ListReceived(ctx context.Context, userID string) ([]ReceivedShare, error) {
	grants, err := s.grants.ListReceivedBy(ctx, userID)
	if err != nil {
		return nil, err
	}
	out := make([]ReceivedShare, len(grants))
	for i, grant := range grants {
		username := "?"
		if u, err := s.users.GetByID(ctx, grant.OwnerID); err == nil {
			username = u.Username
		}
		out[i] = ReceivedShare{Grant: grant, OwnerUsername: username}
	}
	return out, nil
}

// Revoke deletes a grant by its own id — only the owner who created it may
// revoke it (mirrors ShareService.Revoke; the recipient has no "leave"
// action of their own in this first pass).
func (s *UserShareService) Revoke(ctx context.Context, ownerID, id string) error {
	grant, err := s.grants.GetByID(ctx, id)
	if err != nil {
		if err == repository.ErrNotFound {
			return apperr.NotFound
		}
		return err
	}
	if grant.OwnerID != ownerID {
		return apperr.NotFound
	}
	return s.grants.Delete(ctx, id)
}
