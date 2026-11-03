package config

import (
	"slices"
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
		{name: "http host", baseURL: "http://10.140.168.20"},
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
		{name: "IPv4", server: " 10.140.168.20 "},
		{name: "IPv6", server: "fd00::20"},
		{name: "DNS name", server: "swallow.lab.example"},
		{name: "single label", server: "swallow"},
		{name: "port", server: "10.140.168.20:123", wantErr: true},
		{name: "chrony option", server: "10.140.168.20 iburst", wantErr: true},
		{name: "URL", server: "ntp://10.140.168.20", wantErr: true},
		{name: "mistyped IPv4", server: "10.140.168.256", wantErr: true},
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

// The public URL becomes a Platform Grafana's datasource URL with the gateway path appended, so it
// must be a bare absolute http(s) origin; a trailing slash is dropped.
func TestValidate_PublicURL(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		want    string
		wantErr bool
	}{
		{name: "unset", url: ""},
		{name: "http address", url: " http://10.170.168.20 ", want: "http://10.170.168.20"},
		{name: "https with port and slash", url: "https://swallow.lab:8443/", want: "https://swallow.lab:8443"},
		{name: "path", url: "http://10.170.168.20/swallow", wantErr: true},
		{name: "query", url: "http://10.170.168.20?x=1", wantErr: true},
		{name: "credentials", url: "http://user:pass@10.170.168.20", wantErr: true},
		{name: "scheme", url: "ftp://10.170.168.20", wantErr: true},
		{name: "relative", url: "10.170.168.20", wantErr: true},
		{name: "no host", url: "http://:80", wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := validConfig()
			cfg.API.PublicURL = tc.url
			err := Validate(cfg)
			if (err != nil) != tc.wantErr {
				t.Fatalf("Validate(publicURL=%q) = %v, wantErr %t", tc.url, err, tc.wantErr)
			}
			if err == nil && cfg.API.PublicURL != tc.want {
				t.Errorf("Validate(publicURL=%q) left %q, want %q", tc.url, cfg.API.PublicURL, tc.want)
			}
		})
	}
}

// Scrape addresses go into an exporter's IP allowlist, so each entry must be an IP address; blanks
// and surrounding spaces are dropped.
func TestValidate_MonitoringScrapeAddresses(t *testing.T) {
	tests := []struct {
		name      string
		addresses string
		want      []string
		wantErr   bool
	}{
		{name: "unset", addresses: ""},
		{name: "two", addresses: " 10.170.168.20, 192.168.100.1 ,", want: []string{"10.170.168.20", "192.168.100.1"}},
		{name: "IPv6", addresses: "fd00::20", want: []string{"fd00::20"}},
		{name: "name", addresses: "swallow.lab", wantErr: true},
		{name: "CIDR", addresses: "10.0.0.0/24", wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := validConfig()
			cfg.API.MonitoringScrapeAddresses = tc.addresses
			err := Validate(cfg)
			if (err != nil) != tc.wantErr {
				t.Fatalf("Validate(monitoringScrapeAddresses=%q) = %v, wantErr %t", tc.addresses, err, tc.wantErr)
			}
			if err == nil && !slices.Equal(cfg.API.ScrapeAddresses(), tc.want) {
				t.Errorf("ScrapeAddresses() = %v, want %v", cfg.API.ScrapeAddresses(), tc.want)
			}
		})
	}
}
