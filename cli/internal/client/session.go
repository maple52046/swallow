package client

import (
	"context"
	"errors"
	"net/http"
	"time"
)

// refreshLeeway is how early before TokenExpiresAt the client renews the access
// token, so a request does not race the expiry on a slow link.
const refreshLeeway = time.Minute

// SessionTokens is a Session renewal the caller must persist (auth-refresh.md).
// RefreshToken is empty when the server answered from its rotation grace (another
// process refreshed the same Session moments earlier); the caller then keeps the
// newest refresh token it already stored.
type SessionTokens struct {
	AccessToken          string
	AccessTokenExpiresAt time.Time
	RefreshToken         string
}

// sessionRenewable reports whether a request with this AuthMode presents a
// Session access token that this client can refresh.
func (c *Client) sessionRenewable(mode AuthMode) bool {
	if c.apiKey != "" || c.refreshToken == "" {
		return false
	}
	switch mode {
	case AuthBearer:
		return true
	case AuthMachine:
		// The machine token, when configured, is sent instead of the Session.
		return c.machineToken == ""
	default:
		return false
	}
}

func (c *Client) expiresSoon() bool {
	return !c.tokenExpiresAt.IsZero() && time.Until(c.tokenExpiresAt) < refreshLeeway
}

// refresh exchanges the refresh token for a new access token (body delivery),
// updates the client in place, and hands the result to the persistence callback.
// A 401 means the Session is over (expired, logged out, or revoked after reuse)
// and is returned with a hint to sign in again.
func (c *Client) refresh(ctx context.Context) error {
	resp, err := c.send(ctx, Request{
		Method: "POST",
		Path:   "auth/refresh",
		Body:   map[string]string{"refreshToken": c.refreshToken},
		Auth:   AuthNone,
	})
	var apiErr *APIError
	if errors.As(err, &apiErr) && apiErr.Status == http.StatusUnauthorized {
		apiErr.Hint = "your session has ended; run `swallow login` to sign in again"
		return apiErr
	}
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	var out struct {
		AccessToken          string    `json:"accessToken"`
		AccessTokenExpiresAt time.Time `json:"accessTokenExpiresAt"`
		RefreshToken         string    `json:"refreshToken"`
	}
	if err := decodeBody(resp, &out); err != nil {
		return err
	}
	if out.AccessToken == "" {
		return errors.New("refresh succeeded but no access token was returned")
	}
	c.token = out.AccessToken
	c.tokenExpiresAt = out.AccessTokenExpiresAt
	if out.RefreshToken != "" {
		c.refreshToken = out.RefreshToken
	}
	if c.onRefreshed != nil {
		return c.onRefreshed(SessionTokens{
			AccessToken:          out.AccessToken,
			AccessTokenExpiresAt: out.AccessTokenExpiresAt,
			RefreshToken:         out.RefreshToken,
		})
	}
	return nil
}

// unauthorizedHint explains an unrecoverable 401 for the credential in use.
func (c *Client) unauthorizedHint() string {
	switch {
	case c.apiKey != "":
		return "the API key is invalid, expired, or deleted; check --api-key, SWALLOW_API_KEY, or the stored profile"
	case c.token != "":
		return "the access token is no longer valid; run `swallow login` to sign in again"
	default:
		return "not signed in; run `swallow login` or set an API key"
	}
}
