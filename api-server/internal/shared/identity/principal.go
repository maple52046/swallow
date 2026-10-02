// Package identity holds the authenticated caller of one API request, independent of how the
// caller proved who they are.
//
// The auth middleware builds a Principal from either a Session access token or an API Key
// (decision 042) and every handler reads that one shape, so authorization never branches on
// the credential type unless a contract says so (for example, API Keys cannot create API Keys).
// The package is framework-free: it must not import Fiber, MongoDB, or any feature slice, so
// feature slices and middleware can both depend on it. Do not put authorization rules here.
package identity

// Method is how a request authenticated. The values are the wire values of `authMethod` in
// the auth-me contract.
type Method string

const (
	// MethodSession is a Session access token from login or refresh (including tokens issued
	// before Sessions existed, which carry no Session ID).
	MethodSession Method = "session"
	// MethodAPIKey is an API Key secret.
	MethodAPIKey Method = "api_key"
)

// Principal is the User a request acts as.
//
// Role is the role the credential carries: for an access token, the role at the time it was
// issued (at most one access-token lifetime old); for an API Key, the owner's current role.
// Exactly one of SessionID and APIKeyID is set, except for pre-Session access tokens, which have
// neither. Principals are request-scoped values and must not be cached across requests.
type Principal struct {
	UserID   string
	Username string
	Role     string
	Method   Method
	// SessionID identifies the Session behind an access token, so logout can revoke it.
	SessionID string
	// APIKeyID identifies the API Key that authenticated the request.
	APIKeyID string
}
