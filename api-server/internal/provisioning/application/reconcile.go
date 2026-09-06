package application

import (
	"context"
	"errors"
	"fmt"
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
}

func NewReconcileUseCase(
	integrations sitedomain.IntegrationRepository,
	servers serverdomain.ServerRepository,
	providers provisioningdomain.ProviderFactory,
) *ReconcileUseCase {
	return &ReconcileUseCase{integrations: integrations, servers: servers, providers: providers}
}

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

	// Which server each machine in this pass ended up owning.
	//
	// Without this, two machines matching the same existing server would both relink it:
	// the first relink leaves the server count unchanged, so the second never sees an
	// ambiguous match and silently steals the same record. A batch of machines sharing
	// one identifier would collapse into a single server, one machine at a time.
	claimed := make(map[string]string, len(machines))

	for _, machine := range machines {
		outcome, conflict, err := uc.project(ctx, integration, machine, claimed)
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
) (projectionOutcome, *Conflict, error) {
	source := serverdomain.Source{
		SiteID:            integration.SiteID,
		IntegrationID:     integration.ID,
		ProviderMachineID: machine.ID,
	}

	existing, err := uc.servers.FindBySource(ctx, source)
	if err == nil {
		apply(existing, source, machine, integration.ID)
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
		return uc.create(ctx, source, machine, integration.ID, claimed)
	}

	candidates, err := uc.servers.FindByHardware(ctx, hardware)
	if err != nil {
		return 0, nil, err
	}

	switch len(candidates) {
	case 0:
		return uc.create(ctx, source, machine, integration.ID, claimed)

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
		apply(candidate, source, machine, integration.ID)
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
) (projectionOutcome, *Conflict, error) {
	now := time.Now().UTC()
	server := &serverdomain.Server{
		ID:        uuid.NewString(),
		CreatedAt: now,
	}
	apply(server, source, machine, integrationID)
	claimed[server.ID] = machine.ID
	return outcomeCreated, nil, uc.servers.Upsert(ctx, server)
}

// clearStaleDeployment recovers a Server whose deployment record no longer reflects
// reality. When a machine is observed back in the provider's Ready pool, any finished
// deployment outcome (succeeded, failed, or canceled) is stale: the machine has no
// installed OS. Such a record only lingers when a run was canceled or ended without the
// release-os step that normally clears it, and it otherwise sticks a "Failed" badge on an
// available machine and confuses recovery. Reconcile clears it so the Server returns to a
// clean, deployable state without any manual data fix. It is a no-op for an active
// deployment (deploying/verifying) or an operator-pending one (requires_attention), so it
// never races an in-flight Operation.
func (uc *ReconcileUseCase) clearStaleDeployment(
	ctx context.Context,
	server *serverdomain.Server,
	machine *provisioningdomain.Machine,
) error {
	if !staleDeploymentOnReady(machine, server.Deployment) {
		return nil
	}
	if err := uc.servers.SetDeployment(ctx, server.ID, nil); err != nil {
		return err
	}
	server.Deployment = nil
	return nil
}

// staleDeploymentOnReady reports whether a deployment record is a finished outcome that a
// machine now back in the provider's Ready pool has outlived.
func staleDeploymentOnReady(machine *provisioningdomain.Machine, deployment *serverdomain.DeploymentStatus) bool {
	if machine == nil || deployment == nil || machine.Status != provisioningdomain.MachineStatusReady {
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
func apply(server *serverdomain.Server, source serverdomain.Source, machine *provisioningdomain.Machine, integrationID string) {
	now := time.Now().UTC()

	server.Source = source
	server.Hardware = hardwareOf(machine)
	// GPUs are carried forward, not set from the machine. They come from the inventory
	// sweep on its own cadence, so a machine listing has none; overwriting Observed
	// wholesale would wipe what the sweep found. The Mongo repository mirrors this by
	// keeping the stored gpus field out of the reconcile Upsert.
	gpus := server.Observed.GPUs
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
		Tags:                 machine.Tags,
	}
	server.Provisioning = &serverdomain.ProvisioningStatus{
		State:               string(machine.Status),
		ProviderState:       machine.ProviderStatus,
		PowerState:          string(machine.PowerState),
		OSSystem:            machine.OSSystem,
		DistroSeries:        machine.DistroSeries,
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
