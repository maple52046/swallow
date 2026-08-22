// Package config defines the runtime configuration model for swallow.
// All environment variable parsing, config file loading, and validation are
// centralized here; application code receives a fully assembled Config object.
//
// Configuration covers only how this process runs. What it talks to — provisioners,
// automation controllers, metrics stores, clusters — is registered at runtime as
// integration records, because a fleet has many of each and they change without a
// redeploy. See docs/decisions/001-system-ownership-boundaries.md.
package config

import "time"

// Config is the top-level runtime configuration.
// Populated by merging: defaults → config file → CLI flags → env vars.
type Config struct {
	API APIConfig `yaml:"api"`
}

// APIConfig holds all configuration required to run the API server.
type APIConfig struct {
	// Addr is the HTTP listen address, e.g. ":30051".
	Addr string `yaml:"addr"`
	// MongoURI is the full MongoDB connection string.
	MongoURI string `yaml:"mongoUri"`
	// MongoDB is the MongoDB database name.
	MongoDB string `yaml:"mongoDb"`
	// JWTSecret is the HMAC secret used to sign JWT tokens. Never log this.
	JWTSecret string `yaml:"jwtSecret"`
	// JWTExpiryHours is the number of hours before a JWT token expires.
	JWTExpiryHours int `yaml:"jwtExpiryHours"`
	// BootstrapAdminUsername is the username seeded on first startup.
	BootstrapAdminUsername string `yaml:"bootstrapAdminUsername"`
	// BootstrapAdminPassword is the password for the bootstrap admin. Never log this.
	BootstrapAdminPassword string `yaml:"bootstrapAdminPassword"`
	// CredentialKey is a base64-encoded 32-byte key used to seal integration
	// credentials at rest. Required: an integration cannot be registered without
	// it, and starting without it would defer that failure to the first operator
	// who tries. Never log this value.
	CredentialKey string `yaml:"credentialKey"`
	// ReconcileInterval is how often each enabled provisioner integration is
	// polled to refresh its server projections.
	ReconcileInterval time.Duration `yaml:"reconcileInterval"`
	// InventoryInterval is how often the attached-hardware inventory (GPUs) is
	// refreshed. Much longer than ReconcileInterval because attached hardware costs a
	// call per machine and changes only at commissioning, so polling it as often as
	// lifecycle state would multiply request count for near-static data.
	InventoryInterval time.Duration `yaml:"inventoryInterval"`
	// OperationPollInterval is how often unfinished operations are re-read from
	// their automation controller. Shorter than ReconcileInterval because a job
	// changes state far faster than a fleet's inventory does.
	OperationPollInterval time.Duration `yaml:"operationPollInterval"`
	// MachineToken is a static bearer token accepted by the endpoints whose callers
	// are other systems: Prometheus scraping discovery, AWX posting a job
	// notification. Those hold one credential in their configuration and cannot
	// refresh a JWT. Optional: without it those endpoints require an admin JWT.
	// Never log this value.
	MachineToken string `yaml:"machineToken"`
}
