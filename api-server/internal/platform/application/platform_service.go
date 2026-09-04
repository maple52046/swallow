// Package application coordinates platform registration, membership reads, and the
// policy that stops provisioning and an in-platform GPU operator fighting over drivers.
package application

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	platformdomain "github.com/maple52046/swallow/internal/platform/domain"
	serverdomain "github.com/maple52046/swallow/internal/server/domain"
	"github.com/maple52046/swallow/internal/shared/wire"
	sitedomain "github.com/maple52046/swallow/internal/site/domain"
)

// PlatformService owns platform registration, lifecycle projection, and record cleanup.
type PlatformService struct {
	platforms    platformdomain.PlatformRepository
	sites        sitedomain.SiteRepository
	servers      serverdomain.ServerRepository
	lifecycle    platformdomain.LifecycleReader
	integrations platformdomain.ManagedIntegrationCleaner
}

// NewPlatformService constructs the platform application service.
func NewPlatformService(
	platforms platformdomain.PlatformRepository,
	sites sitedomain.SiteRepository,
	servers serverdomain.ServerRepository,
	lifecycle platformdomain.LifecycleReader,
	integrations platformdomain.ManagedIntegrationCleaner,
) *PlatformService {
	return &PlatformService{
		platforms: platforms, sites: sites, servers: servers,
		lifecycle: lifecycle, integrations: integrations,
	}
}

// PlatformItem is the public platform projection.
type PlatformItem struct {
	ID                   string  `json:"id"`
	SiteID               string  `json:"siteId"`
	Name                 string  `json:"name"`
	Type                 string  `json:"type"`
	IntegrationID        *string `json:"integrationId"`
	Origin               string  `json:"origin"`
	LifecycleState       string  `json:"lifecycleState"`
	LifecycleOperationID *string `json:"lifecycleOperationId"`
	// Deployment is the non-secret topology intent recovered from durable Operation
	// provenance. It is null for registered platforms and incomplete legacy history.
	Deployment *PlatformDeploymentItem `json:"deployment"`
	// GPUStackOwner decides which subsystem installs GPU drivers. Swallow refuses
	// operations that contradict it.
	GPUStackOwner string `json:"gpuStackOwner"`
	// ExporterOwner decides which subsystem installs this platform's Prometheus
	// exporters: "ansible" (default) or "k8s".
	ExporterOwner string           `json:"exporterOwner"`
	Sync          PlatformSyncItem `json:"sync"`
	CreatedAt     string           `json:"createdAt"`
	UpdatedAt     string           `json:"updatedAt"`
}

// PlatformDeploymentItem explains topology and workload co-location without exposing
// runner variables or conflating the Node Role with workload capability.
type PlatformDeploymentItem struct {
	Topology        string                       `json:"topology"`
	RoleAssignments []PlatformRoleAssignmentItem `json:"roleAssignments"`
}

// PlatformRoleAssignmentItem is one deployment target's desired role and placement.
type PlatformRoleAssignmentItem struct {
	ServerID     string `json:"serverId"`
	Role         string `json:"role"`
	RunWorkloads bool   `json:"runWorkloads"`
}

// PlatformSyncItem is the platform membership freshness projection.
type PlatformSyncItem struct {
	LastStartedAt   *string `json:"lastStartedAt"`
	LastSucceededAt *string `json:"lastSucceededAt"`
	LastError       *string `json:"lastError"`
	MemberCount     int     `json:"memberCount"`
	// MatchedCount is how many members Swallow could match to a server.
	MatchedCount int `json:"matchedCount"`
}

// CreatePlatformInput is the validated input for a registered platform.
type CreatePlatformInput struct {
	SiteID        string
	Name          string
	Type          string
	IntegrationID string
	GPUStackOwner string
	// ExporterOwner is optional; empty defaults to ansible.
	ExporterOwner string
}

// ErrInvalidPlatform covers validation failures the delivery layer turns into a 400.
var ErrInvalidPlatform = errors.New("invalid platform")

// Create registers a platform without deployment provenance.
func (s *PlatformService) Create(ctx context.Context, input CreatePlatformInput) (*PlatformItem, error) {
	platformType := platformdomain.PlatformType(strings.TrimSpace(input.Type))
	if !platformType.Valid() {
		return nil, fmt.Errorf("%w: type must be one of %v", ErrInvalidPlatform, platformdomain.ValidPlatformTypes)
	}
	if strings.TrimSpace(input.Name) == "" {
		return nil, fmt.Errorf("%w: name is required", ErrInvalidPlatform)
	}

	owner := platformdomain.GPUStackOwner(strings.TrimSpace(input.GPUStackOwner))
	if !owner.Valid() {
		return nil, fmt.Errorf("%w: gpuStackOwner must be one of %v", ErrInvalidPlatform, platformdomain.ValidGPUStackOwners)
	}

	exporterOwner := platformdomain.ExporterOwner(strings.TrimSpace(input.ExporterOwner))
	if exporterOwner == "" {
		exporterOwner = platformdomain.ExporterOwnerAnsible
	}
	if !exporterOwner.Valid() {
		return nil, fmt.Errorf("%w: exporterOwner must be one of %v", ErrInvalidPlatform, platformdomain.ValidExporterOwners)
	}

	if _, err := s.sites.FindByID(ctx, input.SiteID); err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	platform := &platformdomain.Platform{
		ID: uuid.NewString(), SiteID: input.SiteID, Name: strings.TrimSpace(input.Name),
		Type: platformType, IntegrationID: input.IntegrationID,
		GPUStackOwner: owner, ExporterOwner: exporterOwner,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := s.platforms.Create(ctx, platform); err != nil {
		return nil, err
	}

	item := toPlatformItem(platform, registeredLifecycle())
	return &item, nil
}

// List returns platforms with lifecycle states loaded in one operation query.
func (s *PlatformService) List(ctx context.Context, siteID string) ([]PlatformItem, error) {
	platforms, err := s.platforms.List(ctx, siteID)
	if err != nil {
		return nil, err
	}
	lifecycles, err := s.readLifecycles(ctx, platforms)
	if err != nil {
		return nil, err
	}

	items := make([]PlatformItem, 0, len(platforms))
	for _, platform := range platforms {
		items = append(items, toPlatformItem(platform, lifecycles[platform.ID]))
	}
	return items, nil
}

// Get returns one platform with its operation-derived lifecycle.
func (s *PlatformService) Get(ctx context.Context, id string) (*PlatformItem, error) {
	platform, err := s.platforms.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	lifecycle, err := s.readLifecycle(ctx, platform.ID)
	if err != nil {
		return nil, err
	}
	item := toPlatformItem(platform, lifecycle)
	return &item, nil
}

// UpdatePlatformInput contains mutable registration fields.
type UpdatePlatformInput struct {
	Name          *string
	IntegrationID *string
	GPUStackOwner *string
	ExporterOwner *string
}

// Update changes registration and policy without changing lifecycle provenance.
func (s *PlatformService) Update(ctx context.Context, id string, input UpdatePlatformInput) (*PlatformItem, error) {
	platform, err := s.platforms.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}

	if input.Name != nil {
		if strings.TrimSpace(*input.Name) == "" {
			return nil, fmt.Errorf("%w: name cannot be empty", ErrInvalidPlatform)
		}
		platform.Name = strings.TrimSpace(*input.Name)
	}
	if input.IntegrationID != nil {
		platform.IntegrationID = *input.IntegrationID
	}
	if input.GPUStackOwner != nil {
		owner := platformdomain.GPUStackOwner(strings.TrimSpace(*input.GPUStackOwner))
		if !owner.Valid() {
			return nil, fmt.Errorf("%w: gpuStackOwner must be one of %v",
				ErrInvalidPlatform, platformdomain.ValidGPUStackOwners)
		}
		platform.GPUStackOwner = owner
	}
	if input.ExporterOwner != nil {
		exporterOwner := platformdomain.ExporterOwner(strings.TrimSpace(*input.ExporterOwner))
		if !exporterOwner.Valid() {
			return nil, fmt.Errorf("%w: exporterOwner must be one of %v",
				ErrInvalidPlatform, platformdomain.ValidExporterOwners)
		}
		platform.ExporterOwner = exporterOwner
	}
	platform.UpdatedAt = time.Now().UTC()

	if err := s.platforms.Update(ctx, platform); err != nil {
		return nil, err
	}
	lifecycle, err := s.readLifecycle(ctx, platform.ID)
	if err != nil {
		return nil, err
	}
	item := toPlatformItem(platform, lifecycle)
	return &item, nil
}

// Delete removes the record and projections without touching hosts or accepted operations.
func (s *PlatformService) Delete(ctx context.Context, id string) error {
	platform, err := s.platforms.FindByID(ctx, id)
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
		allowLegacy := lifecycle.Origin == platformdomain.PlatformOriginDeployed
		if err := s.integrations.DeleteForPlatform(ctx, platform, allowLegacy); err != nil {
			return err
		}
	}
	return s.platforms.Delete(ctx, id)
}

// CompleteUninstall clears projections after a successful uninstall operation.
//
// A record deleted while the operation was running is already clean from this observer's
// perspective, so a missing platform is an idempotent success.
func (s *PlatformService) CompleteUninstall(ctx context.Context, id string) error {
	platform, err := s.platforms.FindByID(ctx, id)
	if errors.Is(err, platformdomain.ErrPlatformNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := s.clearMemberships(ctx, id); err != nil {
		return err
	}
	if s.integrations != nil {
		if err := s.integrations.DeleteForPlatform(ctx, platform, true); err != nil {
			return err
		}
	}
	platform.IntegrationID = ""
	platform.OwnedIntegrationID = ""
	platform.Sync = platformdomain.SyncState{}
	platform.UpdatedAt = time.Now().UTC()
	if err := s.platforms.Update(ctx, platform); err != nil {
		return err
	}
	return s.platforms.UpdateSyncState(ctx, id, "", platformdomain.SyncState{})
}

func (s *PlatformService) clearMemberships(ctx context.Context, platformID string) error {
	members, err := s.servers.List(ctx, serverdomain.ListFilter{
		PlatformID: platformID, IncludeAbsent: true,
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

func (s *PlatformService) readLifecycles(
	ctx context.Context,
	platforms []*platformdomain.Platform,
) (map[string]platformdomain.LifecycleSnapshot, error) {
	result := make(map[string]platformdomain.LifecycleSnapshot, len(platforms))
	ids := make([]string, len(platforms))
	for i, platform := range platforms {
		ids[i] = platform.ID
		result[platform.ID] = registeredLifecycle()
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

func (s *PlatformService) readLifecycle(ctx context.Context, platformID string) (platformdomain.LifecycleSnapshot, error) {
	if s.lifecycle == nil {
		return registeredLifecycle(), nil
	}
	result, err := s.lifecycle.Read(ctx, []string{platformID})
	if err != nil {
		return platformdomain.LifecycleSnapshot{}, err
	}
	lifecycle, ok := result[platformID]
	if !ok {
		return registeredLifecycle(), nil
	}
	return lifecycle, nil
}

func registeredLifecycle() platformdomain.LifecycleSnapshot {
	return platformdomain.LifecycleSnapshot{
		Origin: platformdomain.PlatformOriginRegistered,
		State:  platformdomain.PlatformLifecycleRegistered,
	}
}

func toPlatformItem(platform *platformdomain.Platform, lifecycle platformdomain.LifecycleSnapshot) PlatformItem {
	return PlatformItem{
		ID: platform.ID, SiteID: platform.SiteID, Name: platform.Name,
		Type: string(platform.Type), IntegrationID: wire.String(platform.IntegrationID),
		Origin: string(lifecycle.Origin), LifecycleState: string(lifecycle.State),
		LifecycleOperationID: wire.String(lifecycle.OperationID),
		Deployment:           toPlatformDeploymentItem(lifecycle.Deployment),
		GPUStackOwner:        string(platform.GPUStackOwner), ExporterOwner: string(platform.ExporterOwner),
		Sync: PlatformSyncItem{
			LastStartedAt:   optionalTime(platform.Sync.LastStartedAt),
			LastSucceededAt: optionalTime(platform.Sync.LastSucceededAt),
			LastError:       wire.String(platform.Sync.LastError),
			MemberCount:     platform.Sync.MemberCount, MatchedCount: platform.Sync.MatchedCount,
		},
		CreatedAt: wire.Time(platform.CreatedAt), UpdatedAt: wire.Time(platform.UpdatedAt),
	}
}

func toPlatformDeploymentItem(operation *platformdomain.LifecycleOperation) *PlatformDeploymentItem {
	if operation == nil || operation.Intent == nil {
		return nil
	}
	assignments := make([]PlatformRoleAssignmentItem, len(operation.Intent.RoleAssignments))
	for i, assignment := range operation.Intent.RoleAssignments {
		assignments[i] = PlatformRoleAssignmentItem{
			ServerID: assignment.ServerID, Role: string(assignment.Role),
			RunWorkloads: assignment.RunWorkloads,
		}
	}
	return &PlatformDeploymentItem{
		Topology: string(operation.Intent.Topology), RoleAssignments: assignments,
	}
}

func optionalTime(t *time.Time) *string {
	if t == nil {
		return nil
	}
	return wire.TimePtr(*t)
}
