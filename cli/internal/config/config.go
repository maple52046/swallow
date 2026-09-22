// Package config owns how the swallow operator CLI resolves and persists its
// connection profile: the API endpoint, the interactive access token, an
// optional default Site scope, an optional machine token for discovery
// endpoints, and TLS verification behavior.
//
// Component boundary: this package is part of the `cli` component, an HTTP
// consumer of the api-server contract. It stores only client-side session
// material chosen by the operator; it never encodes api-server internals or
// domain models. The only durable state the CLI keeps is this profile file.
//
// Precedence: values are resolved lowest-to-highest as file, then process
// environment (SWALLOW_*), then explicit command-line flags. Load applies the
// file and environment layers; the command layer overlays flag values on top so
// an operator can override a saved profile for a single invocation without
// rewriting the file.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// EnvConfigPath overrides the profile file location. It takes precedence over
// the default path but not over an explicit --config flag handled by the caller.
const EnvConfigPath = "SWALLOW_CONFIG"

// Environment variables that override individual profile fields. They exist so
// the CLI works in CI and container contexts where writing a profile file is
// undesirable; a set variable always wins over the stored file value.
const (
	envEndpoint     = "SWALLOW_ENDPOINT"
	envToken        = "SWALLOW_TOKEN"
	envSite         = "SWALLOW_SITE"
	envMachineToken = "SWALLOW_MACHINE_TOKEN"
	envInsecure     = "SWALLOW_INSECURE"
)

// Config is the operator's persisted connection profile. Fields are serialized
// to YAML so an operator can inspect and hand-edit the file. Token and
// MachineToken are credential material: callers must not log them, and the file
// is written with owner-only permissions (see Save).
type Config struct {
	// Endpoint is the api-server base URL, for example https://swallow.example.
	// It has no default because there is no safe default target.
	Endpoint string `yaml:"endpoint,omitempty"`
	// Token is the interactive JWT access token obtained from `swallow login`.
	// It is opaque to the CLI, which re-authenticates on a 401 rather than
	// predicting expiry.
	Token string `yaml:"token,omitempty"`
	// Site is an optional default Site scope applied by commands that accept a
	// siteId query parameter when the operator does not pass --site explicitly.
	Site string `yaml:"site,omitempty"`
	// MachineToken is the static bearer token used only by machine-authenticated
	// endpoints (for example discovery). It is distinct from Token because those
	// endpoints accept a machine token or an admin JWT, not an ordinary session.
	MachineToken string `yaml:"machineToken,omitempty"`
	// InsecureSkipTLS disables TLS certificate verification. It exists for lab
	// endpoints using self-signed certificates and must stay false in production.
	InsecureSkipTLS bool `yaml:"insecureSkipTls,omitempty"`
}

// DefaultPath returns the profile file location honored when neither an explicit
// --config flag nor SWALLOW_CONFIG is provided. It resolves to
// <user-config-dir>/swallow/config.yaml (on Linux, ~/.config/swallow/config.yaml).
func DefaultPath() (string, error) {
	if p := os.Getenv(EnvConfigPath); p != "" {
		return p, nil
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolve user config dir: %w", err)
	}
	return filepath.Join(dir, "swallow", "config.yaml"), nil
}

// Load reads the profile from path and overlays the SWALLOW_* environment
// layer. A missing file is not an error: it yields an empty Config so the
// command layer can still apply flags (for example on first `swallow login`).
// A malformed file is an error, because silently ignoring it would hide a
// misconfigured profile from the operator.
func Load(path string) (*Config, error) {
	cfg := &Config{}
	data, err := os.ReadFile(path)
	switch {
	case err == nil:
		if uerr := yaml.Unmarshal(data, cfg); uerr != nil {
			return nil, fmt.Errorf("parse config %s: %w", path, uerr)
		}
	case errors.Is(err, os.ErrNotExist):
		// No stored profile yet; env and flags may still fully configure the CLI.
	default:
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}

	applyEnv(cfg)
	return cfg, nil
}

// applyEnv overlays process environment variables onto cfg. A set variable
// always overrides the file value; an unset variable leaves the file value
// untouched so partial environments do not erase stored fields.
func applyEnv(cfg *Config) {
	if v, ok := os.LookupEnv(envEndpoint); ok {
		cfg.Endpoint = v
	}
	if v, ok := os.LookupEnv(envToken); ok {
		cfg.Token = v
	}
	if v, ok := os.LookupEnv(envSite); ok {
		cfg.Site = v
	}
	if v, ok := os.LookupEnv(envMachineToken); ok {
		cfg.MachineToken = v
	}
	if v, ok := os.LookupEnv(envInsecure); ok {
		// Any non-empty value other than an explicit falsey token enables it, so
		// `SWALLOW_INSECURE=1` and `SWALLOW_INSECURE=true` both work.
		cfg.InsecureSkipTLS = v != "" && v != "0" && v != "false"
	}
}

// Save writes cfg to path as YAML, creating the parent directory when needed.
// The file is written with mode 0600 and the directory with 0700 because the
// profile carries access and machine tokens; widening these permissions would
// expose credentials to other local users.
func Save(path string, cfg *Config) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create config dir %s: %w", dir, err)
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("encode config: %w", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write config %s: %w", path, err)
	}
	return nil
}
