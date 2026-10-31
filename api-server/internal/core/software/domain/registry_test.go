package domain

import (
	"errors"
	"testing"
)

func TestNormalizeRegistry(t *testing.T) {
	tests := []struct {
		raw  string
		want string
	}{
		{"harbor.lab.local", "harbor.lab.local"},
		{"  Harbor.Lab.Local:5000/ ", "harbor.lab.local:5000"},
		{"https://registry.example.com", "registry.example.com"},
		{"localhost:5000", "localhost:5000"},
		{"192.168.100.1:5443", "192.168.100.1:5443"},
		{"docker.io", "docker.io"},
		{"index.docker.io", "docker.io"},
		{"https://registry-1.docker.io/", "docker.io"},
		{"hub.docker.com", "docker.io"},
		{"https://hub.docker.com/", "docker.io"},
		{"registry.hub.docker.com", "docker.io"},
		{"hub.docker.io", "docker.io"},
		{"https://index.docker.io/v1/", "docker.io"},
	}
	for _, tc := range tests {
		t.Run(tc.raw, func(t *testing.T) {
			got, err := NormalizeRegistry(tc.raw)
			if err != nil || got != tc.want {
				t.Errorf("NormalizeRegistry(%q) = %q, %v; want %q", tc.raw, got, err, tc.want)
			}
		})
	}
	for _, bad := range []string{"", "harbor.lab.local/team", "harbor:0", "harbor:99999", "har bor", "-harbor"} {
		if _, err := NormalizeRegistry(bad); !errors.Is(err, ErrInvalidRegistryCredential) {
			t.Errorf("NormalizeRegistry(%q) error = %v, want ErrInvalidRegistryCredential", bad, err)
		}
	}
}

// ImageRegistry must agree with Docker's own rule, or a stored credential would be sent to the
// wrong registry (or not at all).
func TestImageRegistry(t *testing.T) {
	tests := []struct {
		name string
		want string
	}{
		{"nginx", "docker.io"},
		{"library/nginx", "docker.io"},
		{"team/app", "docker.io"},
		{"harbor.lab.local/team/app", "harbor.lab.local"},
		{"Harbor.Lab.Local/team/app", "harbor.lab.local"},
		{"192.168.100.1:5443/swallow/hello", "192.168.100.1:5443"},
		{"localhost/app", "localhost"},
		{"localhost:5000/app", "localhost:5000"},
		{"index.docker.io/library/nginx", "docker.io"},
		{"hub.docker.com/team/app", "hub.docker.com"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := ImageRegistry(tc.name); got != tc.want {
				t.Errorf("ImageRegistry(%q) = %q, want %q", tc.name, got, tc.want)
			}
		})
	}
}

func TestRegistryServerAddress(t *testing.T) {
	if got := RegistryServerAddress("docker.io"); got != "https://index.docker.io/v1/" {
		t.Errorf("RegistryServerAddress(docker.io) = %q, want the Docker Hub index address", got)
	}
	if got := RegistryServerAddress("harbor.lab.local:5000"); got != "harbor.lab.local:5000" {
		t.Errorf("RegistryServerAddress(harbor.lab.local:5000) = %q, want the host itself", got)
	}
}
