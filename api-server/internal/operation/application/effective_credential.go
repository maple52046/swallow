package application

import (
	"context"
	"errors"
	"strings"

	operationdomain "github.com/maple52046/swallow/internal/operation/domain"
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
// automation reader — the Ansible executor, wait-for-ssh, and the host-user prober — receives the
// effective credential: the site's private key when it overrides, otherwise the Deployment Key,
// always with the site's become password. Every other method passes through unchanged.
//
// It is the single place the precedence is decided, so the readers cannot disagree about which
// key a Site uses. The API's configuration reads keep using the plain repository, because they
// must report what is stored, not what is effective.
type EffectiveAutomationConfigurations struct {
	operationdomain.AutomationConfigurationRepository
	deploymentKeys DeploymentKeySource
}

// NewEffectiveAutomationConfigurations decorates configurations with the Deployment Key fallback.
func NewEffectiveAutomationConfigurations(
	configurations operationdomain.AutomationConfigurationRepository,
	deploymentKeys DeploymentKeySource,
) *EffectiveAutomationConfigurations {
	return &EffectiveAutomationConfigurations{
		AutomationConfigurationRepository: configurations,
		deploymentKeys:                    deploymentKeys,
	}
}

// Credential returns the effective credential for siteID. A Site without an Automation
// Configuration is still ErrAutomationConfigNotFound (automation is not configured there at all);
// a Site whose credential has no private key and an installation without a Deployment Key is
// ErrAutomationCredentialMissing.
func (r *EffectiveAutomationConfigurations) Credential(ctx context.Context, siteID string) (operationdomain.AutomationCredential, error) {
	credential, err := r.AutomationConfigurationRepository.Credential(ctx, siteID)
	if err != nil && !errors.Is(err, operationdomain.ErrAutomationCredentialMissing) {
		return operationdomain.AutomationCredential{}, err
	}
	if strings.TrimSpace(credential.SSHPrivateKey) != "" {
		return credential, nil
	}
	privateKey, ok, keyErr := r.deploymentKeys.DeploymentPrivateKey(ctx)
	if keyErr != nil {
		return operationdomain.AutomationCredential{}, keyErr
	}
	if !ok {
		return operationdomain.AutomationCredential{}, operationdomain.ErrAutomationCredentialMissing
	}
	credential.SSHPrivateKey = privateKey
	return credential, nil
}
