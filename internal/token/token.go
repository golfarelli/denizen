// Package token issues and verifies Denizen's credentials: two self-
// contained, short-lived JWTs (an access token, and a narrower content
// token scoped to one item's content — see ContentClaims), and opaque
// bearer tokens (refresh tokens, share links) looked up by presenting the
// token itself.
//
// The JWTs and the opaque tokens are deliberately different shapes: both
// JWTs are short-lived enough (minutes, not weeks) that there's little
// value in tracking them for revocation — any handler can verify one
// without a DB round trip. Opaque tokens are long-lived and *must* be
// revocable (logout, rotation, a stolen device, revoking a share link), so
// each is a random value whose hash — never the raw value — is what's
// actually stored (in `refresh_tokens`/`shares`); a database leak alone
// doesn't let anyone replay a session or a "should have been revoked"
// share.
package token

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// ErrInvalidToken covers every way an access token can fail to verify: bad
// signature, wrong algorithm, expired, malformed. Deliberately not more
// specific than that — callers shouldn't behave differently based on why a
// token was rejected.
var ErrInvalidToken = errors.New("token: invalid or expired")

// Claims is the payload of an access token.
type Claims struct {
	UserID  string `json:"sub"`
	IsAdmin bool   `json:"is_admin"`
	jwt.RegisteredClaims
}

// Issuer issues and verifies access tokens signed with a single shared
// secret (HMAC-SHA256).
type Issuer struct {
	secret []byte
}

func NewIssuer(secret string) *Issuer {
	return &Issuer{secret: []byte(secret)}
}

// NewAccessToken issues a signed, short-lived JWT identifying userID.
func (i *Issuer) NewAccessToken(userID string, isAdmin bool, ttl time.Duration) (string, error) {
	now := time.Now()
	claims := Claims{
		UserID:  userID,
		IsAdmin: isAdmin,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
			IssuedAt:  jwt.NewNumericDate(now),
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(i.secret)
}

// ParseAccessToken verifies the signature and expiry of an access token and
// returns its claims.
func (i *Issuer) ParseAccessToken(raw string) (*Claims, error) {
	claims := &Claims{}
	_, err := jwt.ParseWithClaims(raw, claims, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, ErrInvalidToken
		}
		return i.secret, nil
	})
	if err != nil {
		return nil, ErrInvalidToken
	}
	return claims, nil
}

// ContentClaims is the payload of a content token — narrower than an
// access token on purpose: good only for GETting one specific item's
// content, not for calling any other endpoint, since RequireAuthOrContentToken
// (internal/middleware) checks ItemID against the URL it's presented on.
type ContentClaims struct {
	UserID string `json:"sub"`
	ItemID string `json:"item_id"`
	jwt.RegisteredClaims
}

// NewContentToken issues a signed, short-lived token good only for GETting
// itemID's content — for <video>/<audio> elements, which (unlike this
// app's own fetch() calls) can't attach a custom Authorization header, so
// this rides along in the URL's query string instead. Self-contained JWT
// like the access token, not an opaque+stored one like a refresh token or
// share: it's short-lived enough that revocability isn't worth a DB round
// trip on every content request.
func (i *Issuer) NewContentToken(userID, itemID string, ttl time.Duration) (string, error) {
	now := time.Now()
	claims := ContentClaims{
		UserID: userID,
		ItemID: itemID,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
			IssuedAt:  jwt.NewNumericDate(now),
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(i.secret)
}

// ParseContentToken verifies the signature and expiry of a content token
// and returns its claims.
func (i *Issuer) ParseContentToken(raw string) (*ContentClaims, error) {
	claims := &ContentClaims{}
	_, err := jwt.ParseWithClaims(raw, claims, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, ErrInvalidToken
		}
		return i.secret, nil
	})
	if err != nil {
		return nil, ErrInvalidToken
	}
	return claims, nil
}

// NewOpaque returns a random opaque token (to hand to the client) and its
// SHA-256 hash (to store in the database) — used for both refresh tokens
// and share tokens, which share the same "long-lived, revocable, looked up
// by hash" shape.
func NewOpaque() (raw string, hash string, err error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", "", err
	}
	raw = base64.RawURLEncoding.EncodeToString(b[:])
	return raw, HashOpaque(raw), nil
}

// HashOpaque hashes a raw opaque token the same way NewOpaque does, so a
// presented token can be looked up by its hash.
func HashOpaque(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}
