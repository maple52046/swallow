package application

import (
	"context"
	"errors"
	"testing"

	operationdomain "github.com/maple52046/swallow/internal/operation/domain"
)

type stubConfigurations struct {
	operationdomain.AutomationConfigurationRepository
	credential operationdomain.AutomationCredential
	err        error
}

func (s stubConfigurations) Credential(context.Context, string) (operationdomain.AutomationCredential, error) {
	return s.credential, s.err
}

type stubDeploymentKeys struct {
	privateKey string
	ok         bool
	err        error
}

func (s stubDeploymentKeys) DeploymentPrivateKey(context.Context) (string, bool, error) {
	return s.privateKey, s.ok, s.err
}

func (s stubDeploymentKeys) HasDeploymentKey(context.Context) (bool, error) {
	return s.ok, s.err
}

func TestEffectiveAutomationConfigurations_Credential(t *testing.T) {
	deployment := stubDeploymentKeys{privateKey: "DEPLOYMENT", ok: true}
	tests := []struct {
		name     string
		site     stubConfigurations
		keys     stubDeploymentKeys
		wantKey  string
		wantPass string
		wantErr  error
	}{
		{
			name:     "site private key overrides the deployment key",
			site:     stubConfigurations{credential: operationdomain.AutomationCredential{SSHPrivateKey: "SITE", BecomePassword: "pw"}},
			keys:     deployment,
			wantKey:  "SITE",
			wantPass: "pw",
		},
		{
			name:     "site without a private key uses the deployment key and keeps its become password",
			site:     stubConfigurations{credential: operationdomain.AutomationCredential{BecomePassword: "pw"}},
			keys:     deployment,
			wantKey:  "DEPLOYMENT",
			wantPass: "pw",
		},
		{
			name:    "site without any credential record uses the deployment key",
			site:    stubConfigurations{err: operationdomain.ErrAutomationCredentialMissing},
			keys:    deployment,
			wantKey: "DEPLOYMENT",
		},
		{
			name:    "no override and no deployment key is a missing credential",
			site:    stubConfigurations{err: operationdomain.ErrAutomationCredentialMissing},
			keys:    stubDeploymentKeys{},
			wantErr: operationdomain.ErrAutomationCredentialMissing,
		},
		{
			name:    "unconfigured automation stays not found",
			site:    stubConfigurations{err: operationdomain.ErrAutomationConfigNotFound},
			keys:    deployment,
			wantErr: operationdomain.ErrAutomationConfigNotFound,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			repo := NewEffectiveAutomationConfigurations(tc.site, tc.keys)
			got, err := repo.Credential(context.Background(), "site-1")
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("Credential() error = %v, want %v", err, tc.wantErr)
			}
			if got.SSHPrivateKey != tc.wantKey || got.BecomePassword != tc.wantPass {
				t.Errorf("Credential() = %+v, want key %q and become password %q", got, tc.wantKey, tc.wantPass)
			}
		})
	}
}

type stubAutomationRepo struct {
	operationdomain.AutomationConfigurationRepository
	configuration operationdomain.AutomationConfiguration
	stored        *operationdomain.AutomationCredential
}

func (s *stubAutomationRepo) FindBySiteID(context.Context, string) (*operationdomain.AutomationConfiguration, error) {
	copied := s.configuration
	return &copied, nil
}

func (s *stubAutomationRepo) ReplaceCredential(_ context.Context, _ string, credential operationdomain.AutomationCredential) error {
	s.stored = &credential
	return nil
}

func TestAutomationConfigurationService_CredentialSource(t *testing.T) {
	for _, tc := range []struct {
		name     string
		override bool
		keys     DeploymentKeySource
		want     operationdomain.CredentialSource
	}{
		{name: "site override", override: true, keys: stubDeploymentKeys{ok: true}, want: operationdomain.CredentialSourceSite},
		{name: "deployment key", keys: stubDeploymentKeys{ok: true}, want: operationdomain.CredentialSourceDeploymentKey},
		{name: "none", keys: stubDeploymentKeys{}, want: operationdomain.CredentialSourceNone},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &stubAutomationRepo{configuration: operationdomain.AutomationConfiguration{HasPrivateKeyOverride: tc.override}}
			service := NewAutomationConfigurationService(repo, nil, nil)
			service.AttachDeploymentKeys(tc.keys)
			got, err := service.Get(context.Background(), "site-1")
			if err != nil {
				t.Fatalf("Get: %v", err)
			}
			if got.CredentialSource != tc.want {
				t.Errorf("CredentialSource = %q, want %q", got.CredentialSource, tc.want)
			}
		})
	}
}

func TestAutomationConfigurationService_ReplaceCredential(t *testing.T) {
	repo := &stubAutomationRepo{}
	service := NewAutomationConfigurationService(repo, nil, nil)

	if err := service.ReplaceCredential(context.Background(), "site-1",
		operationdomain.AutomationCredential{SSHPrivateKey: "not a key"}); !errors.Is(err, ErrInvalidOperation) {
		t.Errorf("unparseable key error = %v, want ErrInvalidOperation", err)
	}
	if repo.stored != nil {
		t.Errorf("an unparseable key must not be stored")
	}

	if err := service.ReplaceCredential(context.Background(), "site-1",
		operationdomain.AutomationCredential{SSHPrivateKey: "   ", BecomePassword: "pw"}); err != nil {
		t.Fatalf("become-password-only credential: %v", err)
	}
	if repo.stored == nil || repo.stored.SSHPrivateKey != "" || repo.stored.BecomePassword != "pw" {
		t.Errorf("stored credential = %+v, want no key override and the become password", repo.stored)
	}
}
