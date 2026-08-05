package token

import (
	"testing"
	"time"
)

func TestAccessToken_RoundTrip(t *testing.T) {
	issuer := NewIssuer("test-secret")

	raw, err := issuer.NewAccessToken("user-1", true, time.Minute)
	if err != nil {
		t.Fatalf("NewAccessToken: %v", err)
	}

	claims, err := issuer.ParseAccessToken(raw)
	if err != nil {
		t.Fatalf("ParseAccessToken: %v", err)
	}
	if claims.UserID != "user-1" || !claims.IsAdmin {
		t.Errorf("claims = %+v, want UserID=user-1 IsAdmin=true", claims)
	}
}

func TestAccessToken_RejectsExpired(t *testing.T) {
	issuer := NewIssuer("test-secret")

	raw, err := issuer.NewAccessToken("user-1", false, -time.Minute) // already expired
	if err != nil {
		t.Fatalf("NewAccessToken: %v", err)
	}
	if _, err := issuer.ParseAccessToken(raw); err == nil {
		t.Error("ParseAccessToken accepted an expired token")
	}
}

func TestAccessToken_RejectsWrongSecret(t *testing.T) {
	issued := NewIssuer("secret-a")
	raw, err := issued.NewAccessToken("user-1", false, time.Minute)
	if err != nil {
		t.Fatalf("NewAccessToken: %v", err)
	}

	verifier := NewIssuer("secret-b")
	if _, err := verifier.ParseAccessToken(raw); err == nil {
		t.Error("ParseAccessToken accepted a token signed with a different secret")
	}
}

func TestAccessToken_RejectsGarbage(t *testing.T) {
	issuer := NewIssuer("test-secret")
	if _, err := issuer.ParseAccessToken("not-a-jwt"); err == nil {
		t.Error("ParseAccessToken accepted a garbage string")
	}
}

func TestOpaqueToken_HashIsDeterministic(t *testing.T) {
	raw, hash, err := NewOpaque()
	if err != nil {
		t.Fatalf("NewOpaque: %v", err)
	}
	if got := HashOpaque(raw); got != hash {
		t.Errorf("HashOpaque(raw) = %q, want %q (the hash NewOpaque returned)", got, hash)
	}
}

func TestOpaqueToken_ProducesUniqueValues(t *testing.T) {
	_, hash1, err := NewOpaque()
	if err != nil {
		t.Fatalf("NewOpaque: %v", err)
	}
	_, hash2, err := NewOpaque()
	if err != nil {
		t.Fatalf("NewOpaque: %v", err)
	}
	if hash1 == hash2 {
		t.Error("two calls to NewOpaque produced the same hash")
	}
}
