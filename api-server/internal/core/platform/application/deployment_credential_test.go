package application

import (
	"context"
	"errors"
	"testing"

	platformdomain "github.com/maple52046/swallow/internal/core/platform/domain"
	sitedomain "github.com/maple52046/swallow/internal/core/site/domain"
	"github.com/maple52046/swallow/internal/core/store/storetest"
)

// credentialIntegrations stores Integrations in memory with their credential.
type credentialIntegrations struct {
	sitedomain.IntegrationRepository
	items       map[string]*sitedomain.Integration
	credentials map[string]string
}

func newCredentialIntegrations() *credentialIntegrations {
	return &credentialIntegrations{items: map[string]*sitedomain.Integration{}, credentials: map[string]string{}}
}

func (r *credentialIntegrations) Create(_ context.Context, integration *sitedomain.Integration, credential string) error {
	r.items[integration.ID] = integration
	r.credentials[integration.ID] = credential
	return nil
}

func (r *credentialIntegrations) FindByID(_ context.Context, id string) (*sitedomain.Integration, error) {
	if integration, ok := r.items[id]; ok {
		return integration, nil
	}
	return nil, sitedomain.ErrIntegrationNotFound
}

func (r *credentialIntegrations) Credential(_ context.Context, id string) (string, error) {
	return r.credentials[id], nil
}

// unreachableReaders fails every read, as a cluster that is not up yet would.
type unreachableReaders struct{}

func (unreachableReaders) For(context.Context, *platformdomain.Platform) (platformdomain.PlatformReader, error) {
	return nil, errors.New("not reachable")
}

// A deployment that returned its cluster CA is recorded with that CA and without
// insecureSkipVerify, so the API certificate is verified (DF-01); one that returned none keeps the
// earlier unverified Integration.
func TestRecordVerifiesTheClusterCAWhenTheDeploymentReturnsOne(t *testing.T) {
	const ca = "-----BEGIN CERTIFICATE-----\nMIIB\n-----END CERTIFICATE-----"
	for name, tc := range map[string]struct {
		ca           string
		wantCA       string
		wantInsecure bool
	}{
		"with a CA":    {ca: ca, wantCA: ca + "\n"},
		"without a CA": {ca: "", wantInsecure: true},
	} {
		t.Run(name, func(t *testing.T) {
			platforms := &deployFakePlatformRepo{platforms: map[string]*platformdomain.Platform{
				"p1": {ID: "p1", SiteID: "site-1", Name: "lab", Type: platformdomain.PlatformTypeKubernetes},
			}}
			integrations := newCredentialIntegrations()
			service := NewDeploymentCredentialService(&storetest.Transactor{}, platforms, integrations,
				NewMembershipSyncUseCase(platforms, nil, unreachableReaders{}))

			err := service.Record(context.Background(), "p1", DeploymentCredential{
				APIEndpoint: "https://10.0.0.1:6443", Token: "token", CACertificate: tc.ca,
			})
			if err != nil {
				t.Fatalf("Record: %v", err)
			}
			integration, err := integrations.FindByID(context.Background(), platforms.platforms["p1"].IntegrationID)
			if err != nil {
				t.Fatalf("recorded Integration: %v", err)
			}
			if got := integration.Setting(platformdomain.SettingCACertificate, ""); got != tc.wantCA {
				t.Errorf("caCertificate = %q, want %q", got, tc.wantCA)
			}
			if got := integration.SettingBool(platformdomain.SettingInsecureSkipVerify); got != tc.wantInsecure {
				t.Errorf("insecureSkipVerify = %v, want %v", got, tc.wantInsecure)
			}
		})
	}
}
