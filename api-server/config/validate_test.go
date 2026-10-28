package config

import (
	"strings"
	"testing"
	"time"
)

func validConfig() *Config {
	cfg := DefaultConfig()
	cfg.API.CredentialKey = "test-key"
	return &cfg
}

func TestValidate_SessionLifetimes(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*APIConfig)
		wantErr string
	}{
		{name: "defaults", mutate: func(*APIConfig) {}},
		{name: "zero access token ttl", mutate: func(a *APIConfig) { a.AccessTokenTTL = 0 }, wantErr: "accessTokenTTL"},
		{name: "zero refresh token ttl", mutate: func(a *APIConfig) { a.RefreshTokenTTL = 0 }, wantErr: "refreshTokenTTL"},
		{name: "zero session max age", mutate: func(a *APIConfig) { a.SessionMaxAge = 0 }, wantErr: "sessionMaxAge"},
		{name: "access outlives refresh", mutate: func(a *APIConfig) { a.AccessTokenTTL = 8 * 24 * time.Hour }, wantErr: "shorter than"},
		{name: "wildcard origin", mutate: func(a *APIConfig) { a.AllowedOrigins = "http://a.example, *" }, wantErr: "explicit origins"},
		{name: "explicit origins", mutate: func(a *APIConfig) { a.AllowedOrigins = "http://a.example,http://b.example" }},
		// The deprecated field no longer has to be positive.
		{name: "deprecated jwt expiry unset", mutate: func(a *APIConfig) { a.JWTExpiryHours = 0 }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := validConfig()
			tc.mutate(&cfg.API)
			err := Validate(cfg)
			switch {
			case tc.wantErr == "" && err != nil:
				t.Errorf("Validate() = %v, want nil", err)
			case tc.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tc.wantErr)):
				t.Errorf("Validate() = %v, want an error mentioning %q", err, tc.wantErr)
			}
		})
	}
}

// Boot Media is optional, but a configured base URL must be one the fixed ISO path can follow.
func TestValidate_BootMedia(t *testing.T) {
	tests := []struct {
		name    string
		baseURL string
		wantErr bool
	}{
		{name: "unset", baseURL: ""},
		{name: "http host", baseURL: "http://10.170.168.20"},
		{name: "trailing slash", baseURL: "http://swallow.lab/"},
		{name: "https with port", baseURL: "https://swallow.lab:8443"},
		{name: "has a path", baseURL: "http://swallow.lab/boot", wantErr: true},
		{name: "has credentials", baseURL: "http://u:p@swallow.lab", wantErr: true},
		{name: "not http", baseURL: "ftp://swallow.lab", wantErr: true},
		{name: "relative", baseURL: "swallow.lab", wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := validConfig()
			cfg.API.BootMedia.BaseURL = tc.baseURL
			if err := Validate(cfg); (err != nil) != tc.wantErr {
				t.Errorf("Validate(baseURL=%q) = %v, wantErr %t", tc.baseURL, err, tc.wantErr)
			}
		})
	}
	cfg := validConfig()
	cfg.API.RedfishProbeInterval = 0
	if err := Validate(cfg); err == nil || !strings.Contains(err.Error(), "redfishProbeInterval") {
		t.Errorf("Validate(zero probe interval) = %v, want an error", err)
	}
}

// The NTP server is optional for startup, but a configured one is written into chrony on every
// provisioned Server, so only one bare IP address or DNS name is accepted.
func TestValidate_NTPServer(t *testing.T) {
	tests := []struct {
		name    string
		server  string
		wantErr bool
	}{
		{name: "unset", server: ""},
		{name: "IPv4", server: " 10.170.168.20 "},
		{name: "IPv6", server: "fd00::20"},
		{name: "DNS name", server: "swallow.lab.example"},
		{name: "single label", server: "swallow"},
		{name: "port", server: "10.170.168.20:123", wantErr: true},
		{name: "chrony option", server: "10.170.168.20 iburst", wantErr: true},
		{name: "URL", server: "ntp://10.170.168.20", wantErr: true},
		{name: "mistyped IPv4", server: "10.170.168.256", wantErr: true},
		{name: "two servers", server: "10.0.0.1,10.0.0.2", wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := validConfig()
			cfg.API.NTPServer = tc.server
			err := Validate(cfg)
			if (err != nil) != tc.wantErr {
				t.Fatalf("Validate(ntpServer=%q) = %v, wantErr %t", tc.server, err, tc.wantErr)
			}
			if err == nil && cfg.API.NTPServer != strings.TrimSpace(tc.server) {
				t.Errorf("Validate(ntpServer=%q) left %q, want it trimmed", tc.server, cfg.API.NTPServer)
			}
		})
	}
}
