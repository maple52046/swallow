package config

import (
	"path/filepath"
	"testing"
)

// TestLoadMissingFileIsEmpty confirms a missing profile is not an error so a
// first-run `login` can still proceed with flags.
func TestLoadMissingFileIsEmpty(t *testing.T) {
	cfg, err := Load(filepath.Join(t.TempDir(), "absent.yaml"))
	if err != nil {
		t.Fatalf("Load missing: %v", err)
	}
	if cfg.Endpoint != "" || cfg.Token != "" {
		t.Errorf("expected empty config, got %+v", cfg)
	}
}

// TestSaveLoadRoundTrip verifies persistence preserves every field.
func TestSaveLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "config.yaml")
	want := &Config{Endpoint: "https://swallow.example", Token: "tok", Site: "site-1", MachineToken: "m", InsecureSkipTLS: true}
	if err := Save(path, want); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if *got != *want {
		t.Errorf("round trip mismatch: got %+v want %+v", got, want)
	}
}

// TestEnvOverridesFile confirms SWALLOW_* variables win over the stored file.
func TestEnvOverridesFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := Save(path, &Config{Endpoint: "https://file.example", Token: "file-token"}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	t.Setenv(envEndpoint, "https://env.example")
	t.Setenv(envInsecure, "1")

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Endpoint != "https://env.example" {
		t.Errorf("endpoint = %q, want env override", cfg.Endpoint)
	}
	if cfg.Token != "file-token" {
		t.Errorf("token = %q, want file value preserved", cfg.Token)
	}
	if !cfg.InsecureSkipTLS {
		t.Error("SWALLOW_INSECURE=1 should enable InsecureSkipTLS")
	}
}
