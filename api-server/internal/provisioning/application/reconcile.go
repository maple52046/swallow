package application

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	provisioningdomain "github.com/maple52046/swallow/internal/provisioning/domain"
	serverdomain "github.com/maple52046/swallow/internal/server/domain"
	sitedomain "github.com/maple52046/swallow/internal/site/domain"
)

// ReconcileReport summarises one pass over one provisioner.
type ReconcileReport struct {
	IntegrationID   string     `json:"integrationId"`
	IntegrationName string     `json:"integrationName"`
	SiteID          string     `json:"siteId"`
	Machines        int        `json:"machines"`
	Created         int        `json:"created"`
	Updated         int        `json:"updated"`
	Relinked        int        `json:"relinked"`
	MarkedAbsent    int        `json:"markedAbsent"`
	Conflicts       []Conflict `json:"conflicts"`
	Error           *string    `json:"error"`
}

// Conflict is a machine the reconciler refused to act on.
//
// Conflicts are reported rather than resolved. Guessing which existing server a machine
// belongs to risks merging two distinct servers, which cannot be undone; leaving the
// machine unprojected is recoverable by an operator.
type Conflict struct {
	ProviderMachineID  string   `json:"providerMachineId"`
	Hostname           string   `json:"hostname"`
	Reason             string   `json:"reason"`
	CandidateServerIDs []string `json:"candidateServerIds"`
}

// ReconcileUseCase projects a provisioner's machine inventory onto server records.
//
// This is the only way a server comes into being. It spans two contexts on purpose:
// reading machines is a provisioning concern, server identity is a server concern, and
// deciding that a machine is a particular server is the workflow between them.
type ReconcileUseCase struct {
	integrations sitedomain.IntegrationRepository
	servers      serverdomain.ServerRepository
	providers    provisioningdomain.ProviderFactory
	// overlays supplies swallow-owned OS image display names so the mirrored deployed-image
	// name reflects an operator's custom rename, not just the provider catalog title.
	overlays provisioningdomain.OSImageOverlayRepository
	// tagOverlays supplies swallow-owned Server tags for the capability-first fallback: when a
	// provisioner cannot own tags, these are unioned into Observed.Tags at projection so every
	// consumer sees the owned tags. Inert while the provisioner is tagging-capable (decision 031).
	tagOverlays provisioningdomain.ServerTagOverlayRepository
}

func NewReconcileUseCase(
	integrations sitedomain.IntegrationRepository,
	servers serverdomain.ServerRepository,
	providers provisioningdomain.ProviderFactory,
	overlays provisioningdomain.OSImageOverlayRepository,
	tagOverlays provisioningdomain.ServerTagOverlayRepository,
) *ReconcileUseCase {
	return &ReconcileUseCase{
		integrations: integrations,
		servers:      servers,
		providers:    providers,
		overlays:     overlays,
		tagOverlays:  tagOverlays,
	}
}

// deployedImageNamer resolves the effective display name of a machine's currently deployed OS
// image, or "" when the machine is not deployed or the image cannot be matched to the catalog.
type deployedImageNamer func(machine *provisioningdomain.Machine) string

// ownedTagResolver returns the swallow-owned tags for a Server id, or nil when there are none.
// It is the read side of the tag fallback (decision 031): non-empty only when the provisioner is
// not tagging-capable, so a capable provisioner (MAAS) yields a resolver that always returns nil.
type ownedTagResolver func(serverID string) []string

// ExecuteAll reconciles every enabled provisioner.
//
// One provisioner failing does not stop the others: a fleet spans sites that fail
// independently, and a single unreachable MAAS must not freeze the whole projection.
func (uc *ReconcileUseCase) ExecuteAll(ctx context.Context) ([]ReconcileReport, error) {
	integrations, err := uc.integrations.List(ctx, sitedomain.IntegrationFilter{
		Kind:        sitedomain.IntegrationKindProvisioner,
		EnabledOnly: true,
	})
	if err != nil {
		return nil, err
	}

	reports := make([]ReconcileReport, 0, len(integrations))
	for _, integration := range integrations {
		report := uc.reconcile(ctx, integration)
		reports = append(reports, report)
	}
	return reports, nil
}

// Execute reconciles one provisioner by ID.
func (uc *ReconcileUseCase) Execute(ctx context.Context, integrationID string) (*ReconcileReport, error) {
	integration, err := uc.integrations.FindByID(ctx, integrationID)
	if err != nil {
		return nil, err
	}
	if integration.Kind != sitedomain.IntegrationKindProvisioner {
		return nil, fmt.Errorf("%w: %q is registered as %q",
			provisioningdomain.ErrIntegrationNotProvisioner, integration.Name, integration.Kind)
	}

	report := uc.reconcile(ctx, integration)
	return &report, nil
}

func (uc *ReconcileUseCase) reconcile(ctx context.Context, integration *sitedomain.Integration) ReconcileReport {
	report := ReconcileReport{
		IntegrationID:   integration.ID,
		IntegrationName: integration.Name,
		SiteID:          integration.SiteID,
		Conflicts:       []Conflict{},
	}

	startedAt := time.Now().UTC()
	uc.recordSyncStart(ctx, integration, startedAt)

	provider, err := uc.providers.For(ctx, integration.ID)
	if err != nil {
		return uc.recordSyncFailure(ctx, integration, report, startedAt, err)
	}

	machines, err := provider.ListMachines(ctx, provisioningdomain.MachineFilter{})
	if err != nil {
		return uc.recordSyncFailure(ctx, integration, report, startedAt, err)
	}
	report.Machines = len(machines)

	// Resolve the deployed-image name once per pass (one catalog read per integration, not per
	// machine and never per API request) so the fleet list can show a meaningful image name
	// without a per-request provider fan-out. A catalog read failure degrades to no names for
	// this pass rather than failing the whole reconcile.
	resolveImageName := uc.buildDeployedImageNamer(ctx, provider, integration.ID)

	// Resolve swallow-owned tags once per pass for the capability-first fallback: only a
	// provisioner that cannot own tags contributes owned tags, so a capable provisioner (MAAS)
	// makes this a no-op that reads nothing (decision 031).
	resolveOwnedTags := uc.buildOwnedTagResolver(ctx, provider, integration.ID)

	// Which server each machine in this pass ended up owning.
	//
	// Without this, two machines matching the same existing server would both relink it:
	// the first relink leaves the server count unchanged, so the second never sees an
	// ambiguous match and silently steals the same record. A batch of machines sharing
	// one identifier would collapse into a single server, one machine at a time.
	claimed := make(map[string]string, len(machines))

	for _, machine := range machines {
		outcome, conflict, err := uc.project(ctx, integration, machine, claimed, resolveImageName, resolveOwnedTags)
		if err != nil {
			return uc.recordSyncFailure(ctx, integration, report, startedAt, err)
		}
		if conflict != nil {
			report.Conflicts = append(report.Conflicts, *conflict)
			continue
		}
		switch outcome {
		case outcomeCreated:
			report.Created++
		case outcomeUpdated:
			report.Updated++
		case outcomeRelinked:
			report.Relinked++
		}
	}

	// Sweep by timestamp: anything this pass did not touch is no longer reported by
	// the provisioner. Absent, not deleted — absence is usually transient.
	absentIDs, err := uc.servers.MarkAbsent(ctx, integration.ID, startedAt)
	if err != nil {
		return uc.recordSyncFailure(ctx, integration, report, startedAt, err)
	}
	report.MarkedAbsent = len(absentIDs)

	uc.recordSyncSuccess(ctx, integration, startedAt)
	return report
}

type projectionOutcome int

const (
	outcomeCreated projectionOutcome = iota
	outcomeUpdated
	outcomeRelinked
)

// project maps one machine onto a server, following the matching rules in
// docs/decisions/002-server-identity.md.
func (uc *ReconcileUseCase) project(
	ctx context.Context,
	integration *sitedomain.Integration,
	machine *provisioningdomain.Machine,
	claimed map[string]string,
	resolveImageName deployedImageNamer,
	resolveOwnedTags ownedTagResolver,
) (projectionOutcome, *Conflict, error) {
	source := serverdomain.Source{
		SiteID:            integration.SiteID,
		IntegrationID:     integration.ID,
		ProviderMachineID: machine.ID,
	}

	existing, err := uc.servers.FindBySource(ctx, source)
	if err == nil {
		apply(existing, source, machine, integration.ID, resolveImageName, resolveOwnedTags)
		claimed[existing.ID] = machine.ID
		if err := uc.servers.Upsert(ctx, existing); err != nil {
			return 0, nil, err
		}
		return outcomeUpdated, nil, uc.clearStaleDeployment(ctx, existing, machine)
	}
	if !errors.Is(err, serverdomain.ErrServerNotFound) {
		return 0, nil, err
	}

	hardware := hardwareOf(machine)
	if hardware.Empty() {
		// Nothing to match on, so this can only be a new server. A machine with no
		// usable hardware identifiers is normal before commissioning, and common on
		// virtual machines whose firmware reports placeholders.
		return uc.create(ctx, source, machine, integration.ID, claimed, resolveImageName, resolveOwnedTags)
	}

	candidates, err := uc.servers.FindByHardware(ctx, hardware)
	if err != nil {
		return 0, nil, err
	}

	switch len(candidates) {
	case 0:
		return uc.create(ctx, source, machine, integration.ID, claimed, resolveImageName, resolveOwnedTags)

	case 1:
		candidate := candidates[0]
		if owner, taken := claimed[candidate.ID]; taken {
			return 0, &Conflict{
				ProviderMachineID: machine.ID,
				Hostname:          machine.Hostname,
				Reason: fmt.Sprintf(
					"hardware identifiers also match machine %q, which already claimed this server in this pass; "+
						"the two machines are reporting the same identifiers", owner),
				CandidateServerIDs: []string{candidate.ID},
			}, nil
		}
		if conflict := relinkConflict(candidate, integration, machine); conflict != nil {
			return 0, conflict, nil
		}
		apply(candidate, source, machine, integration.ID, resolveImageName, resolveOwnedTags)
		claimed[candidate.ID] = machine.ID
		if err := uc.servers.Upsert(ctx, candidate); err != nil {
			return 0, nil, err
		}
		return outcomeRelinked, nil, uc.clearStaleDeployment(ctx, candidate, machine)

	default:
		ids := make([]string, 0, len(candidates))
		for _, candidate := range candidates {
			ids = append(ids, candidate.ID)
		}
		return 0, &Conflict{
			ProviderMachineID:  machine.ID,
			Hostname:           machine.Hostname,
			Reason:             "hardware identifiers match more than one existing server; refusing to guess which one this is",
			CandidateServerIDs: ids,
		}, nil
	}
}

// relinkConflict decides whether a single hardware match may be re-pointed at this
// machine, or whether the situation needs an operator.
//
// Re-enrollment inside one provisioner is normal and always safe to relink. Across
// provisioners it is only safe when the other one has stopped reporting the machine;
// if both still report it, relinking would make two reconcilers fight over the same
// server on every pass.
func relinkConflict(
	candidate *serverdomain.Server,
	integration *sitedomain.Integration,
	machine *provisioningdomain.Machine,
) *Conflict {
	if candidate.Source.IntegrationID == integration.ID {
		return nil
	}
	if candidate.Absent {
		return nil
	}
	return &Conflict{
		ProviderMachineID:  machine.ID,
		Hostname:           machine.Hostname,
		Reason:             "the same hardware is still being reported by another provisioner; remove it from one of them before it can be projected here",
		CandidateServerIDs: []string{candidate.ID},
	}
}

func (uc *ReconcileUseCase) create(
	ctx context.Context,
	source serverdomain.Source,
	machine *provisioningdomain.Machine,
	integrationID string,
	claimed map[string]string,
	resolveImageName deployedImageNamer,
	resolveOwnedTags ownedTagResolver,
) (projectionOutcome, *Conflict, error) {
	now := time.Now().UTC()
	server := &serverdomain.Server{
		ID:        uuid.NewString(),
		CreatedAt: now,
	}
	apply(server, source, machine, integrationID, resolveImageName, resolveOwnedTags)
	claimed[server.ID] = machine.ID
	return outcomeCreated, nil, uc.servers.Upsert(ctx, server)
}

// buildDeployedImageNamer reads the integration's image catalog once and returns a resolver
// from a machine's observed OSSystem/DistroSeries to the effective image display name. The
// effective name is the provider's catalog title overlaid with any swallow custom name, so a
// renamed image shows its swallow name on every server deployed with it.
//
// Failures degrade rather than abort: if the catalog cannot be read this pass, the resolver
// returns "" for every machine, and the mirrored name simply stays empty until a later pass.
func (uc *ReconcileUseCase) buildDeployedImageNamer(
	ctx context.Context,
	provider provisioningdomain.OSProvisioningProvider,
	integrationID string,
) deployedImageNamer {
	images, err := provider.ListOSImages(ctx)
	if err != nil {
		return func(*provisioningdomain.Machine) string { return "" }
	}

	// Overlay lookup is best effort: a failure here means no custom names this pass, not a
	// failed reconcile. Only the display-name override matters for the deployed-image label.
	custom := map[string]string{}
	if overlays, err := uc.overlays.ListByIntegration(ctx, integrationID); err == nil {
		for _, overlay := range overlays {
			if overlay.DisplayName != "" {
				custom[imageArchKey(overlay.ImageID, overlay.Architecture)] = overlay.DisplayName
			}
		}
	}

	// Matching a machine to a catalog image must tolerate MAAS reporting a machine's
	// architecture with a subarch (e.g. "amd64/generic") while the image lists only the
	// primary arch (e.g. "amd64"). Keys therefore use the primary arch on both sides, plus a
	// no-arch fallback for the rare catalog whose names differ only by subarch.
	byIDArch := make(map[string]string, len(images))
	byOSReleaseArch := make(map[string]string, len(images))
	byID := make(map[string]string, len(images))
	for _, image := range images {
		effective := image.Name
		if name, ok := custom[imageArchKey(image.ID, image.Architecture)]; ok {
			effective = name
		}
		arch := primaryArch(image.Architecture)
		byIDArch[imageArchKey(image.ID, arch)] = effective
		byOSReleaseArch[osReleaseArchKey(image.OSSystem, image.Release, arch)] = effective
		byID[image.ID] = effective
	}

	return func(machine *provisioningdomain.Machine) string {
		// Only a deployed machine has a meaningful "deployed image"; other lifecycle states
		// would otherwise mislabel a machine with whatever it last ran.
		if machine.Status != provisioningdomain.MachineStatusDeployed {
			return ""
		}
		// A synced image's catalog ID is "<osSystem>/<distroSeries>"; match that first, then
		// fall back to matching the OS family and release, and finally the ID without arch.
		imageID := machine.OSSystem + "/" + machine.DistroSeries
		arch := primaryArch(machine.Architecture)
		if name, ok := byIDArch[imageArchKey(imageID, arch)]; ok {
			return name
		}
		if name, ok := byOSReleaseArch[osReleaseArchKey(machine.OSSystem, machine.DistroSeries, arch)]; ok {
			return name
		}
		if name, ok := byID[imageID]; ok {
			return name
		}
		return ""
	}
}

// buildOwnedTagResolver reads the swallow-owned tag overlays for one integration once per pass and
// returns a resolver from a Server id to its owned tags. It is the read side of the capability-first
// tag fallback (decision 031): a provisioner that advertises Tagging owns the tags, so no overlay is
// merged and the resolver is a cheap no-op that reads nothing; only a non-capable provisioner has
// owned tags to union in. A failure reading the overlays degrades to no owned tags this pass rather
// than failing the whole reconcile, matching how the image-name overlay lookup degrades.
func (uc *ReconcileUseCase) buildOwnedTagResolver(
	ctx context.Context,
	provider provisioningdomain.OSProvisioningProvider,
	integrationID string,
) ownedTagResolver {
	if provider.Capabilities().Tagging {
		return func(string) []string { return nil }
	}
	byServer := map[string][]string{}
	if overlays, err := uc.tagOverlays.ListByIntegration(ctx, integrationID); err == nil {
		for _, overlay := range overlays {
			byServer[overlay.ServerID] = overlay.Tags
		}
	}
	return func(serverID string) []string { return byServer[serverID] }
}

// unionTags merges owned tags into the provider-observed tags, preserving order and dropping
// duplicates. Provider tags come first because, for a capable provisioner, they are the source of
// truth; for the fallback the provider reports none, so the result is just the owned set.
func unionTags(providerTags, ownedTags []string) []string {
	if len(ownedTags) == 0 {
		return providerTags
	}
	seen := make(map[string]struct{}, len(providerTags)+len(ownedTags))
	merged := make([]string, 0, len(providerTags)+len(ownedTags))
	for _, tag := range providerTags {
		if _, dup := seen[tag]; dup {
			continue
		}
		seen[tag] = struct{}{}
		merged = append(merged, tag)
	}
	for _, tag := range ownedTags {
		if _, dup := seen[tag]; dup {
			continue
		}
		seen[tag] = struct{}{}
		merged = append(merged, tag)
	}
	return merged
}

// imageArchKey and osReleaseArchKey join their parts with a separator that cannot appear in the
// values, so distinct tuples never collide in the resolver maps.
func imageArchKey(imageID, architecture string) string {
	return imageID + "\x00" + architecture
}

func osReleaseArchKey(osSystem, release, architecture string) string {
	return osSystem + "\x00" + release + "\x00" + architecture
}

// primaryArch drops a MAAS subarch suffix ("amd64/generic" -> "amd64") so a machine's reported
// architecture matches an image catalog entry that lists only the primary architecture.
func primaryArch(architecture string) string {
	if index := strings.IndexByte(architecture, '/'); index >= 0 {
		return architecture[:index]
	}
	return architecture
}

// clearStaleDeployment recovers a Server whose deployment record no longer reflects
// reality. When a machine is observed back in a not-deployed provider state — `ready`
// (released to the pool) or `allocated` (reserved but carrying no running OS) — any
// finished deployment outcome (succeeded, failed, or canceled) is stale: the machine has
// no installed OS. Such a record only lingers when a run was canceled, failed, or ended
// without the release-os step that normally clears it, and it otherwise paints a stale
// "Deployed"/"Failed" badge on an available machine and confuses recovery. Reconcile clears
// it so the Server returns to a clean, deployable state without any manual data fix. It is a
// no-op for an active deployment (deploying/verifying) or an operator-pending one
// (requires_attention), so it never races an in-flight Operation.
func (uc *ReconcileUseCase) clearStaleDeployment(
	ctx context.Context,
	server *serverdomain.Server,
	machine *provisioningdomain.Machine,
) error {
	if !staleFinishedDeployment(machine, server.Deployment) {
		return nil
	}
	if err := uc.servers.SetDeployment(ctx, server.ID, nil); err != nil {
		return err
	}
	server.Deployment = nil
	return nil
}

// staleFinishedDeployment reports whether a deployment record is a finished outcome that a
// machine now back in a not-deployed provider state has outlived. Only `ready` and
// `allocated` qualify: a machine actually running an OS reports `deployed`, so clearing on
// those two never erases a live result, while it does clear the leftover record that would
// otherwise mask a reserved/available machine as still deployed. Only terminal deployment
// states are cleared, so an in-flight deploy (deploying/verifying/requires_attention) is
// never touched.
func staleFinishedDeployment(machine *provisioningdomain.Machine, deployment *serverdomain.DeploymentStatus) bool {
	if machine == nil || deployment == nil {
		return false
	}
	switch machine.Status {
	case provisioningdomain.MachineStatusReady, provisioningdomain.MachineStatusAllocated:
	default:
		return false
	}
	switch deployment.State {
	case serverdomain.DeploymentSucceeded,
		serverdomain.DeploymentFailed,
		serverdomain.DeploymentCanceled:
		return true
	default:
		return false
	}
}

// apply copies a machine's observed state onto a server projection.
//
// It never touches the membership axis, which belongs to the platform context, nor
// CreatedAt, which belongs to whoever created the record.
func apply(server *serverdomain.Server, source serverdomain.Source, machine *provisioningdomain.Machine, integrationID string, resolveImageName deployedImageNamer, resolveOwnedTags ownedTagResolver) {
	now := time.Now().UTC()

	server.Source = source
	server.Hardware = hardwareOf(machine)
	// GPUs are carried forward, not set from the machine. They come from the inventory
	// sweep on its own cadence, so a machine listing has none; overwriting Observed
	// wholesale would wipe what the sweep found. The Mongo repository mirrors this by
	// keeping the stored gpus field out of the reconcile Upsert.
	gpus := server.Observed.GPUs
	// Effective tags are the provider's tags for a tagging-capable provisioner, or those plus the
	// swallow-owned overlay for one that is not (decision 031). unionTags is a no-op when the
	// resolver returns nothing, so a capable provisioner keeps exactly the provider's tags.
	tags := unionTags(machine.Tags, resolveOwnedTags(server.ID))
	server.Observed = serverdomain.Observed{
		Hostname:             machine.Hostname,
		FQDN:                 machine.FQDN,
		Addresses:            machine.IPAddresses,
		Architecture:         machine.Architecture,
		CPUCores:             machine.CPUCores,
		MemoryMiB:            machine.MemoryMiB,
		StorageGB:            machine.StorageGB,
		GPUs:                 gpus,
		ProviderZone:         machine.Zone,
		ProviderResourcePool: machine.ResourcePool,
		SystemVendor:         machine.SystemVendor,
		SystemProduct:        machine.SystemProduct,
		CPUModel:             machine.CPUModel,
		ProviderPod:          machine.Pod,
		Tags:                 tags,
	}
	server.Provisioning = &serverdomain.ProvisioningStatus{
		State:               string(machine.Status),
		ProviderState:       machine.ProviderStatus,
		ErrorDescription:    machine.ErrorDescription,
		PowerState:          string(machine.PowerState),
		OSSystem:            machine.OSSystem,
		DistroSeries:        machine.DistroSeries,
		DeployedImageName:   resolveImageName(machine),
		Ephemeral:           projectedEphemeral(machine),
		HWEKernel:           machine.HWEKernel,
		Locked:              machine.Locked,
		CommissioningStatus: machine.CommissioningStatus,
		TestingStatus:       machine.TestingStatus,
		IntegrationID:       integrationID,
		ObservedAt:          now,
	}
	server.Absent = false
	server.LastSeenAt = now
	server.UpdatedAt = now
}

// hardwareOf normalizes as it converts, so that firmware placeholders never reach
// storage or a matching query.
func hardwareOf(machine *provisioningdomain.Machine) serverdomain.Hardware {
	return serverdomain.Hardware{
		SystemUUID:   machine.SystemUUID,
		SerialNumber: machine.SerialNumber,
		MACAddresses: machine.MACAddresses,
	}.Normalized()
}

func (uc *ReconcileUseCase) recordSyncStart(ctx context.Context, integration *sitedomain.Integration, startedAt time.Time) {
	state := integration.Sync
	state.LastStartedAt = &startedAt
	// Sync state is observability, not the job: failing to record it must not abort
	// the reconcile it describes.
	_ = uc.integrations.UpdateSyncState(ctx, integration.ID, state)
}

func (uc *ReconcileUseCase) recordSyncSuccess(ctx context.Context, integration *sitedomain.Integration, startedAt time.Time) {
	succeededAt := time.Now().UTC()
	_ = uc.integrations.UpdateSyncState(ctx, integration.ID, sitedomain.SyncState{
		LastStartedAt:   &startedAt,
		LastSucceededAt: &succeededAt,
	})
}

// recordSyncFailure keeps the previous success timestamp, so that a reader can see both
// that the last attempt failed and how old the data they are looking at is.
func (uc *ReconcileUseCase) recordSyncFailure(
	ctx context.Context,
	integration *sitedomain.Integration,
	report ReconcileReport,
	startedAt time.Time,
	cause error,
) ReconcileReport {
	message := cause.Error()
	report.Error = &message

	_ = uc.integrations.UpdateSyncState(ctx, integration.ID, sitedomain.SyncState{
		LastStartedAt:   &startedAt,
		LastSucceededAt: integration.Sync.LastSucceededAt,
		LastError:       message,
	})
	return report
}
