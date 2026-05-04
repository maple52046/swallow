package config

// DefaultConfig returns the baseline configuration suitable for local development.
// All values can be overridden by a config file, CLI flags, or environment variables.
func DefaultConfig() Config {
	return Config{
		API: APIConfig{
			Addr:                   ":3000",
			MongoURI:               "mongodb://localhost:27017",
			MongoDB:                "swallow",
			JWTSecret:              "changeme-in-production",
			JWTExpiryHours:         24,
			BootstrapAdminUsername: "admin",
			BootstrapAdminPassword: "admin",
		},
		Agent: AgentConfig{
			ControllerAddr: "http://localhost:3000",
		},
	}
}
