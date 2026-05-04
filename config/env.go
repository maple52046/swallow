package config

import (
	"fmt"
	"os"
	"strconv"
)

// applyEnv overlays cfg with GDCM_-prefixed environment variables.
// Legacy variable names (without prefix) are also read for backward compatibility;
// when a legacy variable is found, a deprecation notice is printed to stderr.
// GDCM_-prefixed variables take precedence over legacy names.
func applyEnv(cfg *Config) {
	// --- API config ---

	if v := getEnvWithLegacy("GDCM_API_ADDR", "", "PORT", func(port string) string {
		return ":" + port
	}); v != "" {
		cfg.API.Addr = v
	}

	if v := getEnvWithLegacy("GDCM_API_MONGO_URI", "", "MONGO_URI", nil); v != "" {
		cfg.API.MongoURI = v
	}

	if v := getEnvWithLegacy("GDCM_API_MONGO_DB", "", "MONGO_DB", nil); v != "" {
		cfg.API.MongoDB = v
	}

	if v := getEnvWithLegacy("GDCM_API_JWT_SECRET", "", "JWT_SECRET", nil); v != "" {
		cfg.API.JWTSecret = v
	}

	if v := getEnvWithLegacy("GDCM_API_JWT_EXPIRY_HOURS", "", "JWT_EXPIRY_HOURS", nil); v != "" {
		if h, err := strconv.Atoi(v); err == nil {
			cfg.API.JWTExpiryHours = h
		}
	}

	if v := getEnvWithLegacy("GDCM_API_BOOTSTRAP_ADMIN_USERNAME", "", "BOOTSTRAP_ADMIN_USERNAME", nil); v != "" {
		cfg.API.BootstrapAdminUsername = v
	}

	if v := getEnvWithLegacy("GDCM_API_BOOTSTRAP_ADMIN_PASSWORD", "", "BOOTSTRAP_ADMIN_PASSWORD", nil); v != "" {
		cfg.API.BootstrapAdminPassword = v
	}

	// --- Agent config ---

	if v := os.Getenv("GDCM_AGENT_CONTROLLER_ADDR"); v != "" {
		cfg.Agent.ControllerAddr = v
	}
}

// getEnvWithLegacy reads the canonical GDCM_ key first; if absent, falls back to
// the legacy key and prints a deprecation warning to stderr.
// transform is applied to the legacy value only (e.g. "3000" → ":3000"); pass nil to use as-is.
func getEnvWithLegacy(canonical, _, legacy string, transform func(string) string) string {
	if v := os.Getenv(canonical); v != "" {
		return v
	}
	if legacy != "" {
		if v := os.Getenv(legacy); v != "" {
			fmt.Fprintf(os.Stderr, "warning: environment variable %q is deprecated; use %q instead\n", legacy, canonical)
			if transform != nil {
				return transform(v)
			}
			return v
		}
	}
	return ""
}
