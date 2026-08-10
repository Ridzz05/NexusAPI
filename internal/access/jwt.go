package access

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
)

var (
	ErrMissingToken = errors.New("missing bearer token")
	ErrInvalidToken = errors.New("invalid bearer token")
)

// Principal is the identity available to handlers after authentication.
type Principal struct {
	Subject string   `json:"subject"`
	Roles   []string `json:"roles"`
}

type Authenticator interface {
	Authenticate(token string, now time.Time) (Principal, error)
}

// JWTAuthenticator validates compact JWTs signed with HS256. Token issuance is
// intentionally outside NexusAPI; this component only verifies requests.
type JWTAuthenticator struct {
	secret   []byte
	issuer   string
	audience string
}

func NewJWTAuthenticator(secret string) (*JWTAuthenticator, error) {
	return NewJWTAuthenticatorWithClaims(secret, "", "")
}

func NewJWTAuthenticatorWithClaims(secret, issuer, audience string) (*JWTAuthenticator, error) {
	if len([]byte(secret)) < 32 {
		return nil, errors.New("JWT secret must contain at least 32 bytes")
	}
	return &JWTAuthenticator{secret: []byte(secret), issuer: issuer, audience: audience}, nil
}

func (a *JWTAuthenticator) Authenticate(token string, now time.Time) (Principal, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return Principal{}, ErrInvalidToken
	}

	var header struct {
		Algorithm string `json:"alg"`
		Type      string `json:"typ"`
	}
	if err := decodeJSON(parts[0], &header); err != nil || header.Algorithm != "HS256" || (header.Type != "" && header.Type != "JWT") {
		return Principal{}, ErrInvalidToken
	}

	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || len(signature) != sha256.Size {
		return Principal{}, ErrInvalidToken
	}
	mac := hmac.New(sha256.New, a.secret)
	_, _ = mac.Write([]byte(parts[0] + "." + parts[1]))
	expected := mac.Sum(nil)
	if subtle.ConstantTimeCompare(signature, expected) != 1 {
		return Principal{}, ErrInvalidToken
	}

	var claims map[string]json.RawMessage
	if err := decodeJSON(parts[1], &claims); err != nil {
		return Principal{}, ErrInvalidToken
	}
	var subject string
	if err := json.Unmarshal(claims["sub"], &subject); err != nil || strings.TrimSpace(subject) == "" {
		return Principal{}, ErrInvalidToken
	}
	if a.issuer != "" {
		var issuer string
		if err := json.Unmarshal(claims["iss"], &issuer); err != nil || issuer != a.issuer {
			return Principal{}, ErrInvalidToken
		}
	}
	if a.audience != "" && !claimContains(claims["aud"], a.audience) {
		return Principal{}, ErrInvalidToken
	}
	expiresAt, err := numericClaim(claims, "exp")
	if err != nil || unixSeconds(now) >= expiresAt {
		return Principal{}, ErrInvalidToken
	}
	if raw, ok := claims["nbf"]; ok {
		notBefore, err := numericRawClaim(raw)
		if err != nil || unixSeconds(now) < notBefore {
			return Principal{}, ErrInvalidToken
		}
	}

	roles := []string{}
	if raw, ok := claims["roles"]; ok {
		if err := json.Unmarshal(raw, &roles); err != nil {
			return Principal{}, ErrInvalidToken
		}
	}
	if raw, ok := claims["role"]; ok {
		var role string
		if err := json.Unmarshal(raw, &role); err != nil {
			return Principal{}, ErrInvalidToken
		}
		if role != "" {
			roles = append(roles, role)
		}
	}

	return Principal{Subject: subject, Roles: roles}, nil
}

func BearerToken(header string) (string, error) {
	parts := strings.Fields(header)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || parts[1] == "" {
		return "", ErrMissingToken
	}
	return parts[1], nil
}

func HasRole(principal Principal, required string) bool {
	for _, role := range principal.Roles {
		if role == required {
			return true
		}
	}
	return false
}

func decodeJSON(encoded string, target any) error {
	decoded, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return err
	}
	return json.Unmarshal(decoded, target)
}

func numericClaim(claims map[string]json.RawMessage, name string) (float64, error) {
	value, ok := claims[name]
	if !ok {
		return 0, fmt.Errorf("missing %s claim", name)
	}
	return numericRawClaim(value)
}

func numericRawClaim(value json.RawMessage) (float64, error) {
	var number json.Number
	if err := json.Unmarshal(value, &number); err != nil {
		return 0, err
	}
	parsed, err := number.Float64()
	if err != nil || math.IsNaN(parsed) || math.IsInf(parsed, 0) || parsed > 1<<62 || parsed < -(1<<62) {
		return 0, errors.New("numeric date is out of range")
	}
	return parsed, nil
}

func unixSeconds(value time.Time) float64 {
	return float64(value.Unix()) + float64(value.Nanosecond())/1e9
}

func claimContains(raw json.RawMessage, expected string) bool {
	var single string
	if json.Unmarshal(raw, &single) == nil {
		return single == expected
	}
	var values []string
	if json.Unmarshal(raw, &values) != nil {
		return false
	}
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}
