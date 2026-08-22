package infra

import (
	"context"
	"fmt"
	"time"

	clusterdomain "github.com/maple52046/swallow/internal/cluster/domain"
	"github.com/maple52046/swallow/internal/cluster/infra/clusterapi"
	sitedomain "github.com/maple52046/swallow/internal/site/domain"
)

const defaultTimeout = 30 * time.Second

// Setting keys an operator may set on a cluster integration.
const (
	SettingTimeout            = "timeout"
	SettingInsecureSkipVerify = "insecureSkipVerify"
	// SettingSlurmAPIVersion selects the slurmrestd endpoint version, which tracks
	// the Slurm release.
	SettingSlurmAPIVersion = "slurmApiVersion"
)

// ReaderFactory builds cluster readers from a cluster's integration.
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

func (f *ReaderFactory) For(ctx context.Context, cluster *clusterdomain.Cluster) (clusterdomain.ClusterReader, error) {
	if cluster.IntegrationID == "" {
		return nil, clusterdomain.ErrNoClusterIntegration
	}

	integration, err := f.integrations.FindByID(ctx, cluster.IntegrationID)
	if err != nil {
		return nil, err
	}
	if integration.Kind != sitedomain.IntegrationKindCluster {
		return nil, fmt.Errorf("integration %q is registered as %q, not a cluster API",
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

	switch cluster.Type {
	case clusterdomain.ClusterTypeKubernetes:
		return clusterapi.NewKubernetesReader(integration.Endpoint, token, timeout, insecure)

	case clusterdomain.ClusterTypeSlurm:
		return clusterapi.NewSlurmReader(
			integration.Endpoint,
			token,
			integration.Setting(SettingSlurmAPIVersion, clusterapi.DefaultSlurmAPIVersion),
			timeout,
			insecure,
		)

	default:
		return nil, fmt.Errorf("%w: %q", clusterdomain.ErrUnsupportedClusterType, cluster.Type)
	}
}
