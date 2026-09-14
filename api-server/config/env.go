package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// applyEnv overlays cfg with SWALLOW_-prefixed environment variables.
// Legacy variable names (without prefix) are also read for backward compatibility;
// when a legacy variable is found, a deprecation notice is printed to stderr.
// SWALLOW_-prefixed variables take precedence over legacy names.
func applyEnv(cfg *Config) {
	if v := os.Getenv("SWALLOW_API_RELEASE_VERSION"); v != "" {
		cfg.API.ReleaseVersion = v
	}

	if v := getEnvWithLegacy("SWALLOW_API_ADDR", "PORT", func(port string) string {
		return ":" + port
	}); v != "" {
		cfg.API.Addr = v
	}

	if v := getSecretEnv("SWALLOW_API_MONGO_URI", "MONGO_URI"); v != "" {
		cfg.API.MongoURI = v
	}

	if v := getEnvWithLegacy("SWALLOW_API_MONGO_DB", "MONGO_DB", nil); v != "" {
		cfg.API.MongoDB = v
	}

	if v := getSecretEnv("SWALLOW_API_JWT_SECRET", "JWT_SECRET"); v != "" {
		cfg.API.JWTSecret = v
	}

	if v := getEnvWithLegacy("SWALLOW_API_JWT_EXPIRY_HOURS", "JWT_EXPIRY_HOURS", nil); v != "" {
		if h, err := strconv.Atoi(v); err == nil {
			cfg.API.JWTExpiryHours = h
		}
	}

	if v := getEnvWithLegacy("SWALLOW_API_BOOTSTRAP_ADMIN_USERNAME", "BOOTSTRAP_ADMIN_USERNAME", nil); v != "" {
		cfg.API.BootstrapAdminUsername = v
	}

	if v := getSecretEnv("SWALLOW_API_BOOTSTRAP_ADMIN_PASSWORD", "BOOTSTRAP_ADMIN_PASSWORD"); v != "" {
		cfg.API.BootstrapAdminPassword = v
	}

	if v := getSecretEnv("SWALLOW_API_CREDENTIAL_KEY", ""); v != "" {
		cfg.API.CredentialKey = v
	}

	if v := os.Getenv("SWALLOW_API_RECONCILE_INTERVAL"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			cfg.API.ReconcileInterval = d
		}
	}

	if v := os.Getenv("SWALLOW_API_INVENTORY_INTERVAL"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			cfg.API.InventoryInterval = d
		}
	}

	if v := os.Getenv("SWALLOW_API_OPERATION_DISPATCH_INTERVAL"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			cfg.API.OperationDispatchInterval = d
		}
	}
	if v := os.Getenv("SWALLOW_API_OPERATION_LEASE_DURATION"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			cfg.API.OperationLeaseDuration = d
		}
	}
	if v := os.Getenv("SWALLOW_API_OPERATION_MAX_PARALLELISM"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.API.OperationMaxParallelism = n
		}
	}
	if v := os.Getenv("SWALLOW_API_TEMPORAL_ADDRESS"); v != "" {
		cfg.API.TemporalAddress = v
	}
	if v := os.Getenv("SWALLOW_API_TEMPORAL_NAMESPACE"); v != "" {
		cfg.API.TemporalNamespace = v
	}
	if v := os.Getenv("SWALLOW_API_TEMPORAL_TASK_QUEUE"); v != "" {
		cfg.API.TemporalTaskQueue = v
	}
	if v := os.Getenv("SWALLOW_API_TEMPORAL_START_INTERVAL"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			cfg.API.TemporalStartInterval = d
		}
	}
	if v := os.Getenv("SWALLOW_API_ANSIBLE_RUNNER_COMMAND"); v != "" {
		cfg.API.AnsibleRunnerCommand = v
	}
	if v := os.Getenv("SWALLOW_API_PLAYBOOK_MANIFEST"); v != "" {
		cfg.API.PlaybookManifest = v
	}
	if v := os.Getenv("SWALLOW_API_PLAYBOOK_DIR"); v != "" {
		cfg.API.PlaybookDir = v
	}
	if v := os.Getenv("SWALLOW_API_JOB_RUNTIME_DIR"); v != "" {
		cfg.API.JobRuntimeDir = v
	}
	if v := os.Getenv("SWALLOW_API_JOB_ARTIFACT_DIR"); v != "" {
		cfg.API.JobArtifactDir = v
	}
	if v := os.Getenv("SWALLOW_API_JOB_ARTIFACT_RETENTION"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			cfg.API.JobArtifactRetention = d
		}
	}
	if v := os.Getenv("SWALLOW_API_ALLOWED_ORIGINS"); v != "" {
		cfg.API.AllowedOrigins = v
	}
	if v := os.Getenv("SWALLOW_API_IMAGE_UPLOAD_MAX_BYTES"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			cfg.API.ImageUploadMaxBytes = n
		}
	}
	if v := getSecretEnv("SWALLOW_API_MACHINE_TOKEN", ""); v != "" {
		cfg.API.MachineToken = v
	}
}

// getSecretEnv supports Compose secrets and root-only systemd credential files.
// A direct value takes precedence over the corresponding *_FILE path.
func getSecretEnv(canonical, legacy string) string {
	if value := getEnvWithLegacy(canonical, legacy, nil); value != "" {
		return value
	}
	path := os.Getenv(canonical + "_FILE")
	if path == "" {
		return ""
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: cannot read %s: %v\n", canonical+"_FILE", err)
		return ""
	}
	return strings.TrimSpace(string(raw))
}

// getEnvWithLegacy reads the canonical SWALLOW_ key first; if absent, falls back to
// the legacy key and prints a deprecation warning to stderr.
// transform is applied to the legacy value only (e.g. "3000" → ":3000"); pass nil to use as-is.
func getEnvWithLegacy(canonical, legacy string, transform func(string) string) string {
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
