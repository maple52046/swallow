// Package application coordinates cluster registration, membership reads, and the
// policy that stops provisioning and an in-cluster GPU operator fighting over drivers.
package application

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	clusterdomain "github.com/maple52046/swallow/internal/cluster/domain"
	serverdomain "github.com/maple52046/swallow/internal/server/domain"
	"github.com/maple52046/swallow/internal/shared/wire"
	sitedomain "github.com/maple52046/swallow/internal/site/domain"
)

type ClusterService struct {
	clusters clusterdomain.ClusterRepository
	sites    sitedomain.SiteRepository
	servers  serverdomain.ServerRepository
}

func NewClusterService(
	clusters clusterdomain.ClusterRepository,
	sites sitedomain.SiteRepository,
	servers serverdomain.ServerRepository,
) *ClusterService {
	return &ClusterService{clusters: clusters, sites: sites, servers: servers}
}

type ClusterItem struct {
	ID            string  `json:"id"`
	SiteID        string  `json:"siteId"`
	Name          string  `json:"name"`
	Type          string  `json:"type"`
	IntegrationID *string `json:"integrationId"`
	// GPUStackOwner decides which subsystem installs GPU drivers. swallow refuses
	// operations that contradict it.
	GPUStackOwner string `json:"gpuStackOwner"`
	// ExporterOwner decides which subsystem installs this cluster's Prometheus
	// exporters — "ansible" (default) or "k8s".
	ExporterOwner string          `json:"exporterOwner"`
	Sync          ClusterSyncItem `json:"sync"`
	CreatedAt     string          `json:"createdAt"`
	UpdatedAt     string          `json:"updatedAt"`
}

type ClusterSyncItem struct {
	LastStartedAt   *string `json:"lastStartedAt"`
	LastSucceededAt *string `json:"lastSucceededAt"`
	LastError       *string `json:"lastError"`
	MemberCount     int     `json:"memberCount"`
	// MatchedCount is how many members swallow could match to a server. A gap means the
	// cluster contains machines swallow does not manage.
	MatchedCount int `json:"matchedCount"`
}

type CreateClusterInput struct {
	SiteID        string
	Name          string
	Type          string
	IntegrationID string
	GPUStackOwner string
	// ExporterOwner is optional; empty defaults to ansible. Only "ansible" or "k8s"
	// are accepted.
	ExporterOwner string
}

// ErrInvalidCluster covers validation failures the delivery layer turns into a 400.
var ErrInvalidCluster = errors.New("invalid cluster")

func (s *ClusterService) Create(ctx context.Context, input CreateClusterInput) (*ClusterItem, error) {
	clusterType := clusterdomain.ClusterType(strings.TrimSpace(input.Type))
	if !clusterType.Valid() {
		return nil, fmt.Errorf("%w: type must be one of %v", ErrInvalidCluster, clusterdomain.ValidClusterTypes)
	}
	if strings.TrimSpace(input.Name) == "" {
		return nil, fmt.Errorf("%w: name is required", ErrInvalidCluster)
	}

	// Required rather than defaulted: which subsystem owns GPU drivers has no safe
	// default, and guessing would silently pick a side in a conflict that breaks hosts.
	owner := clusterdomain.GPUStackOwner(strings.TrimSpace(input.GPUStackOwner))
	if !owner.Valid() {
		return nil, fmt.Errorf("%w: gpuStackOwner must be one of %v", ErrInvalidCluster, clusterdomain.ValidGPUStackOwners)
	}

	// Exporter owner defaults to ansible: unlike GPU drivers there is a safe default,
	// because a host in no cluster is already ansible-owned and joining a cluster does
	// not change that unless the operator explicitly chooses k8s.
	exporterOwner := clusterdomain.ExporterOwner(strings.TrimSpace(input.ExporterOwner))
	if exporterOwner == "" {
		exporterOwner = clusterdomain.ExporterOwnerAnsible
	}
	if !exporterOwner.Valid() {
		return nil, fmt.Errorf("%w: exporterOwner must be one of %v", ErrInvalidCluster, clusterdomain.ValidExporterOwners)
	}

	if _, err := s.sites.FindByID(ctx, input.SiteID); err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	cluster := &clusterdomain.Cluster{
		ID:            uuid.NewString(),
		SiteID:        input.SiteID,
		Name:          strings.TrimSpace(input.Name),
		Type:          clusterType,
		IntegrationID: input.IntegrationID,
		GPUStackOwner: owner,
		ExporterOwner: exporterOwner,
		CreatedAt:     now,
		UpdatedAt:     now,
	}

	if err := s.clusters.Create(ctx, cluster); err != nil {
		return nil, err
	}

	item := toClusterItem(cluster)
	return &item, nil
}

func (s *ClusterService) List(ctx context.Context, siteID string) ([]ClusterItem, error) {
	clusters, err := s.clusters.List(ctx, siteID)
	if err != nil {
		return nil, err
	}

	items := make([]ClusterItem, 0, len(clusters))
	for _, cluster := range clusters {
		items = append(items, toClusterItem(cluster))
	}
	return items, nil
}

func (s *ClusterService) Get(ctx context.Context, id string) (*ClusterItem, error) {
	cluster, err := s.clusters.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	item := toClusterItem(cluster)
	return &item, nil
}

type UpdateClusterInput struct {
	Name          *string
	IntegrationID *string
	GPUStackOwner *string
	ExporterOwner *string
}

func (s *ClusterService) Update(ctx context.Context, id string, input UpdateClusterInput) (*ClusterItem, error) {
	cluster, err := s.clusters.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}

	if input.Name != nil {
		if strings.TrimSpace(*input.Name) == "" {
			return nil, fmt.Errorf("%w: name cannot be empty", ErrInvalidCluster)
		}
		cluster.Name = strings.TrimSpace(*input.Name)
	}
	if input.IntegrationID != nil {
		cluster.IntegrationID = *input.IntegrationID
	}
	if input.GPUStackOwner != nil {
		owner := clusterdomain.GPUStackOwner(strings.TrimSpace(*input.GPUStackOwner))
		if !owner.Valid() {
			return nil, fmt.Errorf("%w: gpuStackOwner must be one of %v",
				ErrInvalidCluster, clusterdomain.ValidGPUStackOwners)
		}
		cluster.GPUStackOwner = owner
	}
	if input.ExporterOwner != nil {
		exporterOwner := clusterdomain.ExporterOwner(strings.TrimSpace(*input.ExporterOwner))
		if !exporterOwner.Valid() {
			return nil, fmt.Errorf("%w: exporterOwner must be one of %v",
				ErrInvalidCluster, clusterdomain.ValidExporterOwners)
		}
		cluster.ExporterOwner = exporterOwner
	}
	cluster.UpdatedAt = time.Now().UTC()

	if err := s.clusters.Update(ctx, cluster); err != nil {
		return nil, err
	}

	item := toClusterItem(cluster)
	return &item, nil
}

// Delete removes the registration and clears the membership axis it produced.
//
// Membership is a projection of this cluster, so leaving it behind would leave servers
// claiming to belong to something that no longer exists.
func (s *ClusterService) Delete(ctx context.Context, id string) error {
	if _, err := s.clusters.FindByID(ctx, id); err != nil {
		return err
	}

	members, err := s.servers.List(ctx, serverdomain.ListFilter{ClusterID: id, IncludeAbsent: true})
	if err != nil {
		return err
	}
	for _, server := range members.Servers {
		if err := s.servers.SetMembership(ctx, server.ID, nil); err != nil {
			return err
		}
	}

	return s.clusters.Delete(ctx, id)
}

func toClusterItem(cluster *clusterdomain.Cluster) ClusterItem {
	return ClusterItem{
		ID:            cluster.ID,
		SiteID:        cluster.SiteID,
		Name:          cluster.Name,
		Type:          string(cluster.Type),
		IntegrationID: wire.String(cluster.IntegrationID),
		GPUStackOwner: string(cluster.GPUStackOwner),
		ExporterOwner: string(cluster.ExporterOwner),
		Sync: ClusterSyncItem{
			LastStartedAt:   optionalTime(cluster.Sync.LastStartedAt),
			LastSucceededAt: optionalTime(cluster.Sync.LastSucceededAt),
			LastError:       wire.String(cluster.Sync.LastError),
			MemberCount:     cluster.Sync.MemberCount,
			MatchedCount:    cluster.Sync.MatchedCount,
		},
		CreatedAt: wire.Time(cluster.CreatedAt),
		UpdatedAt: wire.Time(cluster.UpdatedAt),
	}
}

func optionalTime(t *time.Time) *string {
	if t == nil {
		return nil
	}
	return wire.TimePtr(*t)
}
