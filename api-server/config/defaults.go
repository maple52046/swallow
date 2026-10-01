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
			ReconcileInterval:         30 * time.Second,
			InventoryInterval:         15 * time.Minute,
			SSHKeySyncInterval:        5 * time.Minute,
			OperationDispatchInterval: time.Second,
			OperationLeaseDuration:    90 * time.Second,
			OperationMaxParallelism:   4,
			TemporalAddress:           "localhost:7233",
			TemporalNamespace:         "default",
			TemporalTaskQueue:         "swallow-operations",
			TemporalStartInterval:     time.Second,
			AnsibleRunnerCommand:      "ansible-runner",
			PlaybookManifest:          "automation/manifest.json",
			PlaybookDir:               "automation/playbooks",
			JobRuntimeDir:             "/tmp/swallow/jobs",
			JobArtifactDir:            "./var/jobs",
			JobArtifactRetention:      30 * 24 * time.Hour,
			// 64 GiB accommodates a large custom OS image; the body is streamed and spooled,
			// not buffered, so this is a ceiling on a single upload rather than memory use.
			ImageUploadMaxBytes: 64 * 1024 * 1024 * 1024,
		},
	}
}
