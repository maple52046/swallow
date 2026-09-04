package config

import "fmt"

// Validate checks that all required fields are present in the final merged config.
// Validation must run after all sources have been merged so that it reflects the
// true effective configuration.
func Validate(cfg *Config) error {
	if cfg.API.Addr == "" {
		return fmt.Errorf("api.addr is required")
	}
	if cfg.API.MongoURI == "" {
		return fmt.Errorf("api.mongoUri is required")
	}
	if cfg.API.MongoDB == "" {
		return fmt.Errorf("api.mongoDb is required")
	}
	if cfg.API.JWTSecret == "" {
		return fmt.Errorf("api.jwtSecret is required")
	}
	if cfg.API.JWTExpiryHours <= 0 {
		return fmt.Errorf("api.jwtExpiryHours must be > 0")
	}
	if cfg.API.CredentialKey == "" {
		return fmt.Errorf(
			"api.credentialKey is required; generate one with: openssl rand -base64 32")
	}
	if cfg.API.ReconcileInterval <= 0 {
		return fmt.Errorf("api.reconcileInterval must be > 0")
	}
	if cfg.API.InventoryInterval <= 0 {
		return fmt.Errorf("api.inventoryInterval must be > 0")
	}
	if cfg.API.OperationDispatchInterval <= 0 {
		return fmt.Errorf("api.operationDispatchInterval must be > 0")
	}
	if cfg.API.OperationLeaseDuration < 3*cfg.API.OperationDispatchInterval {
		return fmt.Errorf("api.operationLeaseDuration must be at least 3x operationDispatchInterval")
	}
	if cfg.API.OperationMaxParallelism <= 0 {
		return fmt.Errorf("api.operationMaxParallelism must be positive")
	}
	if cfg.API.TemporalAddress == "" || cfg.API.TemporalNamespace == "" || cfg.API.TemporalTaskQueue == "" {
		return fmt.Errorf("api.temporalAddress, api.temporalNamespace, and api.temporalTaskQueue are required")
	}
	if cfg.API.TemporalStartInterval <= 0 {
		return fmt.Errorf("api.temporalStartInterval must be > 0")
	}
	if cfg.API.AnsibleRunnerCommand == "" {
		return fmt.Errorf("api.ansibleRunnerCommand is required")
	}
	if cfg.API.PlaybookManifest == "" || cfg.API.PlaybookDir == "" {
		return fmt.Errorf("api.playbookManifest and api.playbookDir are required")
	}
	if cfg.API.JobRuntimeDir == "" || cfg.API.JobArtifactDir == "" {
		return fmt.Errorf("api.jobRuntimeDir and api.jobArtifactDir are required")
	}
	if cfg.API.JobArtifactRetention <= 0 {
		return fmt.Errorf("api.jobArtifactRetention must be > 0")
	}
	return nil
}
