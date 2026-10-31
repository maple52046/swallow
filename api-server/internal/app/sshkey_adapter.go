package app

import (
	"context"
	"errors"
	"fmt"

	"go.mongodb.org/mongo-driver/mongo"

	"github.com/maple52046/swallow/internal/adapters/keys/openssh"
	sshkeystore "github.com/maple52046/swallow/internal/adapters/store/mongodb/sshkey"
	"github.com/maple52046/swallow/internal/core/convergence"
	provisioningdomain "github.com/maple52046/swallow/internal/core/provisioning/domain"
	sitedomain "github.com/maple52046/swallow/internal/core/site/domain"
	sshkeyapp "github.com/maple52046/swallow/internal/core/sshkey/application"
	sshkeydomain "github.com/maple52046/swallow/internal/core/sshkey/domain"
	workflowdomain "github.com/maple52046/swallow/internal/core/workflow/domain"
	"github.com/maple52046/swallow/internal/shared/secret"
)

// newSSHKeyService assembles the SSH Key service (decision 039) over the process's MongoDB, sealer,
// integration repository, and provider factory. The api, worker, and ansible-executor processes
// each build one: the API serves the HTTP surface and runs the sync loop, the worker ensures
// registration before OS deployments, and every automation process reads the Deployment Key.
// The sealer must use the installation's credential key, or the Deployment Key cannot be opened.
func newSSHKeyService(
	db *mongo.Database,
	sealer *secret.Sealer,
	integrations sitedomain.IntegrationRepository,
	providers provisioningdomain.ProviderFactory,
) (*sshkeyapp.Service, error) {
	keys, err := sshkeystore.NewMongoKeyRepo(db, sealer)
	if err != nil {
		return nil, fmt.Errorf("ssh key repo init: %w", err)
	}
	return sshkeyapp.NewService(
		keys,
		sshkeystore.NewMongoSyncRepo(db),
		openssh.NewKeyMaterial(),
		NewProvisionerKeys(integrations, providers),
	), nil
}

// deploymentKeySource adapts the SSH Key service to the operation context's DeploymentKeySource
// port, translating the sshkey "not found" into ok=false so operation never imports sshkey types.
type deploymentKeySource struct {
	keys *sshkeyapp.Service
}

// DeploymentPrivateKey returns the sealed Deployment Key opened for immediate use; callers must not
// log or persist it.
func (s deploymentKeySource) DeploymentPrivateKey(ctx context.Context) (string, bool, error) {
	privateKey, err := s.keys.DeploymentPrivateKey(ctx)
	if errors.Is(err, sshkeydomain.ErrDeploymentKeyNotFound) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return privateKey, true, nil
}

// HasDeploymentKey answers existence without opening secret material.
func (s deploymentKeySource) HasDeploymentKey(ctx context.Context) (bool, error) {
	return s.keys.HasDeploymentKey(ctx)
}

// siteSSHPorts adapts the Site Automation Configuration to the server feature's SSHPortResolver, so
// the logins that set a Server Default User use the same port as automation. A Site without an
// Automation Configuration reports 0 (the adapter then uses 22) rather than failing the request.
type siteSSHPorts struct {
	configurations workflowdomain.AutomationConfigurationRepository
}

// SSHPort returns the Site's configured SSH port, or 0 when the Site has none.
func (p siteSSHPorts) SSHPort(ctx context.Context, siteID string) (int, error) {
	configuration, err := p.configurations.FindBySiteID(ctx, siteID)
	if errors.Is(err, workflowdomain.ErrAutomationConfigNotFound) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return configuration.SSHPort, nil
}

// sshKeyController attaches the convergence timeline to the worker's SSH Key service and returns
// the controller that runs its realization passes.
func sshKeyController(keys *sshkeyapp.Service, events convergence.EventRecorder) *sshkeyapp.Controller {
	keys.AttachEvents(events)
	return sshkeyapp.NewController(keys)
}
