package domain

import (
	"context"
	"time"
)

// Connection is how swallow reaches a deployed Platform's own API: its Integration's endpoint and
// credential and the connection settings recorded with it. A Platform module builds its reader and
// client from it.
type Connection struct {
	// ProviderKind is the Integration's provider kind; for an Integration swallow recorded, the
	// Platform type.
	ProviderKind string
	Endpoint     string
	Token        string
	Timeout      time.Duration
	// CACertificate is the PEM CA the API's certificate is verified against. When it is empty,
	// InsecureSkipVerify decides whether the system roots verify it or nothing does.
	CACertificate      string
	InsecureSkipVerify bool
	// Settings are all of the Integration's settings, for the keys a module reads.
	Settings map[string]string
}

// Setting returns the setting key, or fallback when it is absent or empty.
func (c Connection) Setting(key, fallback string) string {
	if value, ok := c.Settings[key]; ok && value != "" {
		return value
	}
	return fallback
}

// SettingBool reads a boolean setting. Anything other than "true" is false, because a misspelled
// value must not silently enable something.
func (c Connection) SettingBool(key string) bool {
	return c.Settings[key] == "true"
}

// ConnectionSource resolves a Platform into a Connection to its API.
type ConnectionSource interface {
	Connection(ctx context.Context, platform *Platform) (Connection, error)
}

// HookPlatformReader is the hook name a Platform module lists in its module.json for its
// ReaderBuilder.
const HookPlatformReader = "platform-reader"

// ReaderBuilder builds the reader of one Platform type from a Connection. Each Platform module
// provides one, and the core's ReaderFactory selects it by the Platform's type.
type ReaderBuilder interface {
	PlatformType() PlatformType
	NewReader(conn Connection) (PlatformReader, error)
}
