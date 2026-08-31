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

// ClusterService owns cluster registration, lifecycle projection, and record cleanup.
type ClusterService struct {
	clusters     clusterdomain.ClusterRepository
	sites        sitedomain.SiteRepository
	servers      serverdomain.ServerRepository
	lifecycle    clusterdomain.LifecycleReader
	integrations clusterdomain.ManagedIntegrationCleaner
}

// NewClusterService constructs the cluster application service.
func NewClusterService(
	clusters clusterdomain.ClusterRepository,
	sites sitedomain.SiteRepository,
	servers serverdomain.ServerRepository,
	lifecycle clusterdomain.LifecycleReader,
	integrations clusterdomain.ManagedIntegrationCleaner,
) *ClusterService {
	return &ClusterService{
		clusters: clusters, sites: sites, servers: servers,
		lifecycle: lifecycle, integrations: integrations,
	}
}

// ClusterItem is the public cluster projection.
type ClusterItem struct {
	ID                   string  `json:"id"`
	SiteID               string  `json:"siteId"`
	Name                 string  `json:"name"`
	Type                 string  `json:"type"`
	IntegrationID        *string `json:"integrationId"`
	Origin               string  `json:"origin"`
	LifecycleState       string  `json:"lifecycleState"`
	LifecycleOperationID *string `json:"lifecycleOperationId"`
	// Deployment is the non-secret topology intent recovered from durable Operation
	// provenance. It is null for registered clusters and incomplete legacy history.
	Deployment *ClusterDeploymentItem `json:"deployment"`
	// GPUStackOwner decides which subsystem installs GPU drivers. Swallow refuses
	// operations that contradict it.
	GPUStackOwner string `json:"gpuStackOwner"`
	// ExporterOwner decides which subsystem installs this cluster's Prometheus
	// exporters: "ansible" (default) or "k8s".
	ExporterOwner string          `json:"exporterOwner"`
	Sync          ClusterSyncItem `json:"sync"`
	CreatedAt     string          `json:"createdAt"`
	UpdatedAt     string          `json:"updatedAt"`
}

// ClusterDeploymentItem explains topology and workload co-location without exposing
// runner variables or conflating the Node Role with workload capability.
type ClusterDeploymentItem struct {
	Topology        string                      `json:"topology"`
	RoleAssignments []ClusterRoleAssignmentItem `json:"roleAssignments"`
}

// ClusterRoleAssignmentItem is one deployment target's desired role and placement.
type ClusterRoleAssignmentItem struct {
	ServerID     string `json:"serverId"`
	Role         string `json:"role"`
	RunWorkloads bool   `json:"runWorkloads"`
}

// ClusterSyncItem is the cluster membership freshness projection.
type ClusterSyncItem struct {
	LastStartedAt   *string `json:"lastStartedAt"`
	LastSucceededAt *string `json:"lastSucceededAt"`
	LastError       *string `json:"lastError"`
	MemberCount     int     `json:"memberCount"`
	// MatchedCount is how many members Swallow could match to a server.
	MatchedCount int `json:"matchedCount"`
}

// CreateClusterInput is the validated input for a registered cluster.
type CreateClusterInput struct {
	SiteID        string
	Name          string
	Type          string
	IntegrationID string
	GPUStackOwner string
	// ExporterOwner is optional; empty defaults to ansible.
	ExporterOwner string
}

// ErrInvalidCluster covers validation failures the delivery layer turns into a 400.
var ErrInvalidCluster = errors.New("invalid cluster")

// Create registers a cluster without deployment provenance.
func (s *ClusterService) Create(ctx context.Context, input CreateClusterInput) (*ClusterItem, error) {
	clusterType := clusterdomain.ClusterType(strings.TrimSpace(input.Type))
	if !clusterType.Valid() {
		return nil, fmt.Errorf("%w: type must be one of %v", ErrInvalidCluster, clusterdomain.ValidClusterTypes)
	}
	if strings.TrimSpace(input.Name) == "" {
		return nil, fmt.Errorf("%w: name is required", ErrInvalidCluster)
	}

	owner := clusterdomain.GPUStackOwner(strings.TrimSpace(input.GPUStackOwner))
	if !owner.Valid() {
		return nil, fmt.Errorf("%w: gpuStackOwner must be one of %v", ErrInvalidCluster, clusterdomain.ValidGPUStackOwners)
	}

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
		ID: uuid.NewString(), SiteID: input.SiteID, Name: strings.TrimSpace(input.Name),
		Type: clusterType, IntegrationID: input.IntegrationID,
		GPUStackOwner: owner, ExporterOwner: exporterOwner,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := s.clusters.Create(ctx, cluster); err != nil {
		return nil, err
	}

	item := toClusterItem(cluster, registeredLifecycle())
	return &item, nil
}

// List returns clusters with lifecycle states loaded in one operation query.
func (s *ClusterService) List(ctx context.Context, siteID string) ([]ClusterItem, error) {
	clusters, err := s.clusters.List(ctx, siteID)
	if err != nil {
		return nil, err
	}
	lifecycles, err := s.readLifecycles(ctx, clusters)
	if err != nil {
		return nil, err
	}

	items := make([]ClusterItem, 0, len(clusters))
	for _, cluster := range clusters {
		items = append(items, toClusterItem(cluster, lifecycles[cluster.ID]))
	}
	return items, nil
}

// Get returns one cluster with its operation-derived lifecycle.
func (s *ClusterService) Get(ctx context.Context, id string) (*ClusterItem, error) {
	cluster, err := s.clusters.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	lifecycle, err := s.readLifecycle(ctx, cluster.ID)
	if err != nil {
		return nil, err
	}
	item := toClusterItem(cluster, lifecycle)
	return &item, nil
}

// UpdateClusterInput contains mutable registration fields.
type UpdateClusterInput struct {
	Name          *string
	IntegrationID *string
	GPUStackOwner *string
	ExporterOwner *string
}

// Update changes registration and policy without changing lifecycle provenance.
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
	lifecycle, err := s.readLifecycle(ctx, cluster.ID)
	if err != nil {
		return nil, err
	}
	item := toClusterItem(cluster, lifecycle)
	return &item, nil
}

// Delete removes the record and projections without touching hosts or accepted operations.
func (s *ClusterService) Delete(ctx context.Context, id string) error {
	cluster, err := s.clusters.FindByID(ctx, id)
	if err != nil {
		return err
	}
	lifecycle, err := s.readLifecycle(ctx, id)
	if err != nil {
		return err
	}
	if err := s.clearMemberships(ctx, id); err != nil {
		return err
	}
	if s.integrations != nil {
		allowLegacy := lifecycle.Origin == clusterdomain.ClusterOriginDeployed
		if err := s.integrations.DeleteForCluster(ctx, cluster, allowLegacy); err != nil {
			return err
		}
	}
	return s.clusters.Delete(ctx, id)
}

// CompleteUninstall clears projections after a successful uninstall operation.
//
// A record deleted while the operation was running is already clean from this observer's
// perspective, so a missing cluster is an idempotent success.
func (s *ClusterService) CompleteUninstall(ctx context.Context, id string) error {
	cluster, err := s.clusters.FindByID(ctx, id)
	if errors.Is(err, clusterdomain.ErrClusterNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := s.clearMemberships(ctx, id); err != nil {
		return err
	}
	if s.integrations != nil {
		if err := s.integrations.DeleteForCluster(ctx, cluster, true); err != nil {
			return err
		}
	}
	cluster.IntegrationID = ""
	cluster.OwnedIntegrationID = ""
	cluster.Sync = clusterdomain.SyncState{}
	cluster.UpdatedAt = time.Now().UTC()
	if err := s.clusters.Update(ctx, cluster); err != nil {
		return err
	}
	return s.clusters.UpdateSyncState(ctx, id, "", clusterdomain.SyncState{})
}

func (s *ClusterService) clearMemberships(ctx context.Context, clusterID string) error {
	members, err := s.servers.List(ctx, serverdomain.ListFilter{
		ClusterID: clusterID, IncludeAbsent: true,
	})
	if err != nil {
		return err
	}
	for _, server := range members.Servers {
		if err := s.servers.SetMembership(ctx, server.ID, nil); err != nil {
			return err
		}
	}
	return nil
}

func (s *ClusterService) readLifecycles(
	ctx context.Context,
	clusters []*clusterdomain.Cluster,
) (map[string]clusterdomain.LifecycleSnapshot, error) {
	result := make(map[string]clusterdomain.LifecycleSnapshot, len(clusters))
	ids := make([]string, len(clusters))
	for i, cluster := range clusters {
		ids[i] = cluster.ID
		result[cluster.ID] = registeredLifecycle()
	}
	if s.lifecycle == nil || len(ids) == 0 {
		return result, nil
	}
	read, err := s.lifecycle.Read(ctx, ids)
	if err != nil {
		return nil, err
	}
	for id, lifecycle := range read {
		if _, expected := result[id]; expected {
			result[id] = lifecycle
		}
	}
	return result, nil
}

func (s *ClusterService) readLifecycle(ctx context.Context, clusterID string) (clusterdomain.LifecycleSnapshot, error) {
	if s.lifecycle == nil {
		return registeredLifecycle(), nil
	}
	result, err := s.lifecycle.Read(ctx, []string{clusterID})
	if err != nil {
		return clusterdomain.LifecycleSnapshot{}, err
	}
	lifecycle, ok := result[clusterID]
	if !ok {
		return registeredLifecycle(), nil
	}
	return lifecycle, nil
}

func registeredLifecycle() clusterdomain.LifecycleSnapshot {
	return clusterdomain.LifecycleSnapshot{
		Origin: clusterdomain.ClusterOriginRegistered,
		State:  clusterdomain.ClusterLifecycleRegistered,
	}
}

func toClusterItem(cluster *clusterdomain.Cluster, lifecycle clusterdomain.LifecycleSnapshot) ClusterItem {
	return ClusterItem{
		ID: cluster.ID, SiteID: cluster.SiteID, Name: cluster.Name,
		Type: string(cluster.Type), IntegrationID: wire.String(cluster.IntegrationID),
		Origin: string(lifecycle.Origin), LifecycleState: string(lifecycle.State),
		LifecycleOperationID: wire.String(lifecycle.OperationID),
		Deployment:           toClusterDeploymentItem(lifecycle.Deployment),
		GPUStackOwner:        string(cluster.GPUStackOwner), ExporterOwner: string(cluster.ExporterOwner),
		Sync: ClusterSyncItem{
			LastStartedAt:   optionalTime(cluster.Sync.LastStartedAt),
			LastSucceededAt: optionalTime(cluster.Sync.LastSucceededAt),
			LastError:       wire.String(cluster.Sync.LastError),
			MemberCount:     cluster.Sync.MemberCount, MatchedCount: cluster.Sync.MatchedCount,
		},
		CreatedAt: wire.Time(cluster.CreatedAt), UpdatedAt: wire.Time(cluster.UpdatedAt),
	}
}

func toClusterDeploymentItem(operation *clusterdomain.LifecycleOperation) *ClusterDeploymentItem {
	if operation == nil || operation.Intent == nil {
		return nil
	}
	assignments := make([]ClusterRoleAssignmentItem, len(operation.Intent.RoleAssignments))
	for i, assignment := range operation.Intent.RoleAssignments {
		assignments[i] = ClusterRoleAssignmentItem{
			ServerID: assignment.ServerID, Role: string(assignment.Role),
			RunWorkloads: assignment.RunWorkloads,
		}
	}
	return &ClusterDeploymentItem{
		Topology: string(operation.Intent.Topology), RoleAssignments: assignments,
	}
}

func optionalTime(t *time.Time) *string {
	if t == nil {
		return nil
	}
	return wire.TimePtr(*t)
}
