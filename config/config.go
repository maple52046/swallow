// Package config defines the runtime configuration model for swallow.
// All environment variable parsing, config file loading, and validation are
// centralized here; application code receives a fully assembled Config object.
package config

import "time"

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
	// GRPCAddr is the gRPC listen address for the agent stream service, e.g. ":50051".
	GRPCAddr string `yaml:"grpcAddr"`
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
	// NodeAuthToken is the shared secret that agents must present when connecting
	// via gRPC. Never log this value.
	NodeAuthToken string `yaml:"nodeAuthToken"`
}

// AgentConfig holds configuration for the agent subcommand.
type AgentConfig struct {
	// NodeID is the pre-configured node identity. When empty, identity.Resolve
	// falls back to the state file or generates a UUID.
	NodeID string `yaml:"nodeId"`
	// ServerAddress is the gRPC address of the API server, e.g. "localhost:50051".
	ServerAddress string `yaml:"serverAddress"`
	// HeartbeatInterval controls how often the agent sends a heartbeat over the stream.
	HeartbeatInterval time.Duration `yaml:"heartbeatInterval"`
	// StateFile is the path to the local agent state JSON file used for node ID persistence.
	StateFile string `yaml:"stateFile"`
	// AuthToken is the bearer token used to authenticate against the API server. Never log this.
	AuthToken string `yaml:"authToken"`
}
