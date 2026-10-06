// Package maas adapts Canonical MAAS to the OS provisioning provider port.
//
// Everything MAAS-specific is contained here: OAuth signing, the multipart
// request encoding the MAAS 2.0 API requires, and the translation of MAAS
// vocabulary into the provisioning domain types.
package maas

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// APIKey is a MAAS API key split into its OAuth components. MAAS shows operators
// a single "consumer_key:token_key:token_secret" string.
type APIKey struct {
	ConsumerKey string
	TokenKey    string
	TokenSecret string
}

// ParseAPIKey splits a MAAS API key into its three parts.
//
// The error never echoes the input, so that a mistyped key cannot leak into a log
// line or an API response.
func ParseAPIKey(raw string) (APIKey, error) {
	parts := strings.Split(strings.TrimSpace(raw), ":")
	if len(parts) != 3 {
		return APIKey{}, fmt.Errorf(
			"malformed MAAS API key: expected \"consumer_key:token_key:token_secret\", got %d colon-separated parts",
			len(parts),
		)
	}
	for i, p := range parts {
		if strings.TrimSpace(p) == "" {
			return APIKey{}, fmt.Errorf("malformed MAAS API key: part %d is empty", i+1)
		}
	}

	return APIKey{
		ConsumerKey: parts[0],
		TokenKey:    parts[1],
		TokenSecret: parts[2],
	}, nil
}

// credential re-joins the key into the "consumer_key:token_key:token_secret" form MAAS tools
// accept. It is a secret: only existing-host enrollment hands it out, and it must never be logged.
// It is deliberately not String(), so formatting an APIKey cannot print it by accident.
func (k APIKey) credential() string {
	return k.ConsumerKey + ":" + k.TokenKey + ":" + k.TokenSecret
}

// authorizationHeader builds a 0-legged OAuth 1.0 Authorization header.
//
// MAAS signs with the PLAINTEXT method and an empty consumer secret, so the
// signature is literally "&<token_secret>" and no part of the URL or body is
// covered by it. That is why one header shape works for both GET requests and
// multipart POSTs, and why the nonce and timestamp are the only per-request values.
func (k APIKey) authorizationHeader(nonce string, timestamp int64) string {
	return fmt.Sprintf(
		"OAuth oauth_version=%q, oauth_signature_method=%q, oauth_consumer_key=%q, "+
			"oauth_token=%q, oauth_signature=%q, oauth_nonce=%q, oauth_timestamp=%q",
		"1.0",
		"PLAINTEXT",
		k.ConsumerKey,
		k.TokenKey,
		"&"+url.QueryEscape(k.TokenSecret),
		nonce,
		strconv.FormatInt(timestamp, 10),
	)
}

// newNonce returns a random per-request nonce. MAAS only requires that it varies.
func newNonce() (string, error) {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate oauth nonce: %w", err)
	}
	return hex.EncodeToString(buf), nil
}
