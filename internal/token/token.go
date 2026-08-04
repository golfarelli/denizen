// Package token issues and verifies the two credentials Denizen hands out on
// login: a short-lived JWT access token, and an opaque, revocable refresh
// token.
//
// The two are deliberately different shapes: the access token is
// self-contained (any handler can verify it without a DB round trip) and
// short-lived, so there's little value in tracking it for revocation. The
// refresh token is long-lived and *must* be revocable (logout, rotation,
// a stolen device), so it's an opaque random value whose hash — never the
// raw value — is stored in the `refresh_tokens` table.
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

// NewRefreshToken returns a random opaque token (to hand to the client) and
// its SHA-256 hash (to store in the database) — the raw value is never
// persisted, so a database leak alone doesn't let anyone replay a session.
func NewRefreshToken() (raw string, hash string, err error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", "", err
	}
	raw = base64.RawURLEncoding.EncodeToString(b[:])
	return raw, HashRefreshToken(raw), nil
}

// HashRefreshToken hashes a raw refresh token the same way NewRefreshToken
// does, so a presented token can be looked up by its hash.
func HashRefreshToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}
