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
	if cfg.API.OperationPollInterval <= 0 {
		return fmt.Errorf("api.operationPollInterval must be > 0")
	}
	return nil
}
