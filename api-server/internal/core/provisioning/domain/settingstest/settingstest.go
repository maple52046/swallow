// Package settingstest provides in-memory implementations of the provisioner setting stores
// (provisioningdomain.HTTPProxyStore and SubnetDNSStore) for tests of the use cases, handlers, and
// controllers that use them. They follow the store contracts, including conditional adoption and
// generation increments, but are not durable; production code uses the MongoDB stores.
package settingstest

import (
	"context"
	"sort"
	"sync"

	provisioningdomain "github.com/maple52046/swallow/internal/core/provisioning/domain"
)

// HTTPProxyStore is an in-memory provisioningdomain.HTTPProxyStore. StatusWrites counts SetStatus
// calls so a test can assert that a steady setting is not rewritten.
type HTTPProxyStore struct {
	mu           sync.Mutex
	settings     map[string]provisioningdomain.HTTPProxySetting
	StatusWrites int
}

var _ provisioningdomain.HTTPProxyStore = (*HTTPProxyStore)(nil)

// NewHTTPProxyStore returns an empty store.
func NewHTTPProxyStore() *HTTPProxyStore {
	return &HTTPProxyStore{settings: map[string]provisioningdomain.HTTPProxySetting{}}
}

func (s *HTTPProxyStore) Get(_ context.Context, integrationID string) (*provisioningdomain.HTTPProxySetting, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	setting, ok := s.settings[integrationID]
	if !ok {
		return nil, provisioningdomain.ErrSettingNotFound
	}
	return &setting, nil
}

func (s *HTTPProxyStore) List(context.Context) ([]*provisioningdomain.HTTPProxySetting, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*provisioningdomain.HTTPProxySetting, 0, len(s.settings))
	for _, setting := range s.settings {
		setting := setting
		out = append(out, &setting)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].IntegrationID < out[j].IntegrationID })
	return out, nil
}

func (s *HTTPProxyStore) Adopt(_ context.Context, setting *provisioningdomain.HTTPProxySetting) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.settings[setting.IntegrationID]; exists {
		return false, nil
	}
	s.settings[setting.IntegrationID] = *setting
	return true, nil
}

func (s *HTTPProxyStore) PutSpec(_ context.Context, integrationID string, spec provisioningdomain.HTTPProxyConfiguration) (*provisioningdomain.HTTPProxySetting, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	setting := s.settings[integrationID]
	setting.IntegrationID, setting.Spec = integrationID, spec
	setting.Generation++
	s.settings[integrationID] = setting
	return &setting, nil
}

func (s *HTTPProxyStore) SetStatus(_ context.Context, integrationID string, status provisioningdomain.HTTPProxyStatus) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	setting, ok := s.settings[integrationID]
	if !ok {
		return provisioningdomain.ErrSettingNotFound
	}
	setting.Status = status
	s.settings[integrationID] = setting
	s.StatusWrites++
	return nil
}

func (s *HTTPProxyStore) Delete(_ context.Context, integrationID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.settings, integrationID)
	return nil
}

// SubnetDNSStore is an in-memory provisioningdomain.SubnetDNSStore.
type SubnetDNSStore struct {
	mu           sync.Mutex
	settings     map[string]provisioningdomain.SubnetDNSSetting
	StatusWrites int
}

var _ provisioningdomain.SubnetDNSStore = (*SubnetDNSStore)(nil)

// NewSubnetDNSStore returns an empty store.
func NewSubnetDNSStore() *SubnetDNSStore {
	return &SubnetDNSStore{settings: map[string]provisioningdomain.SubnetDNSSetting{}}
}

func key(integrationID, subnetID string) string { return integrationID + "/" + subnetID }

func (s *SubnetDNSStore) Get(_ context.Context, integrationID, subnetID string) (*provisioningdomain.SubnetDNSSetting, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	setting, ok := s.settings[key(integrationID, subnetID)]
	if !ok {
		return nil, provisioningdomain.ErrSettingNotFound
	}
	return &setting, nil
}

func (s *SubnetDNSStore) ListByIntegration(_ context.Context, integrationID string) ([]*provisioningdomain.SubnetDNSSetting, error) {
	return s.list(func(setting provisioningdomain.SubnetDNSSetting) bool { return setting.IntegrationID == integrationID }), nil
}

func (s *SubnetDNSStore) List(context.Context) ([]*provisioningdomain.SubnetDNSSetting, error) {
	return s.list(func(provisioningdomain.SubnetDNSSetting) bool { return true }), nil
}

func (s *SubnetDNSStore) list(keep func(provisioningdomain.SubnetDNSSetting) bool) []*provisioningdomain.SubnetDNSSetting {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []*provisioningdomain.SubnetDNSSetting
	for _, setting := range s.settings {
		if keep(setting) {
			setting := setting
			out = append(out, &setting)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return key(out[i].IntegrationID, out[i].SubnetID) < key(out[j].IntegrationID, out[j].SubnetID)
	})
	return out
}

func (s *SubnetDNSStore) Adopt(_ context.Context, setting *provisioningdomain.SubnetDNSSetting) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := key(setting.IntegrationID, setting.SubnetID)
	if _, exists := s.settings[k]; exists {
		return false, nil
	}
	s.settings[k] = *setting
	return true, nil
}

func (s *SubnetDNSStore) PutSpec(_ context.Context, integrationID, subnetID string, servers []string) (*provisioningdomain.SubnetDNSSetting, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := key(integrationID, subnetID)
	setting := s.settings[k]
	setting.IntegrationID, setting.SubnetID, setting.Servers = integrationID, subnetID, append([]string(nil), servers...)
	setting.Generation++
	s.settings[k] = setting
	return &setting, nil
}

func (s *SubnetDNSStore) SetStatus(_ context.Context, integrationID, subnetID string, status provisioningdomain.SubnetDNSStatus) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := key(integrationID, subnetID)
	setting, ok := s.settings[k]
	if !ok {
		return provisioningdomain.ErrSettingNotFound
	}
	setting.Status = status
	s.settings[k] = setting
	s.StatusWrites++
	return nil
}

func (s *SubnetDNSStore) Delete(_ context.Context, integrationID, subnetID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.settings, key(integrationID, subnetID))
	return nil
}
