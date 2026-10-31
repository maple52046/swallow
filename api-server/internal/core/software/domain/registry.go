package domain

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// This file defines Registry Credentials (decision 044): swallow-owned, installation-wide
// credentials the Docker Host Explorer attaches to a pull whose image reference resolves to their
// registry. The password is never part of the entity; it crosses the repository boundary only when
// written and when resolved for one pull.

// DockerHubRegistry is the normalized name of Docker Hub, the registry of every reference without
// an explicit registry host.
const DockerHubRegistry = "docker.io"

// dockerHubServerAddress is the server address the Engine expects in X-Registry-Auth for Docker Hub;
// any other registry is addressed by its host.
const dockerHubServerAddress = "https://index.docker.io/v1/"

// dockerHubAliases are the names Docker itself treats as Docker Hub. ImageRegistry folds only these,
// so a reference resolves exactly as the Engine resolves it.
var dockerHubAliases = map[string]bool{
	"docker.io":            true,
	"index.docker.io":      true,
	"registry-1.docker.io": true,
}

// dockerHubCredentialNames are further names operators type for Docker Hub when saving a credential
// (the website, the legacy registry host, and the common misspelling of it). They are folded only by
// NormalizeRegistry: no image reference uses them, so keeping them as separate registries would store
// a credential that never matches a Docker Hub pull.
var dockerHubCredentialNames = map[string]bool{
	"hub.docker.com":          true,
	"registry.hub.docker.com": true,
	"hub.docker.io":           true,
}

// registryHost is a host or host:port with no scheme or path.
var registryHost = regexp.MustCompile(`^[a-z0-9]([a-z0-9.-]*[a-z0-9])?(:[0-9]{1,5})?$`)

// RegistryCredential is the stored identity for one registry. The password is deliberately absent:
// reads never decode it, and only RegistryCredentialRepository.FindAuth returns it, for one pull.
type RegistryCredential struct {
	ID string
	// Registry is the normalized host (see NormalizeRegistry); unique across credentials.
	Registry  string
	Username  string
	CreatedAt time.Time
	UpdatedAt time.Time
	// UpdatedBy is the username of the operator who last created or replaced the credential.
	UpdatedBy string
}

// DockerRegistryAuth is the credential the Engine needs for one pull. It lives only for the request
// and must never be logged or included in an error.
type DockerRegistryAuth struct {
	Username      string
	Password      string
	ServerAddress string
}

// NormalizeRegistry turns operator input into the stored registry key: trimmed, lower-cased, with a
// leading http(s):// and trailing slash removed, and every Docker Hub name — the Engine's aliases and
// the names operators commonly type (hub.docker.com, registry.hub.docker.com, hub.docker.io, the
// index URL https://index.docker.io/v1/) — folded into DockerHubRegistry. Anything else that is not
// host or host:port (a path, a bad port) is ErrInvalidRegistryCredential, so lookups by reference
// can match exactly.
func NormalizeRegistry(raw string) (string, error) {
	value := strings.ToLower(strings.TrimSpace(raw))
	value = strings.TrimPrefix(value, "https://")
	value = strings.TrimPrefix(value, "http://")
	value = strings.TrimSuffix(value, "/")
	if value == "index.docker.io/v1" || dockerHubAliases[value] || dockerHubCredentialNames[value] {
		return DockerHubRegistry, nil
	}
	if !registryHost.MatchString(value) {
		return "", fmt.Errorf("%w: registry %q must be a host or host:port without a path", ErrInvalidRegistryCredential, raw)
	}
	if _, port, ok := strings.Cut(value, ":"); ok {
		if number, err := strconv.Atoi(port); err != nil || number < 1 || number > 65535 {
			return "", fmt.Errorf("%w: registry %q has an invalid port", ErrInvalidRegistryCredential, raw)
		}
	}
	return value, nil
}

// ImageRegistry returns the normalized registry an image name (without tag or digest) is pulled
// from, using Docker's rule: the first path component is a registry only when it contains "." or
// ":" or is "localhost"; everything else, including bare names like "nginx", is Docker Hub.
func ImageRegistry(name string) string {
	first, _, hasPath := strings.Cut(name, "/")
	if !hasPath || (!strings.ContainsAny(first, ".:") && first != "localhost") {
		return DockerHubRegistry
	}
	registry := strings.ToLower(first)
	if dockerHubAliases[registry] {
		return DockerHubRegistry
	}
	return registry
}

// RegistryServerAddress is the server address to put in X-Registry-Auth for a normalized registry.
func RegistryServerAddress(registry string) string {
	if registry == DockerHubRegistry {
		return dockerHubServerAddress
	}
	return registry
}

// RegistryCredentialRepository persists Registry Credentials with their password sealed.
//
// Implementations must enforce registry uniqueness atomically (a duplicate is
// ErrRegistryCredentialExists), must seal the password before it reaches storage, and must keep it
// out of every read except FindAuth. A missing record is ErrRegistryCredentialNotFound. Production
// implementations are durable; in-memory ones are for tests.
type RegistryCredentialRepository interface {
	// List returns every credential ordered by registry, without passwords.
	List(ctx context.Context) ([]*RegistryCredential, error)
	FindByID(ctx context.Context, id string) (*RegistryCredential, error)
	// FindAuth returns the credential for a normalized registry together with its unsealed
	// password, for exactly one pull.
	FindAuth(ctx context.Context, registry string) (*RegistryCredential, string, error)
	Create(ctx context.Context, credential *RegistryCredential, password string) error
	// Replace overwrites the username, password, and update stamp of an existing credential.
	Replace(ctx context.Context, credential *RegistryCredential, password string) error
	Delete(ctx context.Context, id string) error
}

var (
	// ErrRegistryCredentialNotFound means no credential has the requested id or registry.
	ErrRegistryCredentialNotFound = errors.New("registry credential not found")
	// ErrRegistryCredentialExists means a credential for the normalized registry already exists.
	ErrRegistryCredentialExists = errors.New("a credential for this registry already exists")
	// ErrInvalidRegistryCredential is a request-shape failure the delivery layer maps to 400.
	ErrInvalidRegistryCredential = errors.New("invalid registry credential")
)
