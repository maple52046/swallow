package domain

import (
	"context"
	"errors"
	"time"

	"github.com/maple52046/swallow/internal/core/convergence"
)

// This file holds the provisioner settings swallow owns as desired state (decision 071): a
// provisioner Integration's HTTP Proxy and each provisioner subnet's DNS servers. swallow stores
// the spec, a controller writes it to the provisioner and reads it back, and drift made in the
// provisioner is corrected at the periodic resync. The first observation of a setting is adopted
// as its spec without writing to the provisioner.

// ErrSettingNotFound means swallow holds no setting for the Integration (or subnet) yet; it is
// adopted from the provisioner at first observation.
var ErrSettingNotFound = errors.New("provisioner setting not found")

// HTTPProxySetting is one provisioner Integration's Provisioner HTTP Proxy as a migrated resource.
// Spec is normalized (NormalizeHTTPProxy). Status is written by the HTTP Proxy controller, except
// that adoption writes the first one.
type HTTPProxySetting struct {
	IntegrationID string
	Generation    int64
	Spec          HTTPProxyConfiguration
	Status        HTTPProxyStatus
}

// HTTPProxyStatus is the controller's report: the generation realized, its conditions, and what
// the provisioner held when last read (nil before the first read).
type HTTPProxyStatus struct {
	ObservedGeneration int64
	Conditions         []convergence.Condition
	Observed           *HTTPProxyConfiguration
	ObservedAt         *time.Time
}

// HTTPProxyStore persists HTTP Proxy settings, keyed by provisioner Integration id.
//
// Implementations must be safe for concurrent use. Get returns ErrSettingNotFound for an
// Integration without one. Adopt inserts setting only when none exists and reports whether it did,
// so adoption never replaces a spec the API wrote. PutSpec replaces the spec and increments the
// generation, creating the setting at generation 1 when there is none, and returns what it
// stored. SetStatus changes nothing but the status; Delete of a missing setting is not an error.
type HTTPProxyStore interface {
	Get(ctx context.Context, integrationID string) (*HTTPProxySetting, error)
	List(ctx context.Context) ([]*HTTPProxySetting, error)
	Adopt(ctx context.Context, setting *HTTPProxySetting) (bool, error)
	PutSpec(ctx context.Context, integrationID string, spec HTTPProxyConfiguration) (*HTTPProxySetting, error)
	SetStatus(ctx context.Context, integrationID string, status HTTPProxyStatus) error
	Delete(ctx context.Context, integrationID string) error
}

// SubnetDNSSetting is one provisioner subnet's Subnet DNS as a migrated resource. Servers is the
// normalized spec (NormalizeSubnetDNSServers; nil means none). The subnet itself belongs to the
// provisioner: when it disappears there, its setting is removed.
type SubnetDNSSetting struct {
	IntegrationID string
	SubnetID      string
	Generation    int64
	Servers       []string
	Status        SubnetDNSStatus
}

// SubnetDNSStatus is the controller's report: the generation realized, its conditions, and the
// servers the provisioner held when last read.
type SubnetDNSStatus struct {
	ObservedGeneration int64
	Conditions         []convergence.Condition
	Observed           []string
	ObservedAt         *time.Time
}

// SubnetDNSStore persists Subnet DNS settings, keyed by provisioner Integration and provider
// subnet id, with the same contract as HTTPProxyStore.
type SubnetDNSStore interface {
	Get(ctx context.Context, integrationID, subnetID string) (*SubnetDNSSetting, error)
	ListByIntegration(ctx context.Context, integrationID string) ([]*SubnetDNSSetting, error)
	List(ctx context.Context) ([]*SubnetDNSSetting, error)
	Adopt(ctx context.Context, setting *SubnetDNSSetting) (bool, error)
	PutSpec(ctx context.Context, integrationID, subnetID string, servers []string) (*SubnetDNSSetting, error)
	SetStatus(ctx context.Context, integrationID, subnetID string, status SubnetDNSStatus) error
	Delete(ctx context.Context, integrationID, subnetID string) error
}

// ProvisionerIntegrations lists the enabled provisioner Integrations, which the provisioner
// setting controllers converge.
type ProvisionerIntegrations interface {
	ProvisionerIDs(ctx context.Context) ([]string, error)
}
