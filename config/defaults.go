package config

import "time"

// DefaultConfig returns the baseline configuration suitable for local development.
// All values can be overridden by a config file, CLI flags, or environment variables.
//
// CredentialKey has no default on purpose: a shipped default encryption key is worse
// than none, because it looks like protection and is not.
func DefaultConfig() Config {
	return Config{
		API: APIConfig{
			Addr:                   ":30051",
			MongoURI:               "mongodb://localhost:27017",
			MongoDB:                "swallow",
			JWTSecret:              "changeme-in-production",
			JWTExpiryHours:         24,
			BootstrapAdminUsername: "admin",
			BootstrapAdminPassword: "admin",
			ReconcileInterval:      60 * time.Second,
			OperationPollInterval:  15 * time.Second,
		},
	}
}
