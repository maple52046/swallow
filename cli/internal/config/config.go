// Package config owns how the swallow operator CLI resolves and persists its
// connection profile: the API endpoint, the credential (a Session's access and
// refresh tokens from `swallow login`, or an API Key), an optional default Site
// scope, an optional machine token for discovery endpoints, and TLS
// verification behavior.
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
	"time"

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
	envAPIKey       = "SWALLOW_API_KEY"
	envSite         = "SWALLOW_SITE"
	envMachineToken = "SWALLOW_MACHINE_TOKEN"
	envInsecure     = "SWALLOW_INSECURE"
)

// Config is the operator's persisted connection profile. Fields are serialized
// to YAML so an operator can inspect and hand-edit the file. Token,
// RefreshToken, APIKey, and MachineToken are credential material: callers must
// not log them, and the file is written with owner-only permissions (see Save).
//
// A profile holds either a Session (Token, RefreshToken, TokenExpiresAt from a
// password login) or an API Key; `swallow login` keeps only one of the two.
// When both are present (for example an API Key from SWALLOW_API_KEY), the API
// Key is used.
type Config struct {
	// Endpoint is the api-server base URL, for example https://swallow.example.
	// It has no default because there is no safe default target.
	Endpoint string `yaml:"endpoint,omitempty"`
	// Token is the Session access token obtained from `swallow login` (or the
	// last refresh). It is opaque to the CLI and short-lived; the client renews
	// it with RefreshToken.
	Token string `yaml:"token,omitempty"`
	// TokenExpiresAt is the accessTokenExpiresAt the server reported for Token,
	// so the client can refresh shortly before expiry. The CLI never decodes the
	// token itself (the contract forbids parsing its claims).
	TokenExpiresAt time.Time `yaml:"tokenExpiresAt,omitempty"`
	// RefreshToken renews Token through POST /api/v1/auth/refresh. Every refresh
	// replaces it, so it must be persisted after each refresh (see Update).
	RefreshToken string `yaml:"refreshToken,omitempty"`
	// APIKey is an API Key secret (`swk_…`) used instead of a Session. It does not
	// expire on its own unless it was created with an expiry, and it cannot be
	// refreshed.
	APIKey string `yaml:"apiKey,omitempty"`
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
	cfg, err := LoadFile(path)
	if err != nil {
		return nil, err
	}
	applyEnv(cfg)
	return cfg, nil
}

// LoadFile reads only the stored profile, without the environment layer. A
// missing file yields an empty Config. Commands use it when they must act on
// what is saved — for example logout revoking the stored Session even when an
// environment variable overrides the credential for this invocation.
func LoadFile(path string) (*Config, error) {
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
		// An access token from the environment belongs to no stored Session, so the
		// stored refresh token must not be used to "renew" it.
		cfg.Token = v
		cfg.RefreshToken = ""
		cfg.TokenExpiresAt = time.Time{}
	}
	if v, ok := os.LookupEnv(envAPIKey); ok {
		cfg.APIKey = v
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

// Update re-reads the profile file at path (without the environment layer),
// applies mutate, and writes it back. It is how the client persists rotated
// Session tokens: reading the file again first keeps changes another `swallow`
// process wrote meanwhile (for example a newer refresh token) instead of
// overwriting them with this process's stale copy, and keeps environment-only
// values out of the file.
func Update(path string, mutate func(*Config)) error {
	cfg, err := LoadFile(path)
	if err != nil {
		return err
	}
	mutate(cfg)
	return Save(path, cfg)
}

// Save writes cfg to path as YAML, creating the parent directory when needed.
// The file is written with mode 0600 and the directory with 0700 because the
// profile carries Session tokens, API Keys, and machine tokens; widening these
// permissions would expose credentials to other local users.
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
