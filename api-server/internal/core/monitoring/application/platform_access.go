package application

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	monitoringdomain "github.com/maple52046/swallow/internal/core/monitoring/domain"
)

// platformTokenPrefix marks a Platform monitoring token, so a leaked one is recognisable.
const platformTokenPrefix = "swpm_"

// ErrPlatformAccessDenied means a request did not present the Platform's current monitoring
// token. It does not say whether the Platform exists.
var ErrPlatformAccessDenied = errors.New("platform monitoring access denied")

// PlatformAccess issues and verifies Platform monitoring tokens: the bearer credential a Platform's
// Grafana sends to swallow's gateway, which then reads only that Platform's series. A Platform has
// one token at a time; only its hash is stored.
type PlatformAccess struct {
	tokens monitoringdomain.PlatformTokenStore
	random io.Reader
	now    func() time.Time
}

// NewPlatformAccess builds the token service over its store.
func NewPlatformAccess(tokens monitoringdomain.PlatformTokenStore) *PlatformAccess {
	return &PlatformAccess{tokens: tokens, random: rand.Reader, now: time.Now}
}

// Issue gives the Platform a new token and returns it; the previous token stops working. The
// caller hands it to the Platform's Grafana as a secret and keeps no copy.
func (a *PlatformAccess) Issue(ctx context.Context, platformID string) (string, error) {
	if strings.TrimSpace(platformID) == "" {
		return "", fmt.Errorf("issue a platform monitoring token: empty platform id")
	}
	secret := make([]byte, 32)
	if _, err := io.ReadFull(a.random, secret); err != nil {
		return "", fmt.Errorf("issue a platform monitoring token: %w", err)
	}
	token := platformTokenPrefix + base64.RawURLEncoding.EncodeToString(secret)
	if err := a.tokens.SaveToken(ctx, platformID, hashToken(token), a.now().UTC()); err != nil {
		return "", err
	}
	return token, nil
}

// Verify returns nil when token is the Platform's current token and ErrPlatformAccessDenied
// otherwise, including when the Platform has no token at all.
func (a *PlatformAccess) Verify(ctx context.Context, platformID, token string) error {
	if platformID == "" || !strings.HasPrefix(token, platformTokenPrefix) {
		return ErrPlatformAccessDenied
	}
	stored, err := a.tokens.TokenHash(ctx, platformID)
	if errors.Is(err, monitoringdomain.ErrPlatformTokenNotFound) {
		return ErrPlatformAccessDenied
	}
	if err != nil {
		return err
	}
	if subtle.ConstantTimeCompare(stored, hashToken(token)) != 1 {
		return ErrPlatformAccessDenied
	}
	return nil
}

func hashToken(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}
