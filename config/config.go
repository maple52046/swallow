// Package config defines the runtime configuration model for swallow.
// All environment variable parsing, config file loading, and validation are
// centralized here; application code receives a fully assembled Config object.
package config

// Config is the top-level runtime configuration.
// Populated by merging: defaults → config file → CLI flags → env vars.
type Config struct {
	API   APIConfig   `yaml:"api"`
	Agent AgentConfig `yaml:"agent"`
}

// APIConfig holds all configuration required to run the API server.
type APIConfig struct {
	// Addr is the HTTP listen address, e.g. ":3000".
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
}

// AgentConfig holds configuration for the agent subcommand.
type AgentConfig struct {
	// ControllerAddr is the base URL of the API server the agent connects to.
	ControllerAddr string `yaml:"controllerAddr"`
}
