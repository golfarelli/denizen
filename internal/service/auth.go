// Package service holds Denizen's business logic. No HTTP concerns here
// (see internal/handler) and no raw SQL (see internal/repository).
package service

import (
	"context"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/golfarelli/denizen/internal/apperr"
	"github.com/golfarelli/denizen/internal/idgen"
	"github.com/golfarelli/denizen/internal/model"
	"github.com/golfarelli/denizen/internal/repository"
	"github.com/golfarelli/denizen/internal/token"
)

// AuthService covers account creation (via invite) and session management
// (login/refresh/logout).
type AuthService struct {
	users          *repository.UserRepository
	invites        *repository.InviteRepository
	refreshTokens  *repository.RefreshTokenRepository
	tokens         *token.Issuer
	defaultQuota   int64
	accessTokenTTL time.Duration
	refreshTTL     time.Duration
	now            func() time.Time // swappable in tests; defaults to time.Now
}

func NewAuthService(
	users *repository.UserRepository,
	invites *repository.InviteRepository,
	refreshTokens *repository.RefreshTokenRepository,
	tokens *token.Issuer,
	defaultQuota int64,
	accessTokenTTL, refreshTTL time.Duration,
) *AuthService {
	return &AuthService{
		users:          users,
		invites:        invites,
		refreshTokens:  refreshTokens,
		tokens:         tokens,
		defaultQuota:   defaultQuota,
		accessTokenTTL: accessTokenTTL,
		refreshTTL:     refreshTTL,
		now:            time.Now,
	}
}

// EnsureBootstrapInvite creates the single invite that grants admin, but
// only if the instance has no users yet. Meant to be called once at
// startup; the returned code is what the operator logs and uses to create
// the first (admin) account. Safe to call on every boot — it's a no-op once
// a user exists.
func (s *AuthService) EnsureBootstrapInvite(ctx context.Context, ttl time.Duration) (code string, created bool, err error) {
	count, err := s.users.CountAll(ctx)
	if err != nil {
		return "", false, err
	}
	if count > 0 {
		return "", false, nil
	}
	code = idgen.New()
	now := s.now().Unix()
	err = s.invites.Create(ctx, &model.Invite{
		ID:          idgen.New(),
		Code:        code,
		GrantsAdmin: true,
		ExpiresAt:   s.now().Add(ttl).Unix(),
		CreatedAt:   now,
	})
	if err != nil {
		return "", false, err
	}
	return code, true, nil
}

// CreateInvite issues a new invite. Authorization (createdBy must be an
// admin) is enforced by middleware.RequireAdmin at the route level, not
// re-checked here — see internal/router.
func (s *AuthService) CreateInvite(ctx context.Context, createdBy string, quotaBytes *int64, ttl time.Duration) (code string, expiresAt int64, err error) {
	now := s.now()
	code = idgen.New()
	expiresAt = now.Add(ttl).Unix()
	err = s.invites.Create(ctx, &model.Invite{
		ID:         idgen.New(),
		Code:       code,
		CreatedBy:  createdBy,
		QuotaBytes: quotaBytes,
		ExpiresAt:  expiresAt,
		CreatedAt:  now.Unix(),
	})
	if err != nil {
		return "", 0, err
	}
	return code, expiresAt, nil
}

// RegisterInput is the validated-on-entry input to Register.
type RegisterInput struct {
	InviteCode string
	Username   string
	Password   string
}

// Register redeems an invite code to create a new account.
func (s *AuthService) Register(ctx context.Context, in RegisterInput) (*model.User, error) {
	if len(in.Username) < 3 {
		return nil, apperr.Validation("username must be at least 3 characters")
	}
	if len(in.Password) < 8 {
		return nil, apperr.Validation("password must be at least 8 characters")
	}

	invite, err := s.invites.GetByCode(ctx, in.InviteCode)
	if err != nil {
		if err == repository.ErrNotFound {
			return nil, apperr.Validation("invalid invite code")
		}
		return nil, err
	}
	now := s.now()
	if invite.UsedAt != nil {
		return nil, apperr.Validation("invite code already used")
	}
	if invite.ExpiresAt < now.Unix() {
		return nil, apperr.Validation("invite code expired")
	}

	existing, err := s.users.GetByUsername(ctx, in.Username)
	if err != nil && err != repository.ErrNotFound {
		return nil, err
	}
	if existing != nil {
		return nil, apperr.Conflict("username already taken")
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(in.Password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}

	quota := s.defaultQuota
	if invite.QuotaBytes != nil {
		quota = *invite.QuotaBytes
	}

	item := &model.User{
		ID:           idgen.New(),
		Username:     in.Username,
		PasswordHash: string(hash),
		IsAdmin:      invite.GrantsAdmin,
		QuotaBytes:   quota,
		CreatedAt:    now.Unix(),
	}
	if err := s.users.Create(ctx, item); err != nil {
		return nil, err
	}

	ok, err := s.invites.MarkUsed(ctx, invite.Code, item.ID, now.Unix())
	if err != nil {
		return nil, err
	}
	if !ok {
		// Someone else redeemed the same code in the race window between
		// the check above and this update — extremely unlikely for a
		// random invite code, but not impossible, so we don't leave a
		// dangling account behind with no valid invite to show for it.
		return nil, apperr.Conflict("invite code already used")
	}

	return item, nil
}

// TokenPair is what a successful login/refresh hands back to the client.
type TokenPair struct {
	AccessToken  string
	RefreshToken string
}

// Login verifies credentials and issues a fresh token pair.
func (s *AuthService) Login(ctx context.Context, username, password string) (*TokenPair, error) {
	item, err := s.users.GetByUsername(ctx, username)
	if err != nil {
		if err == repository.ErrNotFound {
			return nil, apperr.Unauthorized
		}
		return nil, err
	}
	if item.Disabled {
		return nil, apperr.Forbidden
	}
	if err := bcrypt.CompareHashAndPassword([]byte(item.PasswordHash), []byte(password)); err != nil {
		return nil, apperr.Unauthorized
	}
	return s.issueTokenPair(ctx, item)
}

func (s *AuthService) issueTokenPair(ctx context.Context, user *model.User) (*TokenPair, error) {
	access, err := s.tokens.NewAccessToken(user.ID, user.IsAdmin, s.accessTokenTTL)
	if err != nil {
		return nil, err
	}
	raw, hash, err := token.NewOpaque()
	if err != nil {
		return nil, err
	}
	now := s.now()
	err = s.refreshTokens.Create(ctx, &model.RefreshToken{
		ID:        idgen.New(),
		UserID:    user.ID,
		TokenHash: hash,
		ExpiresAt: now.Add(s.refreshTTL).Unix(),
		CreatedAt: now.Unix(),
	})
	if err != nil {
		return nil, err
	}
	return &TokenPair{AccessToken: access, RefreshToken: raw}, nil
}

// Refresh rotates a refresh token: the presented one is revoked and a brand
// new pair is issued, so a stolen-and-replayed refresh token stops working
// the moment the legitimate client uses it again.
func (s *AuthService) Refresh(ctx context.Context, rawRefreshToken string) (*TokenPair, error) {
	hash := token.HashOpaque(rawRefreshToken)
	item, err := s.refreshTokens.GetByHash(ctx, hash)
	if err != nil {
		if err == repository.ErrNotFound {
			return nil, apperr.Unauthorized
		}
		return nil, err
	}
	now := s.now()
	if item.RevokedAt != nil || item.ExpiresAt < now.Unix() {
		return nil, apperr.Unauthorized
	}
	if err := s.refreshTokens.Revoke(ctx, item.ID, now.Unix()); err != nil {
		return nil, err
	}
	user, err := s.users.GetByID(ctx, item.UserID)
	if err != nil {
		return nil, err
	}
	return s.issueTokenPair(ctx, user)
}

// Logout revokes a single refresh token. The matching access token is
// short-lived enough (see config.AccessTokenTTL) that it's left to expire
// naturally rather than tracked for revocation too.
func (s *AuthService) Logout(ctx context.Context, rawRefreshToken string) error {
	hash := token.HashOpaque(rawRefreshToken)
	item, err := s.refreshTokens.GetByHash(ctx, hash)
	if err != nil {
		if err == repository.ErrNotFound {
			return nil // already gone; logout is idempotent
		}
		return err
	}
	if item.RevokedAt != nil {
		return nil
	}
	return s.refreshTokens.Revoke(ctx, item.ID, s.now().Unix())
}
