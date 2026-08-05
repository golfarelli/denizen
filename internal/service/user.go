package service

import (
	"context"

	"github.com/golfarelli/denizen/internal/apperr"
	"github.com/golfarelli/denizen/internal/model"
	"github.com/golfarelli/denizen/internal/repository"
)

// UserService covers reading and (admin-only) editing user accounts.
// Authorization for the admin-only operations is enforced by
// middleware.RequireAdmin at the route level (see internal/router) — there
// is no separate ownership dimension here the way there is for items
// (an admin may edit any user), so this layer doesn't re-check it.
type UserService struct {
	users *repository.UserRepository
}

func NewUserService(users *repository.UserRepository) *UserService {
	return &UserService{users: users}
}

// Get returns a single user by id — used both for GET /me (id = the
// caller's own id) and to look up the target of an admin PATCH.
func (s *UserService) Get(ctx context.Context, id string) (*model.User, error) {
	user, err := s.users.GetByID(ctx, id)
	if err != nil {
		if err == repository.ErrNotFound {
			return nil, apperr.NotFound
		}
		return nil, err
	}
	return user, nil
}

// ListAll lists every user — admin only.
func (s *UserService) ListAll(ctx context.Context) ([]*model.User, error) {
	return s.users.ListAll(ctx)
}

// UpdateInput is a partial update: nil fields are left unchanged. Unlike
// items' PATCH (a full replacement of name+location — see
// ItemService.Move), a user only has two independently toggleable admin
// settings, so "don't touch what wasn't sent" is the more sensible default
// here and there's no ambiguity it could hide.
type UpdateInput struct {
	QuotaBytes *int64
	Disabled   *bool
}

// Update applies a partial admin edit to a user.
func (s *UserService) Update(ctx context.Context, id string, in UpdateInput) (*model.User, error) {
	user, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}

	quotaBytes := user.QuotaBytes
	if in.QuotaBytes != nil {
		if *in.QuotaBytes < 0 {
			return nil, apperr.Validation("quota_bytes cannot be negative")
		}
		quotaBytes = *in.QuotaBytes
	}
	disabled := user.Disabled
	if in.Disabled != nil {
		disabled = *in.Disabled
	}

	if err := s.users.UpdateQuotaAndDisabled(ctx, id, quotaBytes, disabled); err != nil {
		return nil, err
	}
	user.QuotaBytes = quotaBytes
	user.Disabled = disabled
	return user, nil
}
