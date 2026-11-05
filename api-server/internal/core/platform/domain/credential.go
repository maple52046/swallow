package domain

// HookCredential is the hook name a Platform module lists in its module.json for its
// CredentialReader.
const HookCredential = "platform-credential"

// Credential is what a Platform deployment returned for swallow to reach the new Platform with: the
// API endpoint, the read token, an optional operator token, and the connection settings the module's
// reader needs (the Integration's settings).
//
// The two tokens are a security boundary, not two copies of one access. Token is sealed as the
// Integration's credential and authenticates every background read (membership, controller
// discovery) and every read an admin asks for, so a module should return one that can only read.
// OperatorToken is sealed as the Integration's operator credential and authenticates only the
// operations that change the Platform's runtime and the reads Token is denied by design, each on an
// admin's request (ConnectionOperate); no background work ever uses it. A module whose deployment
// returns no second token leaves it empty, and those operations then use Token (see
// OperatorConnectionSource). Neither token is ever returned by the API, logged, or kept on the
// Platform.
type Credential struct {
	Endpoint      string
	Token         string
	OperatorToken string
	Settings      map[string]string
}

// CredentialReader turns a deployment's result into the Platform's Credential. Each Platform
// module provides one; the composition root registers it for the completion type the module's
// deploy Task declares.
type CredentialReader interface {
	PlatformType() PlatformType
	// Completion is the completion type the module's deploy Task declares for its credential.
	Completion() string
	// Required reports whether a deployment that returned no credential failed.
	Required() bool
	// Credential reads a succeeded Task's result data; ok is false when it holds no read token. A
	// result without an operator token is still a credential, with OperatorToken empty.
	Credential(data map[string]any) (credential Credential, ok bool)
}
