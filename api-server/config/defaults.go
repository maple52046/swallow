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
			Addr:                      ":30051",
			MongoURI:                  "mongodb://localhost:27017",
			MongoDB:                   "swallow",
			JWTSecret:                 "changeme-in-production",
			JWTExpiryHours:            24,
			BootstrapAdminUsername:    "admin",
			BootstrapAdminPassword:    "admin",
			ReconcileInterval:         60 * time.Second,
			InventoryInterval:         15 * time.Minute,
			OperationDispatchInterval: time.Second,
			OperationLeaseDuration:    90 * time.Second,
			AnsibleRunnerCommand:      "ansible-runner",
			PlaybookManifest:          "automation/manifest.json",
			PlaybookDir:               "automation/playbooks",
			JobRuntimeDir:             "/tmp/swallow/jobs",
			JobArtifactDir:            "./var/jobs",
			JobArtifactRetention:      30 * 24 * time.Hour,
		},
	}
}
