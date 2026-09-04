package infra

import (
	"context"
	"fmt"
	"time"

	platformdomain "github.com/maple52046/swallow/internal/platform/domain"
	"github.com/maple52046/swallow/internal/platform/infra/platformapi"
	sitedomain "github.com/maple52046/swallow/internal/site/domain"
)

const defaultTimeout = 30 * time.Second

// Setting keys an operator may set on a platform integration.
const (
	SettingTimeout            = "timeout"
	SettingInsecureSkipVerify = "insecureSkipVerify"
	// SettingSlurmAPIVersion selects the slurmrestd endpoint version, which tracks
	// the Slurm release.
	SettingSlurmAPIVersion = "slurmApiVersion"
	// SettingControllerLeaseDiscovery makes the Kubernetes reader also report dedicated
	// k0s controllers from their kube-node-lease leases. Off by default because the lease
	// naming is a k0s implementation detail; a platform swallow itself deploys turns it on.
	SettingControllerLeaseDiscovery = "controllerLeaseDiscovery"
)

// ReaderFactory builds platform readers from a platform's integration.
//
// Not cached, unlike the provisioning and automation factories: membership is read once
// per interval rather than continuously, so there is no connection reuse to protect and
// no reason to hold a stale client.
type ReaderFactory struct {
	integrations sitedomain.IntegrationRepository
}

func NewReaderFactory(integrations sitedomain.IntegrationRepository) *ReaderFactory {
	return &ReaderFactory{integrations: integrations}
}

func (f *ReaderFactory) For(ctx context.Context, platform *platformdomain.Platform) (platformdomain.PlatformReader, error) {
	if platform.IntegrationID == "" {
		return nil, platformdomain.ErrNoPlatformIntegration
	}

	integration, err := f.integrations.FindByID(ctx, platform.IntegrationID)
	if err != nil {
		return nil, err
	}
	if integration.Kind != sitedomain.IntegrationKindPlatform {
		return nil, fmt.Errorf("integration %q is registered as %q, not a platform API",
			integration.Name, integration.Kind)
	}

	token, err := f.integrations.Credential(ctx, integration.ID)
	if err != nil {
		return nil, err
	}

	timeout := defaultTimeout
	if raw := integration.Setting(SettingTimeout, ""); raw != "" {
		if parsed, err := time.ParseDuration(raw); err == nil && parsed > 0 {
			timeout = parsed
		}
	}
	insecure := integration.SettingBool(SettingInsecureSkipVerify)

	switch platform.Type {
	case platformdomain.PlatformTypeKubernetes:
		controllerLeases := integration.SettingBool(SettingControllerLeaseDiscovery)
		return platformapi.NewKubernetesReader(integration.Endpoint, token, timeout, insecure, controllerLeases)

	case platformdomain.PlatformTypeSlurm:
		return platformapi.NewSlurmReader(
			integration.Endpoint,
			token,
			integration.Setting(SettingSlurmAPIVersion, platformapi.DefaultSlurmAPIVersion),
			timeout,
			insecure,
		)

	default:
		return nil, fmt.Errorf("%w: %q", platformdomain.ErrUnsupportedPlatformType, platform.Type)
	}
}
