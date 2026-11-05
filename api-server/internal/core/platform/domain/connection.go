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

// ConnectionAccess is the privilege a request needs on a Platform's own API, which decides the
// credential its Connection authenticates with (see Credential).
type ConnectionAccess int

const (
	// ConnectionRead authenticates with the Integration's credential, the read token. Every
	// background read and every read an admin asks for uses it, unless the read is one the read
	// credential is denied by design.
	ConnectionRead ConnectionAccess = iota
	// ConnectionOperate authenticates with the Integration's operator credential. Only an admin's
	// request may ask for it: an operation that changes the Platform's runtime, or a read the read
	// credential is denied by design (for example one that returns secret values to swallow).
	ConnectionOperate
)

// OperatorConnectionSource resolves a Platform into a Connection for either access: Connection is
// the ConnectionRead one and OperatorConnection the ConnectionOperate one.
//
// Nothing that runs in the background may call OperatorConnection, so the operator credential is
// decrypted only for an admin's request. When the Integration holds no operator credential (a
// Platform deployed before its module returned one) OperatorConnection authenticates with the read
// credential instead, the only credential swallow has for that Platform, so such a Platform keeps
// the access it had and gains none. It returns the same errors as Connection.
type OperatorConnectionSource interface {
	ConnectionSource
	OperatorConnection(ctx context.Context, platform *Platform) (Connection, error)
}

// OperatorClientFactory is the capability of a Platform module's client factory, whose For builds
// a client of type C from the ConnectionRead Connection, to build one from the ConnectionOperate
// Connection (OperatorConnectionSource.OperatorConnection) for ConnectionOperate requests.
//
// It returns the same errors as the factory's For. A factory without it has only the read client
// to give, so a ConnectionOperate request gets that one: the request can then be denied by the
// Platform, but it never authenticates with more than reads already do.
type OperatorClientFactory[C any] interface {
	ForOperator(ctx context.Context, platform *Platform) (C, error)
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
