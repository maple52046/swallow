package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// applyFile reads the YAML config file at path and merges its values into cfg.
// It returns an error when the path was explicitly provided but cannot be read or parsed,
// because silently ignoring a user-specified config file would mask misconfiguration.
func applyFile(cfg *Config, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("config file %q: %w", path, err)
	}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return fmt.Errorf("config file %q: parse error: %w", path, err)
	}
	return nil
}
