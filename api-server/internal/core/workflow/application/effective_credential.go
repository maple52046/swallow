package application

import (
	"context"
	"errors"

	workflowdomain "github.com/maple52046/swallow/internal/core/workflow/domain"
)

// DeploymentKeySource supplies the installation's Deployment Key to automation (decision 039). It
// is implemented by the sshkey feature through a composition-root adapter so this context never
// depends on SSH Key records.
//
// DeploymentPrivateKey returns ok=false (and a nil error) when no Deployment Key exists; an error
// means the key exists but could not be read (for example the credential key does not match).
// Callers must not log, persist, or return the private key. HasDeploymentKey answers existence
// without reading secret material.
type DeploymentKeySource interface {
	DeploymentPrivateKey(ctx context.Context) (privateKey string, ok bool, err error)
	HasDeploymentKey(ctx context.Context) (bool, error)
}

// EffectiveAutomationConfigurations wraps the Site Automation Configuration repository so every
// automation reader — the execution service, the Ansible executor, wait-for-ssh, and the
// host-user prober — sees what a run will actually use: the installation's Deployment Key, the
// only automation SSH key (decision 041), with the Site's become password. FindBySiteID derives
// CredentialSource so callers can gate on AutomationConfiguration.CheckRunnable; every other
// method passes through unchanged.
//
// It is the single place automation obtains key material, so readers cannot disagree about
// which key a Site uses.
type EffectiveAutomationConfigurations struct {
	workflowdomain.AutomationConfigurationRepository
	deploymentKeys DeploymentKeySource
}

// NewEffectiveAutomationConfigurations decorates configurations with the Deployment Key.
func NewEffectiveAutomationConfigurations(
	configurations workflowdomain.AutomationConfigurationRepository,
	deploymentKeys DeploymentKeySource,
) *EffectiveAutomationConfigurations {
	return &EffectiveAutomationConfigurations{
		AutomationConfigurationRepository: configurations,
		deploymentKeys:                    deploymentKeys,
	}
}

// FindBySiteID returns the Site's configuration with CredentialSource derived from the
// Deployment Key's existence. A repository error, including ErrAutomationConfigNotFound, is
// returned unchanged.
func (r *EffectiveAutomationConfigurations) FindBySiteID(ctx context.Context, siteID string) (*workflowdomain.AutomationConfiguration, error) {
	configuration, err := r.AutomationConfigurationRepository.FindBySiteID(ctx, siteID)
	if err != nil {
		return nil, err
	}
	if configuration.CredentialSource, err = credentialSource(ctx, r.deploymentKeys); err != nil {
		return nil, err
	}
	return configuration, nil
}

// Credential returns the effective credential for siteID: the Deployment Key with the Site's
// become password, if any. A Site without an Automation Configuration is still
// ErrAutomationConfigNotFound (automation is not configured there at all); an installation
// without a Deployment Key is ErrAutomationCredentialMissing.
func (r *EffectiveAutomationConfigurations) Credential(ctx context.Context, siteID string) (workflowdomain.AutomationCredential, error) {
	credential, err := r.AutomationConfigurationRepository.Credential(ctx, siteID)
	if err != nil && !errors.Is(err, workflowdomain.ErrAutomationCredentialMissing) {
		return workflowdomain.AutomationCredential{}, err
	}
	privateKey, ok, keyErr := r.deploymentKeys.DeploymentPrivateKey(ctx)
	if keyErr != nil {
		return workflowdomain.AutomationCredential{}, keyErr
	}
	if !ok {
		return workflowdomain.AutomationCredential{}, workflowdomain.ErrAutomationCredentialMissing
	}
	return workflowdomain.AutomationCredential{SSHPrivateKey: privateKey, BecomePassword: credential.BecomePassword}, nil
}

// credentialSource is the one derivation of a Site's credential source, shared by the API read
// path and automation so a Site the API reports as deploymentKey is exactly a Site automation
// can run. A nil source (only in tests and partial wiring) reports none.
func credentialSource(ctx context.Context, deploymentKeys DeploymentKeySource) (workflowdomain.CredentialSource, error) {
	if deploymentKeys == nil {
		return workflowdomain.CredentialSourceNone, nil
	}
	exists, err := deploymentKeys.HasDeploymentKey(ctx)
	if err != nil {
		return "", err
	}
	if exists {
		return workflowdomain.CredentialSourceDeploymentKey, nil
	}
	return workflowdomain.CredentialSourceNone, nil
}
