package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"go.mongodb.org/mongo-driver/mongo"

	operationdomain "github.com/maple52046/swallow/internal/operation/domain"
	provisioningdomain "github.com/maple52046/swallow/internal/provisioning/domain"
	"github.com/maple52046/swallow/internal/shared/secret"
	sitedomain "github.com/maple52046/swallow/internal/site/domain"
	sshkeyapp "github.com/maple52046/swallow/internal/sshkey/application"
	sshkeydomain "github.com/maple52046/swallow/internal/sshkey/domain"
	sshkeyinfra "github.com/maple52046/swallow/internal/sshkey/infra"
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
	keys, err := sshkeyinfra.NewMongoKeyRepo(db, sealer)
	if err != nil {
		return nil, fmt.Errorf("ssh key repo init: %w", err)
	}
	return sshkeyapp.NewService(
		keys,
		sshkeyinfra.NewMongoSyncRepo(db),
		sshkeyinfra.NewKeyMaterial(),
		sshkeyinfra.NewProvisionerKeys(integrations, providers),
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
	configurations operationdomain.AutomationConfigurationRepository
}

// SSHPort returns the Site's configured SSH port, or 0 when the Site has none.
func (p siteSSHPorts) SSHPort(ctx context.Context, siteID string) (int, error) {
	configuration, err := p.configurations.FindBySiteID(ctx, siteID)
	if errors.Is(err, operationdomain.ErrAutomationConfigNotFound) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return configuration.SSHPort, nil
}

// sshKeySyncOnProvisionerChange turns a provisioner Integration write into a sync request, so a
// newly registered MAAS receives swallow's keys without waiting for the periodic pass.
type sshKeySyncOnProvisionerChange struct {
	keys *sshkeyapp.Service
}

// ProvisionerChanged requests a sync without blocking the Integration write that triggered it.
func (l sshKeySyncOnProvisionerChange) ProvisionerChanged() {
	l.keys.RequestSync()
}

// runSSHKeySync realizes SSH Keys in provisioners until ctx is cancelled. It runs once at start
// (so keys added while the API was down, and the freshly bootstrapped Deployment Key, reach the
// provisioner promptly), on every sync request, and every interval to repair drift. Passes are
// sequential in this goroutine; a failed pass is logged and retried on the next trigger, never
// fatal. Only the API process runs this loop, because only it consumes the sync request channel.
func runSSHKeySync(ctx context.Context, keys *sshkeyapp.Service, interval time.Duration) {
	syncOnce := func() {
		if err := keys.Sync(ctx); err != nil && ctx.Err() == nil {
			slog.Warn("ssh key sync incomplete", "error", err)
		}
	}
	syncOnce()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			syncOnce()
		case <-keys.SyncRequests():
			syncOnce()
		}
	}
}
