package jwt

import (
	"testing"
	"time"

	gojwt "github.com/golang-jwt/jwt/v5"
)

func TestIssue_CarriesSessionAndAccessType(t *testing.T) {
	svc := NewService("secret", 15*time.Minute)
	token, expiresAt, err := svc.Issue(AccessTokenInput{UserID: "u1", Username: "alice", Role: "admin", SessionID: "s1"})
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if d := time.Until(expiresAt); d <= 14*time.Minute || d > 15*time.Minute {
		t.Errorf("expiresAt in %v, want about 15m", d)
	}
	claims, err := svc.Verify(token)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if claims.Type != TypeAccess || claims.SessionID != "s1" || claims.Issuer != Issuer || claims.ID == "" {
		t.Errorf("claims = %+v, want typ=access, sid=s1, iss=swallow, and a jti", claims)
	}
}

func TestVerify_AcceptsPreSessionToken(t *testing.T) {
	svc := NewService("secret", time.Hour)
	// The shape tokens had before Sessions: no typ, no iss, no sid.
	legacy := gojwt.NewWithClaims(gojwt.SigningMethodHS256, Claims{
		UserID: "u1", Username: "alice", Role: "admin",
		RegisteredClaims: gojwt.RegisteredClaims{
			Subject:   "u1",
			ExpiresAt: gojwt.NewNumericDate(time.Now().Add(time.Hour)),
			IssuedAt:  gojwt.NewNumericDate(time.Now()),
		},
	})
	signed, err := legacy.SignedString([]byte("secret"))
	if err != nil {
		t.Fatalf("sign legacy token: %v", err)
	}
	if _, err := svc.Verify(signed); err != nil {
		t.Errorf("Verify(pre-Session token) = %v, want accepted until it expires", err)
	}
}

func TestVerify_RejectsOtherTypesAndExpiredTokens(t *testing.T) {
	svc := NewService("secret", time.Minute)
	other := gojwt.NewWithClaims(gojwt.SigningMethodHS256, Claims{
		UserID: "u1", Type: "refresh",
		RegisteredClaims: gojwt.RegisteredClaims{ExpiresAt: gojwt.NewNumericDate(time.Now().Add(time.Hour))},
	})
	signed, err := other.SignedString([]byte("secret"))
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	if _, err := svc.Verify(signed); err == nil {
		t.Error("Verify accepted a token whose typ is not access")
	}

	token, _, err := svc.Issue(AccessTokenInput{UserID: "u1"})
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	svc.now = func() time.Time { return time.Now().Add(2 * time.Minute) }
	if _, err := svc.Verify(token); err == nil {
		t.Error("Verify accepted an expired access token")
	}
}

func TestVerify_RejectsWrongSecret(t *testing.T) {
	token, _, err := NewService("secret", time.Minute).Issue(AccessTokenInput{UserID: "u1"})
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if _, err := NewService("other", time.Minute).Verify(token); err == nil {
		t.Error("Verify accepted a token signed with another secret")
	}
}
