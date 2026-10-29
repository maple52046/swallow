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
	// canceler is optional. When wired, Delete cancels the platform's in-flight durable
	// operations first so their resource leases are released instead of orphaned.
	canceler platformdomain.PlatformOperationCanceler
	// gpuOperators is optional. When wired, a completed Platform uninstall marks a managed GPU
	// Operator absent, because removing k0s removed the operator with it.
	gpuOperators platformdomain.GPUOperatorRecorder
	// omnistats is optional. When wired, a completed Platform uninstall marks a managed Omnistat
	// absent and removes its Monitoring Link, because uninstall-slurm removed the exporter.
	omnistats platformdomain.OmnistatRecorder
	// deviceMetricsExporters is optional, like omnistats: a completed uninstall marks a managed
	// Device Metrics Exporter absent and removes its link; an install's completion records the
	// GPU identity it found.
	deviceMetricsExporters platformdomain.DeviceMetricsExporterRecorder
	// claims is optional (tests). When wired, Delete and a successful uninstall release the
	// Platform's Platform Claims, so its Servers can be released or claimed again (decision 067).
	claims serverdomain.PlatformClaimStore
	// exporterAssignments is optional (tests). When wired with claims, a successful Slurm uninstall
	// marks its Servers' device-metrics-exporter Software Assignments absent (decision 068).
	exporterAssignments platformdomain.DeviceMetricsExporterAssignments
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

// AttachOperationCanceler wires in-flight operation cancellation after construction, keeping
// the operation context out of the platform service's constructor dependencies. It is
// composed at the API wiring root, where the orchestration service already exists.
func (s *PlatformService) AttachOperationCanceler(canceler platformdomain.PlatformOperationCanceler) {
	s.canceler = canceler
}

// AttachGPUOperatorRecorder lets CompleteUninstall record that a Platform uninstall also removed
// its Swallow-managed GPU Operator. Every process that completes uninstalls must wire it, or the
// Platform keeps reporting an operator that no longer exists.
func (s *PlatformService) AttachGPUOperatorRecorder(recorder platformdomain.GPUOperatorRecorder) {
	s.gpuOperators = recorder
}

// AttachOmnistatRecorder lets CompleteUninstall record that a Slurm uninstall also removed the
// Platform's Swallow-managed Omnistat. Every process that completes uninstalls must wire it.
func (s *PlatformService) AttachOmnistatRecorder(recorder platformdomain.OmnistatRecorder) {
	s.omnistats = recorder
}

// AttachDeviceMetricsExporterRecorder lets CompleteUninstall record that a Slurm uninstall also
// removed the Platform's Swallow-managed Device Metrics Exporter, and lets an install's completion
// record its GPU identity. Every process that completes uninstalls or installs must wire it.
func (s *PlatformService) AttachDeviceMetricsExporterRecorder(recorder platformdomain.DeviceMetricsExporterRecorder) {
	s.deviceMetricsExporters = recorder
}

// AttachDeviceMetricsExporterAssignments lets a successful Slurm uninstall mark the
// device-metrics-exporter Software Assignments of its Servers absent, as the uninstall removed the
// exporter from them. Every process that completes uninstalls must wire it together with the
// Platform Claim store (which names those Servers), or the records outlive the package.
func (s *PlatformService) AttachDeviceMetricsExporterAssignments(assignments platformdomain.DeviceMetricsExporterAssignments) {
	s.exporterAssignments = assignments
}

// RecordOmnistatGrafana registers the Grafana a successful Omnistat install reported as the
// Platform's `Omnistat Grafana` Monitoring Link (see recordFeatureGrafana).
func (s *PlatformService) RecordOmnistatGrafana(ctx context.Context, platformID, grafanaURL string) error {
	return s.recordFeatureGrafana(ctx, platformID, platformdomain.OmnistatGrafanaLinkName, grafanaURL)
}

// RecordDeviceMetricsExporterResult records what a successful Device Metrics Exporter install
// reported: its Grafana as the Platform's `Device Metrics Exporter Grafana` Monitoring Link, and the
// GPU identity it found, fenced on recordID (the Workflow the record names) so a superseded
// Workflow's late result changes nothing. Like the link, a write failure fails the install Task.
func (s *PlatformService) RecordDeviceMetricsExporterResult(
	ctx context.Context,
	platformID, recordID, grafanaURL string,
	identity platformdomain.GPUIdentity,
) error {
	if s.deviceMetricsExporters == nil {
		return fmt.Errorf("%w: Device Metrics Exporter recording is unavailable", platformdomain.ErrInvalidDeviceMetricsExporter)
	}
	if err := s.recordFeatureGrafana(ctx, platformID, platformdomain.DeviceMetricsExporterGrafanaLinkName, grafanaURL); err != nil {
		return err
	}
	_, err := s.deviceMetricsExporters.UpdateDeviceMetricsExporterGPUIdentity(ctx, platformID, recordID, identity, time.Now().UTC())
	return err
}

// recordFeatureGrafana registers the Grafana a successful Slurm monitoring feature install reported
// as the Monitoring Link named linkName, replacing a previous link of that name and keeping every
// other link (glossary Monitoring Link). It is called from the install Task's completion, so a write
// failure fails that Task and its retry records the link again. A Platform that already holds the
// maximum number of other links cannot take it and returns ErrInvalidMonitoringLink.
func (s *PlatformService) recordFeatureGrafana(ctx context.Context, platformID, linkName, grafanaURL string) error {
	platform, err := s.platforms.FindByID(ctx, platformID)
	if err != nil {
		return err
	}
	links, err := platformdomain.NormalizeMonitoringLinks(platformdomain.UpsertMonitoringLink(platform.MonitoringLinks,
		platformdomain.MonitoringLink{Name: linkName, URL: grafanaURL}))
	if err != nil {
		return err
	}
	platform.MonitoringLinks = links
	platform.UpdatedAt = time.Now().UTC()
	return s.platforms.Update(ctx, platform)
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
	ExporterOwner string `json:"exporterOwner"`
	// MonitoringLinks are the operator-registered links to the runtime's own monitoring UI.
	// Always an array (empty when none), so clients never branch on null.
	MonitoringLinks []MonitoringLinkItem `json:"monitoringLinks"`
	// GPUOperator is the Swallow-managed GPU Operator, null when Swallow never managed one here.
	GPUOperator *GPUOperatorItem `json:"gpuOperator"`
	// Omnistat is the Swallow-managed Omnistat of a Slurm Platform, null when never installed.
	Omnistat *OmnistatItem `json:"omnistat"`
	// DeviceMetricsExporter is the Swallow-managed Device Metrics Exporter of a Slurm Platform,
	// null when never installed.
	DeviceMetricsExporter *DeviceMetricsExporterItem `json:"deviceMetricsExporter"`
	// ContainerRuntime is the Slurm container runtime feature (docker-ce or podman), null when off.
	ContainerRuntime *string `json:"containerRuntime"`
	// GPUDriver is the operator's amdgpu driver intent, null when none was requested.
	GPUDriver *GPUDriverItem `json:"gpuDriver"`
	// GPUGresType is the GRES type name a Slurm deploy gave every GPU node, null when it is
	// derived per node from the GPU architecture.
	GPUGresType *string `json:"gpuGresType"`
	// OCIContainers and PMIx are the Slurm native OCI container and host MPI features; false when
	// off (always for Kubernetes).
	OCIContainers bool             `json:"ociContainers"`
	PMIx          bool             `json:"pmix"`
	Sync          PlatformSyncItem `json:"sync"`
	CreatedAt     string           `json:"createdAt"`
	UpdatedAt     string           `json:"updatedAt"`
}

// GPUDriverItem is the wire shape of a Platform's amdgpu driver intent (platforms.md, "GPU
// driver options"): the operator's module options. Options a topology rule adds on a node are not
// listed here; the Workflow that loads the driver reports them.
type GPUDriverItem struct {
	ModuleOptions map[string]string `json:"moduleOptions"`
}

func toGPUDriverItem(settings platformdomain.GPUDriverSettings) *GPUDriverItem {
	if settings.Empty() {
		return nil
	}
	return &GPUDriverItem{ModuleOptions: settings.ModuleOptions}
}

// OmnistatItem is the wire shape of a Slurm Platform's Swallow-managed Omnistat (platforms.md,
// "Omnistat"): the pinned version, how Prometheus and Grafana were installed and on which Server,
// and the Omnistat State of the Workflow installing it.
type OmnistatItem struct {
	Enabled               bool    `json:"enabled"`
	Version               string  `json:"version"`
	InstallMethod         string  `json:"installMethod"`
	ObservabilityServerID string  `json:"observabilityServerId"`
	State                 string  `json:"state"`
	LastWorkflowID        *string `json:"lastWorkflowId"`
	UpdatedAt             string  `json:"updatedAt"`
}

// DeviceMetricsExporterItem is the wire shape of a Slurm Platform's Swallow-managed Device Metrics
// Exporter (platforms.md, "Device Metrics Exporter"): the pinned version, how Prometheus and
// Grafana were installed and on which Server, the Device Metrics Exporter State of the Workflow
// installing it, and the GPU identity a successful install found (null before one).
type DeviceMetricsExporterItem struct {
	Enabled               bool    `json:"enabled"`
	Version               string  `json:"version"`
	InstallMethod         string  `json:"installMethod"`
	ObservabilityServerID string  `json:"observabilityServerId"`
	State                 string  `json:"state"`
	GPUIdentity           *string `json:"gpuIdentity"`
	LastWorkflowID        *string `json:"lastWorkflowId"`
	UpdatedAt             string  `json:"updatedAt"`
}

// GPUOperatorItem is the wire shape of a Platform's Swallow-managed GPU Operator (platforms.md,
// "Manage The GPU Operator"): the latest requested settings plus the GPU Operator State of the
// Workflow applying them. DRADriverVersion is null outside DRA mode.
type GPUOperatorItem struct {
	Enabled          bool    `json:"enabled"`
	Version          string  `json:"version"`
	AllocationMode   string  `json:"allocationMode"`
	DRADriverVersion *string `json:"draDriverVersion"`
	ResourceNaming   string  `json:"resourceNaming"`
	NodeLabeller     bool    `json:"nodeLabeller"`
	MetricsExporter  bool    `json:"metricsExporter"`
	State            string  `json:"state"`
	LastWorkflowID   *string `json:"lastWorkflowId"`
	UpdatedAt        string  `json:"updatedAt"`
}

// MonitoringLinkItem is the wire shape of one Monitoring Link, used both in the projection and
// as the PATCH replacement list.
type MonitoringLinkItem struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

// PlatformDeploymentItem explains topology and workload co-location without exposing
// runner variables or conflating the Node Role with workload capability. LoginServerIDs names
// the Slurm login (submission) hosts, which are neither role assignments nor members; a read
// model resolves them to an address to tell the operator which host to use to run the cluster.
type PlatformDeploymentItem struct {
	Topology        string                       `json:"topology"`
	RoleAssignments []PlatformRoleAssignmentItem `json:"roleAssignments"`
	LoginServerIDs  []string                     `json:"loginServerIds,omitempty"`
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

// CreatePlatformInput is the validated input for creating a Platform record.
//
// It is internal-only: the deploy use case calls it to create the record before launching the
// build Operation. There is no public register-existing route (decision 032), so it carries no
// integrationId — a Platform's credential Integration is attached later by its deployment.
type CreatePlatformInput struct {
	SiteID        string
	Name          string
	Type          string
	GPUStackOwner string
	// ExporterOwner is optional; empty defaults to ansible.
	ExporterOwner string
	// ContainerRuntime is the Slurm container runtime feature, empty when off. The deploy use case
	// validates it.
	ContainerRuntime platformdomain.ContainerRuntime
	// GPUDriver is the normalized amdgpu driver intent, empty when none was requested.
	GPUDriver platformdomain.GPUDriverSettings
	// GPUGresType is the validated Slurm GRES type override, empty when derived.
	GPUGresType string
	// OCIContainers and PMIx are the Slurm native OCI container and host MPI features.
	OCIContainers bool
	PMIx          bool
}

// ErrInvalidPlatform covers validation failures the delivery layer turns into a 400.
var ErrInvalidPlatform = errors.New("invalid platform")

// Create persists a Platform record without deployment provenance. Its origin becomes
// `deployed` once the caller launches a deploy Operation for it; a record left without an
// Operation would read as the legacy `registered` state, which no public route now produces.
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
		Type:          platformType,
		GPUStackOwner: owner, ExporterOwner: exporterOwner,
		ContainerRuntime: input.ContainerRuntime, GPUDriver: input.GPUDriver, GPUGresType: input.GPUGresType,
		OCIContainers: input.OCIContainers, PMIx: input.PMIx,
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

// UpdatePlatformInput contains the mutable policy fields. It intentionally excludes the
// credential Integration: that is set only by deployment, and letting it be repointed here
// would be a register-existing back door removed by decision 032.
type UpdatePlatformInput struct {
	Name          *string
	GPUStackOwner *string
	ExporterOwner *string
	// MonitoringLinks replaces the whole list when non-nil; a pointer to an empty slice clears
	// it, and nil leaves it unchanged.
	MonitoringLinks *[]MonitoringLinkItem
}

// Update changes registration, policy, and Monitoring Links without changing lifecycle
// provenance. Every supplied field is validated before anything is written, so a request with
// one invalid field (ErrInvalidPlatform) changes nothing.
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
	if input.GPUStackOwner != nil {
		owner := platformdomain.GPUStackOwner(strings.TrimSpace(*input.GPUStackOwner))
		if !owner.Valid() {
			return nil, fmt.Errorf("%w: gpuStackOwner must be one of %v",
				ErrInvalidPlatform, platformdomain.ValidGPUStackOwners)
		}
		if owner == platformdomain.GPUStackOwnerProvisioning && platform.GPUOperator.Managed() {
			return nil, fmt.Errorf("%w: Swallow manages a GPU Operator on this Platform; uninstall it before setting gpuStackOwner to provisioning",
				platformdomain.ErrGPUOperatorConflict)
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
	if input.MonitoringLinks != nil {
		requested := make([]platformdomain.MonitoringLink, len(*input.MonitoringLinks))
		for i, link := range *input.MonitoringLinks {
			requested[i] = platformdomain.MonitoringLink{Name: link.Name, URL: link.URL}
		}
		links, err := platformdomain.NormalizeMonitoringLinks(requested)
		if err != nil {
			return nil, fmt.Errorf("%w: %w", ErrInvalidPlatform, err)
		}
		platform.MonitoringLinks = links
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

// Delete removes the record and projections. It first cancels the platform's in-flight
// durable operations so their resource leases are released and the member servers are freed;
// leaving them would orphan work that blocks any later deploy or release on those hosts.
func (s *PlatformService) Delete(ctx context.Context, id string) error {
	platform, err := s.platforms.FindByID(ctx, id)
	if err != nil {
		return err
	}
	lifecycle, err := s.readLifecycle(ctx, id)
	if err != nil {
		return err
	}
	if s.canceler != nil {
		if err := s.canceler.CancelActiveForPlatform(ctx, id); err != nil {
			return err
		}
	}
	if err := s.clearMemberships(ctx, id); err != nil {
		return err
	}
	// The record is going away, so nothing can uninstall through it any more; its claims go with
	// it (the hosts are not changed, which is what Delete means).
	if err := s.releaseClaims(ctx, id); err != nil {
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

// markExportersRemoved marks the device-metrics-exporter Software Assignments of a Slurm
// Platform's claimed Servers absent. Kubernetes Platforms are skipped: their uninstall does not
// touch a standalone exporter. Without the claim store or the port there is nothing to do.
func (s *PlatformService) markExportersRemoved(ctx context.Context, platform *platformdomain.Platform) error {
	if platform.Type != platformdomain.PlatformTypeSlurm || s.claims == nil || s.exporterAssignments == nil {
		return nil
	}
	serverIDs, err := s.claims.ClaimedServerIDs(ctx, platform.ID)
	if err != nil || len(serverIDs) == 0 {
		return err
	}
	return s.exporterAssignments.MarkDeviceMetricsExporterRemoved(ctx, serverIDs)
}

// CompleteUninstall clears projections after a successful uninstall operation.
//
// A record deleted while the operation was running is already clean from this observer's
// perspective, so a missing platform is an idempotent success. That also makes a retried
// completion safe after deleteRecord already removed the record.
//
// deleteRecord is the operator's uninstall-time choice (recorded on the Operation) to remove the
// Platform record once the cleanup succeeds. Unlike Delete it never cancels Operations: the
// caller is the uninstall's own success hook, and that uninstall is still the Workflow holding
// the targets, so canceling would turn a finished uninstall into a canceled one.
func (s *PlatformService) CompleteUninstall(ctx context.Context, id string, deleteRecord bool) error {
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
	// uninstall-slurm removes the Device Metrics Exporter from every target whatever the feature
	// was, so the Servers' records go with it. They are named by the claims released below.
	if err := s.markExportersRemoved(ctx, platform); err != nil {
		return err
	}
	// The hosts no longer carry the platform, so its Servers are free to be released or claimed.
	if err := s.releaseClaims(ctx, id); err != nil {
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
	omnistatManaged := s.omnistats != nil && platform.Omnistat.Managed()
	if omnistatManaged {
		// The exporter left with the Platform, so its Grafana shortcut goes too; other links stay.
		platform.MonitoringLinks = platformdomain.RemoveMonitoringLink(platform.MonitoringLinks, platformdomain.OmnistatGrafanaLinkName)
	}
	exporterManaged := s.deviceMetricsExporters != nil && platform.DeviceMetricsExporter.Managed()
	if exporterManaged {
		platform.MonitoringLinks = platformdomain.RemoveMonitoringLink(platform.MonitoringLinks,
			platformdomain.DeviceMetricsExporterGrafanaLinkName)
	}
	if err := s.platforms.Update(ctx, platform); err != nil {
		return err
	}
	if omnistatManaged {
		// The record keeps its last workflow, so its own fence still matches.
		omnistat := *platform.Omnistat
		omnistat.State = platformdomain.OmnistatAbsent
		omnistat.UpdatedAt = platform.UpdatedAt
		if err := s.omnistats.SetOmnistat(ctx, id, &omnistat); err != nil {
			return err
		}
	}
	if exporterManaged {
		exporter := *platform.DeviceMetricsExporter
		exporter.State = platformdomain.DeviceMetricsExporterAbsent
		exporter.UpdatedAt = platform.UpdatedAt
		if err := s.deviceMetricsExporters.SetDeviceMetricsExporter(ctx, id, &exporter); err != nil {
			return err
		}
	}
	if s.gpuOperators != nil && platform.GPUOperator.Managed() {
		// k0s reset (or the release that wiped the hosts) removed the operator with the cluster.
		// The record keeps its last workflow, so its own fence still matches.
		operator := *platform.GPUOperator
		operator.State = platformdomain.GPUOperatorAbsent
		operator.UpdatedAt = platform.UpdatedAt
		if err := s.gpuOperators.SetGPUOperator(ctx, id, &operator, platformdomain.GPUStackOwnerProvisioning); err != nil {
			return err
		}
	}
	if err := s.platforms.UpdateSyncState(ctx, id, "", platformdomain.SyncState{}); err != nil {
		return err
	}
	if !deleteRecord {
		return nil
	}
	// Membership and the owned Integration are already gone above, which is everything Delete
	// removes besides the record itself. An operator Delete racing this step leaves the same end
	// state, so a record that is already gone is not a failure.
	if err := s.platforms.Delete(ctx, id); err != nil && !errors.Is(err, platformdomain.ErrPlatformNotFound) {
		return err
	}
	return nil
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
		MonitoringLinks:       toMonitoringLinkItems(platform.MonitoringLinks),
		GPUOperator:           toGPUOperatorItem(platform.GPUOperator),
		Omnistat:              toOmnistatItem(platform.Omnistat),
		DeviceMetricsExporter: toDeviceMetricsExporterItem(platform.DeviceMetricsExporter),
		ContainerRuntime:      wire.String(string(platform.ContainerRuntime)),
		GPUDriver:             toGPUDriverItem(platform.GPUDriver),
		GPUGresType:           wire.String(platform.GPUGresType),
		OCIContainers:         platform.OCIContainers, PMIx: platform.PMIx,
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
		LoginServerIDs: operation.Intent.LoginServerIDs,
	}
}

// toGPUOperatorItem projects the record.
func toGPUOperatorItem(operator *platformdomain.GPUOperator) *GPUOperatorItem {
	if operator == nil {
		return nil
	}
	settings := operator.Settings
	return &GPUOperatorItem{
		Enabled: settings.Enabled, Version: settings.Version,
		AllocationMode:   string(settings.AllocationMode),
		DRADriverVersion: wire.String(settings.DRADriverVersion),
		ResourceNaming:   string(settings.ResourceNaming),
		NodeLabeller:     settings.NodeLabeller, MetricsExporter: settings.MetricsExporter,
		State:          string(operator.State),
		LastWorkflowID: wire.String(operator.LastWorkflowID), UpdatedAt: wire.Time(operator.UpdatedAt),
	}
}

// toOmnistatItem projects the record.
func toOmnistatItem(omnistat *platformdomain.Omnistat) *OmnistatItem {
	if omnistat == nil {
		return nil
	}
	return &OmnistatItem{
		Enabled: omnistat.Enabled, Version: omnistat.Version, InstallMethod: string(omnistat.InstallMethod),
		ObservabilityServerID: omnistat.ObservabilityServerID, State: string(omnistat.State),
		LastWorkflowID: wire.String(omnistat.LastWorkflowID), UpdatedAt: wire.Time(omnistat.UpdatedAt),
	}
}

// toDeviceMetricsExporterItem projects the record.
func toDeviceMetricsExporterItem(exporter *platformdomain.DeviceMetricsExporter) *DeviceMetricsExporterItem {
	if exporter == nil {
		return nil
	}
	return &DeviceMetricsExporterItem{
		Enabled: exporter.Enabled, Version: exporter.Version, InstallMethod: string(exporter.InstallMethod),
		ObservabilityServerID: exporter.ObservabilityServerID, State: string(exporter.State),
		GPUIdentity:    wire.String(string(exporter.GPUIdentity)),
		LastWorkflowID: wire.String(exporter.LastWorkflowID), UpdatedAt: wire.Time(exporter.UpdatedAt),
	}
}

// toMonitoringLinkItems projects links as a non-nil array, so a Platform created before the
// field existed (or a fake repository that never set it) still serializes as [].
func toMonitoringLinkItems(links []platformdomain.MonitoringLink) []MonitoringLinkItem {
	items := make([]MonitoringLinkItem, len(links))
	for i, link := range links {
		items[i] = MonitoringLinkItem{Name: link.Name, URL: link.URL}
	}
	return items
}

func optionalTime(t *time.Time) *string {
	if t == nil {
		return nil
	}
	return wire.TimePtr(*t)
}
