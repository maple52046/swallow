package config

import (
	"path/filepath"
	"testing"
	"time"
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

// TestUpdateKeepsOtherProcessesChanges guards Session persistence: Update reads the
// file again, so a refresh token another process saved survives a renewal that did
// not rotate, and environment-only values never reach the file.
func TestUpdateKeepsOtherProcessesChanges(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := Save(path, &Config{Endpoint: "https://file.example", Token: "old", RefreshToken: "newer-from-other-process"}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	t.Setenv(envSite, "env-site")

	expires := time.Date(2026, 10, 2, 3, 0, 0, 0, time.UTC)
	if err := Update(path, func(c *Config) {
		c.Token = "renewed"
		c.TokenExpiresAt = expires
	}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	got, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if got.Token != "renewed" || !got.TokenExpiresAt.Equal(expires) || got.RefreshToken != "newer-from-other-process" {
		t.Errorf("after Update = %+v, want the renewed token and the other process's refresh token", got)
	}
	if got.Site != "" {
		t.Errorf("Site = %q, want the environment value kept out of the file", got.Site)
	}
}

// TestEnvTokenDisablesStoredRefresh confirms a SWALLOW_TOKEN override is never
// "renewed" with the stored Session's refresh token, while SWALLOW_API_KEY is read.
func TestEnvTokenDisablesStoredRefresh(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := Save(path, &Config{Token: "stored", RefreshToken: "stored-refresh", TokenExpiresAt: time.Now()}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	t.Setenv(envToken, "from-env")
	t.Setenv(envAPIKey, "swk_env")

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Token != "from-env" || cfg.RefreshToken != "" || !cfg.TokenExpiresAt.IsZero() {
		t.Errorf("Load = %+v, want the env token without the stored refresh token or expiry", cfg)
	}
	if cfg.APIKey != "swk_env" {
		t.Errorf("APIKey = %q, want SWALLOW_API_KEY", cfg.APIKey)
	}
}
