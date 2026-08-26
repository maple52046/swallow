package application

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	clusterdomain "github.com/maple52046/swallow/internal/cluster/domain"
	clusterinfra "github.com/maple52046/swallow/internal/cluster/infra"
	sitedomain "github.com/maple52046/swallow/internal/site/domain"
)

// DeploymentCredential is what a successful cluster deployment produced: how to reach the
// new cluster's API and a bearer token to read it with.
type DeploymentCredential struct {
	APIEndpoint   string
	Token         string
	CACertificate string
}

// DeploymentCredentialService records the credential a cluster deployment produced by
// creating the cluster's read integration and attaching it, then reading membership once.
//
// It is the cluster side of the operation completion hook: the composition root adapts a
// successful deploy-kubernetes operation into a call here, so the cluster context never
// depends on the operation context.
type DeploymentCredentialService struct {
	clusters     clusterdomain.ClusterRepository
	integrations sitedomain.IntegrationRepository
	membership   *MembershipSyncUseCase
}

// NewDeploymentCredentialService constructs the credential recorder.
func NewDeploymentCredentialService(
	clusters clusterdomain.ClusterRepository,
	integrations sitedomain.IntegrationRepository,
	membership *MembershipSyncUseCase,
) *DeploymentCredentialService {
	return &DeploymentCredentialService{clusters: clusters, integrations: integrations, membership: membership}
}

// Record attaches a freshly deployed cluster's read credential and syncs its membership.
//
// The token is stored as a sealed cluster-kind integration credential; control-plane lease
// discovery is enabled because a k0s HA cluster's controllers are not Kubernetes nodes; and
// insecureSkipVerify is set only when the deployment returned no CA certificate. A first
// membership read is triggered so the cluster's members appear without waiting for the
// background interval; its failure is not fatal because the integration is stored and the
// background sync retries.
func (s *DeploymentCredentialService) Record(ctx context.Context, clusterID string, credential DeploymentCredential) error {
	if strings.TrimSpace(credential.APIEndpoint) == "" || strings.TrimSpace(credential.Token) == "" {
		return fmt.Errorf("cluster deployment returned no usable credential")
	}
	cluster, err := s.clusters.FindByID(ctx, clusterID)
	if err != nil {
		return err
	}

	settings := map[string]string{
		clusterinfra.SettingControllerLeaseDiscovery: "true",
	}
	if strings.TrimSpace(credential.CACertificate) == "" {
		settings[clusterinfra.SettingInsecureSkipVerify] = "true"
	}

	now := time.Now().UTC()
	integration := &sitedomain.Integration{
		ID:           uuid.NewString(),
		SiteID:       cluster.SiteID,
		Kind:         sitedomain.IntegrationKindCluster,
		ProviderKind: sitedomain.ProviderKindKubernetes,
		Name:         cluster.Name + " (deployed)",
		Endpoint:     credential.APIEndpoint,
		Enabled:      true,
		Settings:     settings,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := s.integrations.Create(ctx, integration, credential.Token); err != nil {
		return err
	}

	cluster.IntegrationID = integration.ID
	cluster.UpdatedAt = now
	if err := s.clusters.Update(ctx, cluster); err != nil {
		return err
	}

	_, _ = s.membership.Execute(ctx, cluster.ID)
	return nil
}
