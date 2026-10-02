// Package jwt issues and verifies the Session access tokens of the HTTP API (decision 042).
//
// Access tokens are short-lived HS256 JWTs signed with the installation's `jwtSecret`. They are
// the only JWTs this package handles: refresh tokens and API Keys are opaque random secrets owned
// by the auth and apikey slices, never JWTs. The claim set is an implementation detail — the
// published contracts tell consumers to treat tokens as opaque — so claims may grow, but a token
// accepted today must keep verifying until it expires.
package jwt

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const (
	// Issuer is the `iss` claim of every access token swallow issues.
	Issuer = "swallow"
	// TypeAccess is the `typ` claim of an access token. Tokens issued before Sessions existed
	// carry no `typ` and are accepted as access tokens until they expire.
	TypeAccess = "access"
)

// Claims is the claim set of an access token.
type Claims struct {
	UserID   string `json:"sub"`
	Username string `json:"username"`
	Role     string `json:"role"`
	// Type distinguishes access tokens from any future token kind; see TypeAccess.
	Type string `json:"typ,omitempty"`
	// SessionID is the Session that issued the token; empty for pre-Session tokens.
	SessionID string `json:"sid,omitempty"`
	jwt.RegisteredClaims
}

// AccessTokenInput identifies who an access token speaks for.
type AccessTokenInput struct {
	UserID    string
	Username  string
	Role      string
	SessionID string
}

// Service signs and verifies access tokens. It holds only the signing secret and the token
// lifetime, so it is safe for concurrent use.
type Service struct {
	secret []byte
	ttl    time.Duration
	now    func() time.Time
}

// NewService returns a Service whose access tokens live for ttl (accessTokenTTL in config).
func NewService(secret string, ttl time.Duration) *Service {
	return &Service{secret: []byte(secret), ttl: ttl, now: time.Now}
}

// TTL is how long an access token issued now stays valid.
func (s *Service) TTL() time.Duration { return s.ttl }

// Issue signs an access token for input and returns it with its expiry. Each token gets a
// random `jti`, so two tokens issued in the same second still differ.
func (s *Service) Issue(input AccessTokenInput) (string, time.Time, error) {
	jti, err := randomID()
	if err != nil {
		return "", time.Time{}, err
	}
	issuedAt := s.now()
	expiresAt := issuedAt.Add(s.ttl)
	claims := Claims{
		UserID:    input.UserID,
		Username:  input.Username,
		Role:      input.Role,
		Type:      TypeAccess,
		SessionID: input.SessionID,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    Issuer,
			Subject:   input.UserID,
			ID:        jti,
			ExpiresAt: jwt.NewNumericDate(expiresAt),
			IssuedAt:  jwt.NewNumericDate(issuedAt),
		},
	}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(s.secret)
	if err != nil {
		return "", time.Time{}, err
	}
	return token, expiresAt, nil
}

// Sign issues an access token that belongs to no Session. Tests and tooling use it to mint a
// credential for a known user; production logins go through Issue with a SessionID so logout can
// revoke the Session.
func (s *Service) Sign(userID, username, role string) (string, error) {
	token, _, err := s.Issue(AccessTokenInput{UserID: userID, Username: username, Role: role})
	return token, err
}

// Verify checks the signature, expiry, issuer, and type of an access token and returns its
// claims. Only HMAC-signed tokens are accepted, which rules out `alg: none` and key-confusion
// attacks. Any failure is returned as an error; callers map every error to `401 unauthorized`
// without distinguishing causes.
func (s *Service) Verify(tokenStr string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenStr, &Claims{}, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("unexpected signing method")
		}
		return s.secret, nil
	}, jwt.WithTimeFunc(s.now))
	if err != nil {
		return nil, err
	}
	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, errors.New("invalid token")
	}
	// Pre-Session tokens have neither `typ` nor `iss`; accepting the empty values keeps them
	// valid until they expire after an upgrade.
	if claims.Type != "" && claims.Type != TypeAccess {
		return nil, fmt.Errorf("token type %q is not an access token", claims.Type)
	}
	if claims.Issuer != "" && claims.Issuer != Issuer {
		return nil, fmt.Errorf("token issuer %q is not swallow", claims.Issuer)
	}
	return claims, nil
}

func randomID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate token id: %w", err)
	}
	return hex.EncodeToString(b), nil
}
