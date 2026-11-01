package domain

// HookCredential is the hook name a Platform module lists in its module.json for its
// CredentialReader.
const HookCredential = "platform-credential"

// Credential is what a Platform deployment returned for swallow to read the new Platform with: the
// API endpoint, the token sealed as the Integration's credential, and the connection settings the
// module's reader needs (the Integration's settings).
type Credential struct {
	Endpoint string
	Token    string
	Settings map[string]string
}

// CredentialReader turns a deployment's result into the Platform's read Credential. Each Platform
// module provides one; the composition root registers it for the completion type the module's
// deploy Task declares.
type CredentialReader interface {
	PlatformType() PlatformType
	// Completion is the completion type the module's deploy Task declares for its credential.
	Completion() string
	// Required reports whether a deployment that returned no credential failed.
	Required() bool
	// Credential reads a succeeded Task's result data; ok is false when it holds no credential.
	Credential(data map[string]any) (credential Credential, ok bool)
}
