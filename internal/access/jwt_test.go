package access

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"testing"
	"time"
)

func TestJWTAuthenticatorAcceptsValidTokenAndRejectsExpiredToken(t *testing.T) {
	secret := "01234567890123456789012345678901"
	authenticator, err := NewJWTAuthenticator(secret)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1_700_000_000, 0)
	token := signTestToken(t, secret, map[string]any{
		"sub":   "user-123",
		"roles": []string{"member"},
		"exp":   now.Add(time.Hour).Unix(),
	})
	principal, err := authenticator.Authenticate(token, now)
	if err != nil {
		t.Fatal(err)
	}
	if principal.Subject != "user-123" || !HasRole(principal, "member") {
		t.Fatalf("unexpected principal: %#v", principal)
	}

	expired := signTestToken(t, secret, map[string]any{"sub": "user-123", "exp": now.Add(-time.Second).Unix()})
	if _, err := authenticator.Authenticate(expired, now); err == nil {
		t.Fatal("expected expired token to be rejected")
	}
}

func TestBearerToken(t *testing.T) {
	if token, err := BearerToken("Bearer abc"); err != nil || token != "abc" {
		t.Fatalf("unexpected bearer token: %q, %v", token, err)
	}
	if _, err := BearerToken("Basic abc"); err == nil {
		t.Fatal("expected non-bearer credentials to be rejected")
	}
}

func TestJWTAuthenticatorValidatesIssuerAndAudienceWhenConfigured(t *testing.T) {
	secret := "01234567890123456789012345678901"
	now := time.Unix(1_700_000_000, 0)
	authenticator, err := NewJWTAuthenticatorWithClaims(secret, "https://issuer.example", "nexus-api")
	if err != nil {
		t.Fatal(err)
	}
	token := signTestToken(t, secret, map[string]any{
		"sub": "user-123", "iss": "https://issuer.example", "aud": []string{"nexus-api", "other"}, "exp": now.Add(time.Hour).Unix(),
	})
	if _, err := authenticator.Authenticate(token, now); err != nil {
		t.Fatal(err)
	}
	wrongAudience := signTestToken(t, secret, map[string]any{
		"sub": "user-123", "iss": "https://issuer.example", "aud": "other", "exp": now.Add(time.Hour).Unix(),
	})
	if _, err := authenticator.Authenticate(wrongAudience, now); err == nil {
		t.Fatal("expected wrong audience to be rejected")
	}
}

func TestJWTAuthenticatorUsesFractionalNumericDateAndRejectsOutOfRangeValues(t *testing.T) {
	secret := "01234567890123456789012345678901"
	authenticator, err := NewJWTAuthenticator(secret)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1_700_000_000, 600_000_000)
	fractionalExpiry := signTestToken(t, secret, map[string]any{
		"sub": "user-123", "exp": json.Number("1700000000.5"),
	})
	if _, err := authenticator.Authenticate(fractionalExpiry, now); err == nil {
		t.Fatal("expected a token past a fractional expiry to be rejected")
	}

	outOfRange := signTestToken(t, secret, map[string]any{
		"sub": "user-123", "exp": json.Number("1e100"),
	})
	if _, err := authenticator.Authenticate(outOfRange, time.Unix(1_700_000_000, 0)); err == nil {
		t.Fatal("expected an out-of-range numeric date to be rejected")
	}
}

func signTestToken(t *testing.T, secret string, claims map[string]any) string {
	t.Helper()
	encode := func(value any) string {
		data, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		return base64.RawURLEncoding.EncodeToString(data)
	}
	unsigned := encode(map[string]string{"alg": "HS256", "typ": "JWT"}) + "." + encode(claims)
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(unsigned))
	return unsigned + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
