package config

import "time"

// DefaultConfig returns the baseline configuration suitable for local development.
// All values can be overridden by a config file, CLI flags, or environment variables.
func DefaultConfig() Config {
	return Config{
		API: APIConfig{
			Addr:                   ":30051",
			GRPCAddr:               ":50051",
			MongoURI:               "mongodb://localhost:27017",
			MongoDB:                "swallow",
			JWTSecret:              "changeme-in-production",
			JWTExpiryHours:         24,
			BootstrapAdminUsername: "admin",
			BootstrapAdminPassword: "admin",
		},
		Agent: AgentConfig{
			ServerAddress:     "localhost:50051",
			HeartbeatInterval: 30 * time.Second,
			StateFile:         "/var/lib/swallow/agent-state.json",
		},
	}
}
