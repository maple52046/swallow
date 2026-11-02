// Package domain defines Site and Integration: the only part of the world swallow
// defines rather than observes.
//
// No external system knows the set of sites, and none knows which other systems swallow
// should talk to. Everything else in swallow is a projection of, or a reference
// into, one of the integrations registered here.
package domain

import (
	"errors"
	"time"
)

// Site is a location that owns its own infrastructure: a datacenter, a colocation
// cage, a lab.
//
// A site is deliberately thin — an identifier and a name, not a model of a building.
// Anything more specific about a location is an attribute of the things inside it.
type Site struct {
	ID          string
	Name        string
	Description string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// IntegrationKind is the role an external system plays for swallow.
type IntegrationKind string

const (
	// IntegrationKindProvisioner enumerates machines and installs operating systems.
	IntegrationKindProvisioner IntegrationKind = "provisioner"
	// IntegrationKindAutomation is retained only to decode pre-v2 development data.
	// Deprecated: automation is an owned site configuration, not an integration.
	IntegrationKindAutomation IntegrationKind = "automation"
	// IntegrationKindMetrics answers metric queries.
	IntegrationKindMetrics IntegrationKind = "metrics"
	// IntegrationKindPlatform reports live platform state and membership.
	IntegrationKindPlatform IntegrationKind = "platform"
)

// ValidIntegrationKinds lists every kind, for validation and for telling a user
// what they may have meant.
var ValidIntegrationKinds = []IntegrationKind{
	IntegrationKindProvisioner,
	IntegrationKindMetrics,
	IntegrationKindPlatform,
}

func (k IntegrationKind) Valid() bool {
	for _, valid := range ValidIntegrationKinds {
		if k == valid {
			return true
		}
	}
	return false
}

// IntegrationProviders says which provider kinds may play each Integration kind at this
// installation. A provider kind selects an adapter (or, for a platform Integration, a Platform
// module) and is persisted in server sources, so its string stays stable; the adapters and modules
// own those strings and the composition root collects them, so the core lists no product
// (core-and-extensions.md §3.5).
//
// Validating the pair rather than each half independently catches the mistake that actually
// happens: registering a provisioner's endpoint as a metrics backend, which would otherwise fail
// much later with a confusing protocol error.
type IntegrationProviders map[IntegrationKind][]string

// Kinds returns the provider kinds valid for an integration kind.
func (p IntegrationProviders) Kinds(kind IntegrationKind) []string {
	return append([]string(nil), p[kind]...)
}

// Allows reports whether providerKind can implement kind.
func (p IntegrationProviders) Allows(kind IntegrationKind, providerKind string) bool {
	for _, valid := range p[kind] {
		if providerKind == valid {
			return true
		}
	}
	return false
}

// Integration is a registered external system, scoped to one site.
//
// It carries no credential field. A credential is written through
// IntegrationRepository and read back only by an explicit Credential call, so that no
// serialization path can leak one by accident.
type Integration struct {
	// ID is persisted in every server source, so it must never be reissued to
	// refer to a different system.
	ID     string
	SiteID string
	Kind   IntegrationKind
	// ProviderKind selects the adapter, e.g. "maas" for a provisioner.
	ProviderKind string
	Name         string
	Endpoint     string
	// Enabled allows an integration to be registered but paused, for instance
	// during a maintenance window, without losing its configuration.
	Enabled bool
	// Settings holds adapter-specific non-secret options, e.g. TLS verification.
	// Kept as a string map so that adding an adapter option is not a schema change.
	Settings map[string]string
	Sync     SyncState

	CreatedAt time.Time
	UpdatedAt time.Time
}

// SyncState records the freshness of whatever this integration feeds. It is part of
// the API rather than an implementation detail: a reader must always be able to tell
// "last synced 14 minutes ago" from "up to date".
type SyncState struct {
	LastStartedAt   *time.Time
	LastSucceededAt *time.Time
	// LastError is the reason the most recent attempt failed, empty when the most
	// recent attempt succeeded.
	LastError string
}

// Setting returns a settings value, or fallback when unset.
func (i *Integration) Setting(key, fallback string) string {
	if v, ok := i.Settings[key]; ok && v != "" {
		return v
	}
	return fallback
}

// SettingBool reads a boolean setting. Anything other than "true" is false, because a
// misspelled value must not silently enable something.
func (i *Integration) SettingBool(key string) bool {
	return i.Settings[key] == "true"
}

var (
	ErrSiteNotFound        = errors.New("site not found")
	ErrSiteNameTaken       = errors.New("site name already exists")
	ErrIntegrationNotFound = errors.New("integration not found")
	// ErrIntegrationNameTaken means another Integration of the same Site has the name; names
	// are unique within a Site.
	ErrIntegrationNameTaken = errors.New("integration name already exists in this site")
	// ErrSiteHasIntegrations prevents deleting a site out from under the
	// integrations and servers that reference it.
	ErrSiteHasIntegrations = errors.New("site still has integrations")
	// ErrIntegrationHasServers prevents deleting an integration that servers were
	// projected from, which would orphan them.
	ErrIntegrationHasServers = errors.New("integration still has servers")
	// ErrIntegrationHasDeploymentTemplates prevents deleting template ownership.
	ErrIntegrationHasDeploymentTemplates = errors.New("integration still has deployment templates")
	// ErrIntegrationHasBootISOs prevents deleting a provisioner whose Boot ISOs chain to it: their
	// files and records would outlive the Integration they boot into.
	ErrIntegrationHasBootISOs = errors.New("integration still has boot ISOs")
	// ErrIntegrationHasPlatforms prevents deleting an Integration a Platform reads its live state
	// through, which would leave the Platform unable to report membership.
	ErrIntegrationHasPlatforms = errors.New("a platform still reads through this integration")
	// ErrCredentialNotSet means the integration has no stored credential, which for
	// most adapters makes it unusable.
	ErrCredentialNotSet = errors.New("integration has no credential")
)
