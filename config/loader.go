package config

// LoadOptions controls how the final Config is assembled.
type LoadOptions struct {
	// ConfigFile is the path to a YAML config file. An empty string means no
	// file is loaded and the program runs on defaults + flags + env vars only.
	ConfigFile string
	// FlagOverlay contains CLI-flag values the user explicitly set.
	// Only non-zero fields are applied; nil means no flag overlay.
	// This prevents zero-value CLI flags from overwriting config file values.
	FlagOverlay *APIConfig
	// Role is "api" or "agent" and determines which validation rules apply.
	Role string
}

// Load assembles the runtime Config in strict priority order:
//  1. DefaultConfig (lowest priority)
//  2. Config file (if ConfigFile is set)
//  3. CLI flags (FlagOverlay; only explicitly-set fields)
//  4. Environment variables (GDCM_-prefixed; highest priority)
//  5. Role-specific validation
//
// If ConfigFile is non-empty but cannot be read or parsed, Load returns an
// error immediately so that misconfiguration is never silently ignored.
func Load(opts LoadOptions) (*Config, error) {
	cfg := DefaultConfig()

	if opts.ConfigFile != "" {
		if err := applyFile(&cfg, opts.ConfigFile); err != nil {
			return nil, err
		}
	}

	if opts.FlagOverlay != nil {
		applyAPIFlagOverlay(&cfg, opts.FlagOverlay)
	}

	applyEnv(&cfg)

	if err := Validate(&cfg, opts.Role); err != nil {
		return nil, err
	}

	return &cfg, nil
}

// applyAPIFlagOverlay copies non-zero fields from overlay into cfg.API.
// Zero-value fields are skipped so that unset flags do not erase config file values.
func applyAPIFlagOverlay(cfg *Config, overlay *APIConfig) {
	if overlay.Addr != "" {
		cfg.API.Addr = overlay.Addr
	}
	if overlay.MongoURI != "" {
		cfg.API.MongoURI = overlay.MongoURI
	}
	if overlay.MongoDB != "" {
		cfg.API.MongoDB = overlay.MongoDB
	}
	if overlay.JWTSecret != "" {
		cfg.API.JWTSecret = overlay.JWTSecret
	}
	if overlay.JWTExpiryHours != 0 {
		cfg.API.JWTExpiryHours = overlay.JWTExpiryHours
	}
	if overlay.BootstrapAdminUsername != "" {
		cfg.API.BootstrapAdminUsername = overlay.BootstrapAdminUsername
	}
	if overlay.BootstrapAdminPassword != "" {
		cfg.API.BootstrapAdminPassword = overlay.BootstrapAdminPassword
	}
}
