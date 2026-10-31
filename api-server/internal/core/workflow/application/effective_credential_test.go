package application

import (
	"context"
	"errors"
	"testing"

	workflowdomain "github.com/maple52046/swallow/internal/core/workflow/domain"
)

type stubConfigurations struct {
	workflowdomain.AutomationConfigurationRepository
	configuration *workflowdomain.AutomationConfiguration
	credential    workflowdomain.AutomationCredential
	err           error
}

func (s stubConfigurations) FindBySiteID(context.Context, string) (*workflowdomain.AutomationConfiguration, error) {
	if s.err != nil {
		return nil, s.err
	}
	copied := *s.configuration
	return &copied, nil
}

func (s stubConfigurations) Credential(context.Context, string) (workflowdomain.AutomationCredential, error) {
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

// TestEffectiveAutomationConfigurations_Credential covers decision 041: every Site's run uses
// the Deployment Key, even if the repository were to hand back a key, and keeps the Site's
// become password.
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
			name:     "a stored key never replaces the deployment key",
			site:     stubConfigurations{credential: workflowdomain.AutomationCredential{SSHPrivateKey: "SITE", BecomePassword: "pw"}},
			keys:     deployment,
			wantKey:  "DEPLOYMENT",
			wantPass: "pw",
		},
		{
			name:     "site with a become password uses the deployment key and keeps the password",
			site:     stubConfigurations{credential: workflowdomain.AutomationCredential{BecomePassword: "pw"}},
			keys:     deployment,
			wantKey:  "DEPLOYMENT",
			wantPass: "pw",
		},
		{
			name:    "site without any credential record uses the deployment key",
			site:    stubConfigurations{err: workflowdomain.ErrAutomationCredentialMissing},
			keys:    deployment,
			wantKey: "DEPLOYMENT",
		},
		{
			name:    "no deployment key is a missing credential",
			site:    stubConfigurations{err: workflowdomain.ErrAutomationCredentialMissing},
			keys:    stubDeploymentKeys{},
			wantErr: workflowdomain.ErrAutomationCredentialMissing,
		},
		{
			name:    "unconfigured automation stays not found",
			site:    stubConfigurations{err: workflowdomain.ErrAutomationConfigNotFound},
			keys:    deployment,
			wantErr: workflowdomain.ErrAutomationConfigNotFound,
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
				t.Errorf("Credential() = key %q, become password %q; want key %q, become password %q",
					got.SSHPrivateKey, got.BecomePassword, tc.wantKey, tc.wantPass)
			}
		})
	}
}

// TestEffectiveAutomationConfigurations_FindBySiteIDGatesOnDeploymentKey is the regression test
// for the stale gate: a Site that never stored a credential record is runnable as soon as the
// Deployment Key exists, and is not runnable without it.
func TestEffectiveAutomationConfigurations_FindBySiteIDGatesOnDeploymentKey(t *testing.T) {
	tests := []struct {
		name       string
		enabled    bool
		keys       stubDeploymentKeys
		wantSource workflowdomain.CredentialSource
		wantErr    error
	}{
		{name: "enabled with deployment key runs", enabled: true, keys: stubDeploymentKeys{ok: true},
			wantSource: workflowdomain.CredentialSourceDeploymentKey},
		{name: "enabled without deployment key is missing a credential", enabled: true,
			wantSource: workflowdomain.CredentialSourceNone, wantErr: workflowdomain.ErrAutomationCredentialMissing},
		{name: "disabled automation is disabled", keys: stubDeploymentKeys{ok: true},
			wantSource: workflowdomain.CredentialSourceDeploymentKey, wantErr: workflowdomain.ErrAutomationDisabled},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			site := stubConfigurations{configuration: &workflowdomain.AutomationConfiguration{SiteID: "site-1", Enabled: tc.enabled}}
			got, err := NewEffectiveAutomationConfigurations(site, tc.keys).FindBySiteID(context.Background(), "site-1")
			if err != nil {
				t.Fatalf("FindBySiteID: %v", err)
			}
			if got.HasCredential {
				t.Errorf("HasCredential = true, want false: the Site never stored a become password")
			}
			if got.CredentialSource != tc.wantSource {
				t.Errorf("CredentialSource = %q, want %q", got.CredentialSource, tc.wantSource)
			}
			if err := got.CheckRunnable(); !errors.Is(err, tc.wantErr) {
				t.Errorf("CheckRunnable() = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

func TestEffectiveAutomationConfigurations_FindBySiteIDPropagatesErrors(t *testing.T) {
	site := stubConfigurations{err: workflowdomain.ErrAutomationConfigNotFound}
	if _, err := NewEffectiveAutomationConfigurations(site, stubDeploymentKeys{ok: true}).
		FindBySiteID(context.Background(), "site-1"); !errors.Is(err, workflowdomain.ErrAutomationConfigNotFound) {
		t.Errorf("FindBySiteID() error = %v, want ErrAutomationConfigNotFound", err)
	}
	keyErr := errors.New("credential key mismatch")
	site = stubConfigurations{configuration: &workflowdomain.AutomationConfiguration{Enabled: true}}
	if _, err := NewEffectiveAutomationConfigurations(site, stubDeploymentKeys{err: keyErr}).
		FindBySiteID(context.Background(), "site-1"); !errors.Is(err, keyErr) {
		t.Errorf("FindBySiteID() error = %v, want the Deployment Key lookup error", err)
	}
}

type stubAutomationRepo struct {
	workflowdomain.AutomationConfigurationRepository
	configuration workflowdomain.AutomationConfiguration
	stored        *workflowdomain.AutomationCredential
}

func (s *stubAutomationRepo) FindBySiteID(context.Context, string) (*workflowdomain.AutomationConfiguration, error) {
	copied := s.configuration
	return &copied, nil
}

func (s *stubAutomationRepo) ReplaceCredential(_ context.Context, _ string, credential workflowdomain.AutomationCredential) error {
	s.stored = &credential
	return nil
}

func TestAutomationConfigurationService_CredentialSource(t *testing.T) {
	for _, tc := range []struct {
		name string
		keys DeploymentKeySource
		want workflowdomain.CredentialSource
	}{
		{name: "deployment key", keys: stubDeploymentKeys{ok: true}, want: workflowdomain.CredentialSourceDeploymentKey},
		{name: "no deployment key", keys: stubDeploymentKeys{}, want: workflowdomain.CredentialSourceNone},
		{name: "deployment keys not wired", want: workflowdomain.CredentialSourceNone},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &stubAutomationRepo{configuration: workflowdomain.AutomationConfiguration{HasCredential: true}}
			service := NewAutomationConfigurationService(repo, nil, nil)
			if tc.keys != nil {
				service.AttachDeploymentKeys(tc.keys)
			}
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

func TestAutomationConfigurationService_ReplaceCredentialStoresOnlyBecomePassword(t *testing.T) {
	repo := &stubAutomationRepo{}
	service := NewAutomationConfigurationService(repo, nil, nil)
	if err := service.ReplaceCredential(context.Background(), "site-1", "pw"); err != nil {
		t.Fatalf("ReplaceCredential: %v", err)
	}
	if repo.stored == nil || repo.stored.SSHPrivateKey != "" || repo.stored.BecomePassword != "pw" {
		t.Errorf("stored credential = %+v, want only the become password", repo.stored)
	}
}
