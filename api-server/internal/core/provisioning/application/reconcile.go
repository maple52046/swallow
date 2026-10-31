package application

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	provisioningdomain "github.com/maple52046/swallow/internal/core/provisioning/domain"
	serverdomain "github.com/maple52046/swallow/internal/core/server/domain"
	sitedomain "github.com/maple52046/swallow/internal/core/site/domain"
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
	// metadata records supplies swallow-owned OS image display names so the mirrored deployed-image
	// name reflects an operator's custom rename, not just the provider catalog title.
	metadataRecords provisioningdomain.OSImageMetadataRepository
}

func NewReconcileUseCase(
	integrations sitedomain.IntegrationRepository,
	servers serverdomain.ServerRepository,
	providers provisioningdomain.ProviderFactory,
	metadataRecords provisioningdomain.OSImageMetadataRepository,
) *ReconcileUseCase {
	return &ReconcileUseCase{
		integrations:    integrations,
		servers:         servers,
		providers:       providers,
		metadataRecords: metadataRecords,
	}
}

// deployedImageNamer resolves the effective deployed image (display name and default user) of a
// machine, or the zero value when the machine is not deployed.
type deployedImageNamer func(machine *provisioningdomain.Machine) deployedImage

// automaticTagResolver returns which of a machine's tags the provisioner computes itself (decision
// 071), given the machine's tags and the automatic subset observed before. Automatic tags are
// observed only; the rest are the manual tags a Server's spec owns.
type automaticTagResolver func(machineTags, previousAutomatic []string) []string

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

// RefreshDeployedImageNames re-mirrors the effective OS image display name and default user onto
// one integration's Server projections so an OS Image metadata write shows on the fleet list and
// detail, and reaches automation, immediately instead of waiting for the next reconcile pass. It
// does a single catalog + metadata read and no provider machine poll — both values are fully
// determined by the metadata and the catalog — then Upserts only the servers whose mirrored values
// actually changed, so each change publishes exactly one server event.
//
// This is the DeployedImageNameRefresher the metadata use case invokes best-effort: a returned
// error means some names could not be refreshed now (the next reconcile still corrects them), so
// the caller must not treat it as a failed metadata write. Absent servers are intentionally
// skipped — they are excluded from the fleet list and Upsert would revive them (it forces
// absent=false) — and are refreshed when the provider next reports them.
func (uc *ReconcileUseCase) RefreshDeployedImageNames(ctx context.Context, integrationID string) error {
	provider, err := uc.providers.For(ctx, integrationID)
	if err != nil {
		return fmt.Errorf("refresh deployed image names: resolve provider for %q: %w", integrationID, err)
	}
	// A catalog read failure must not blank every mirrored name: skip this refresh and let the
	// next reconcile pass recompute once the catalog is readable again.
	resolver, ok := buildDeployedImageResolver(ctx, provider, uc.metadataRecords, integrationID)
	if !ok {
		return fmt.Errorf("refresh deployed image names: read image catalog for %q", integrationID)
	}

	result, err := uc.servers.List(ctx, serverdomain.ListFilter{IntegrationID: integrationID})
	if err != nil {
		return fmt.Errorf("refresh deployed image names: list servers for %q: %w", integrationID, err)
	}

	var errs []error
	for _, server := range result.Servers {
		if server.Provisioning == nil {
			continue
		}
		deployed := server.Provisioning.State == string(provisioningdomain.MachineStatusDeployed)
		image := resolver.resolve(
			server.Provisioning.OSSystem,
			server.Provisioning.DistroSeries,
			server.Observed.Architecture,
			deployed,
		)
		if image.Name == server.Provisioning.DeployedImageName &&
			image.DefaultUser == server.Provisioning.DeployedImageDefaultUser {
			continue
		}
		server.Provisioning.DeployedImageName = image.Name
		server.Provisioning.DeployedImageDefaultUser = image.DefaultUser
		if err := uc.servers.Upsert(ctx, server); err != nil {
			errs = append(errs, fmt.Errorf("server %q: %w", server.ID, err))
		}
	}
	return errors.Join(errs...)
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

	// Classify automatic tags once per pass from the provisioner's tag catalog (one read per
	// integration), so the Server controller knows which observed tags its spec owns.
	resolveAutomaticTags := buildAutomaticTagResolver(ctx, provider)

	// Which server each machine in this pass ended up owning.
	//
	// Without this, two machines matching the same existing server would both relink it:
	// the first relink leaves the server count unchanged, so the second never sees an
	// ambiguous match and silently steals the same record. A batch of machines sharing
	// one identifier would collapse into a single server, one machine at a time.
	claimed := make(map[string]string, len(machines))

	for _, machine := range machines {
		outcome, conflict, err := uc.project(ctx, integration, machine, claimed, resolveImageName, resolveAutomaticTags)
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
	resolveAutomaticTags automaticTagResolver,
) (projectionOutcome, *Conflict, error) {
	source := serverdomain.Source{
		SiteID:            integration.SiteID,
		IntegrationID:     integration.ID,
		ProviderMachineID: machine.ID,
	}

	existing, err := uc.servers.FindBySource(ctx, source)
	if err == nil {
		apply(existing, source, machine, integration.ID, resolveImageName, resolveAutomaticTags)
		claimed[existing.ID] = machine.ID
		if err := uc.servers.Upsert(ctx, existing); err != nil {
			return 0, nil, err
		}
		return outcomeUpdated, nil, uc.clearStaleOSState(ctx, existing, machine)
	}
	if !errors.Is(err, serverdomain.ErrServerNotFound) {
		return 0, nil, err
	}

	hardware := hardwareOf(machine)
	if hardware.Empty() {
		// Nothing to match on, so this can only be a new server. A machine with no
		// usable hardware identifiers is normal before commissioning, and common on
		// virtual machines whose firmware reports placeholders.
		return uc.create(ctx, source, machine, integration.ID, claimed, resolveImageName, resolveAutomaticTags)
	}

	candidates, err := uc.servers.FindByHardware(ctx, hardware)
	if err != nil {
		return 0, nil, err
	}

	switch len(candidates) {
	case 0:
		return uc.create(ctx, source, machine, integration.ID, claimed, resolveImageName, resolveAutomaticTags)

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
		apply(candidate, source, machine, integration.ID, resolveImageName, resolveAutomaticTags)
		claimed[candidate.ID] = machine.ID
		if err := uc.servers.Upsert(ctx, candidate); err != nil {
			return 0, nil, err
		}
		return outcomeRelinked, nil, uc.clearStaleOSState(ctx, candidate, machine)

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
	resolveAutomaticTags automaticTagResolver,
) (projectionOutcome, *Conflict, error) {
	now := time.Now().UTC()
	server := &serverdomain.Server{
		ID:        uuid.NewString(),
		Origin:    originOf(machine),
		CreatedAt: now,
	}
	apply(server, source, machine, integrationID, resolveImageName, resolveAutomaticTags)
	claimed[server.ID] = machine.ID
	return outcomeCreated, nil, uc.servers.Upsert(ctx, server)
}

// deployedImage is what swallow mirrors onto a Server about its currently deployed OS image: the
// effective display name and the effective default login user (decision 039).
type deployedImage struct {
	Name        string
	DefaultUser string
}

// deployedImageResolver maps a deployed item's observed OSSystem/DistroSeries and CPU architecture
// to the effective deployed image: the provider catalog title overlaid with any swallow custom
// name, and the metadata default user or the built-in for the provider OS family. It is built once
// from a single catalog + metadata read so the same lookup serves both a reconcile pass (per
// machine) and the eager refresh metadata write triggers (per Server projection), never a
// per-item provider round trip.
//
// The lookup tolerates MAAS reporting a machine's architecture with a subarch
// (e.g. "amd64/generic") while the catalog lists only the primary arch (e.g. "amd64"): keys
// use the primary arch on both sides, with a no-arch fallback for the rare catalog whose
// names differ only by subarch. A zero-value resolver (empty maps) is what a failed catalog read
// degrades to: it resolves no name, and the default user falls back to the built-in for the
// observed OS family so automation keeps a login user for synced images during a catalog outage.
type deployedImageResolver struct {
	byIDArch        map[string]deployedImage
	byOSReleaseArch map[string]deployedImage
	byID            map[string]deployedImage
}

// resolve returns the effective deployed image for the OS image identified by the observed
// OSSystem/DistroSeries and CPU architecture. It returns the zero value when the item is not
// deployed, so an item in any other lifecycle state is never labeled with whatever it last ran; a
// deployed item that cannot be matched to the catalog gets no name and the built-in default user.
func (r deployedImageResolver) resolve(osSystem, distroSeries, architecture string, deployed bool) deployedImage {
	if !deployed {
		return deployedImage{}
	}
	// A synced image's catalog ID is "<osSystem>/<distroSeries>"; match that first, then fall
	// back to matching the OS family and release, and finally the ID without arch.
	imageID := osSystem + "/" + distroSeries
	arch := primaryArch(architecture)
	if image, ok := r.byIDArch[imageArchKey(imageID, arch)]; ok {
		return image
	}
	if image, ok := r.byOSReleaseArch[osReleaseArchKey(osSystem, distroSeries, arch)]; ok {
		return image
	}
	if image, ok := r.byID[imageID]; ok {
		return image
	}
	return deployedImage{DefaultUser: provisioningdomain.BuiltinDefaultUser(osSystem)}
}

// buildDeployedImageResolver reads one integration's image catalog and metadata set once and
// returns a resolver from an observed OSSystem/DistroSeries/architecture to the effective deployed
// image. The bool reports whether the catalog could be read: on a read failure it returns a
// zero-value resolver and false, so a mirrored name stays empty rather than aborting the caller.
// The metadata read is best effort — a failure there means no custom names or default users this
// pass, not a failure.
//
// It is a package function (not a ReconcileUseCase method) so the single-server RefreshServer path
// can reuse the exact same resolution at deploy completion, filling the mirrored values
// immediately instead of only on the next reconcile pass. Callers must pass a non-nil metadata
// repository.
func buildDeployedImageResolver(
	ctx context.Context,
	provider provisioningdomain.OSProvisioningProvider,
	metadataRecords provisioningdomain.OSImageMetadataRepository,
	integrationID string,
) (deployedImageResolver, bool) {
	images, err := provider.ListOSImages(ctx)
	if err != nil {
		return deployedImageResolver{}, false
	}

	byImage := map[string]*provisioningdomain.OSImageMetadata{}
	if list, err := metadataRecords.ListByIntegration(ctx, integrationID); err == nil {
		for _, metadata := range list {
			byImage[imageArchKey(metadata.ImageID, metadata.Architecture)] = metadata
		}
	}

	resolver := deployedImageResolver{
		byIDArch:        make(map[string]deployedImage, len(images)),
		byOSReleaseArch: make(map[string]deployedImage, len(images)),
		byID:            make(map[string]deployedImage, len(images)),
	}
	for _, image := range images {
		effective := deployedImage{Name: image.Name}
		metadataDefaultUser := ""
		if metadata, ok := byImage[imageArchKey(image.ID, image.Architecture)]; ok {
			if metadata.DisplayName != "" {
				effective.Name = metadata.DisplayName
			}
			metadataDefaultUser = metadata.DefaultUser
		}
		// The built-in keys off the provider OS family, never metadata label, so relabeling an
		// image for display cannot change which account automation logs in as.
		effective.DefaultUser = provisioningdomain.EffectiveDefaultUser(metadataDefaultUser, image.OSSystem)
		arch := primaryArch(image.Architecture)
		resolver.byIDArch[imageArchKey(image.ID, arch)] = effective
		resolver.byOSReleaseArch[osReleaseArchKey(image.OSSystem, image.Release, arch)] = effective
		resolver.byID[image.ID] = effective
	}
	return resolver, true
}

// buildDeployedImageNamer adapts the shared resolver to the per-machine lookup the reconcile pass
// applies, so a renamed image shows its swallow name and an image's default user reaches every
// server deployed with it; a catalog read failure degrades to no name (and the built-in default
// user) for every machine until a later pass.
func (uc *ReconcileUseCase) buildDeployedImageNamer(
	ctx context.Context,
	provider provisioningdomain.OSProvisioningProvider,
	integrationID string,
) deployedImageNamer {
	resolver, _ := buildDeployedImageResolver(ctx, provider, uc.metadataRecords, integrationID)
	return func(machine *provisioningdomain.Machine) deployedImage {
		return resolver.resolve(
			machine.OSSystem,
			machine.DistroSeries,
			machine.Architecture,
			machine.Status == provisioningdomain.MachineStatusDeployed,
		)
	}
}

// buildAutomaticTagResolver reads the provisioner's tag catalog once per pass and returns a
// resolver that picks the automatic tags (MAAS tags with a definition) out of a machine's tags. A
// provisioner without tagging has no automatic tags. A catalog read failure degrades to the
// previously observed classification for this pass rather than failing the reconcile, so a
// transient error never makes an automatic tag look manual (which the controller would remove).
func buildAutomaticTagResolver(ctx context.Context, provider provisioningdomain.OSProvisioningProvider) automaticTagResolver {
	controller, ok := provider.(provisioningdomain.MachineTagController)
	if !ok || !provider.Capabilities().Tagging {
		return func([]string, []string) []string { return nil }
	}
	catalog, err := controller.ListTags(ctx)
	if err != nil {
		return func(machineTags, previous []string) []string { return intersectTags(machineTags, previous) }
	}
	automatic := map[string]bool{}
	for _, tag := range catalog {
		if !tag.Editable {
			automatic[tag.Name] = true
		}
	}
	return func(machineTags, _ []string) []string {
		out := make([]string, 0)
		for _, tag := range machineTags {
			if automatic[tag] {
				out = append(out, tag)
			}
		}
		return out
	}
}

// intersectTags returns the tags of a that are also in b, in a's order.
func intersectTags(a, b []string) []string {
	out := make([]string, 0)
	for _, tag := range a {
		for _, other := range b {
			if tag == other {
				out = append(out, tag)
				break
			}
		}
	}
	return out
}

// originOf infers how a machine seen for the first time arrived (decision 071): one already
// deployed was registered with its existing OS (imported); anything else was enlisted by the
// provisioner (discovered). Enrollment by swallow records its own origin afterwards.
func originOf(machine *provisioningdomain.Machine) serverdomain.Origin {
	if machine.Status == provisioningdomain.MachineStatusDeployed {
		return serverdomain.OriginImported
	}
	return serverdomain.OriginDiscovered
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

// clearStaleOSState drops the swallow-owned facts that describe an OS installation the machine no
// longer has: a finished deployment record and an operator-set Server Default User.
func (uc *ReconcileUseCase) clearStaleOSState(
	ctx context.Context,
	server *serverdomain.Server,
	machine *provisioningdomain.Machine,
) error {
	if err := uc.clearStaleDeployment(ctx, server, machine); err != nil {
		return err
	}
	return uc.clearDefaultUserWithoutOS(ctx, server, machine)
}

// clearDefaultUserWithoutOS clears an operator-set Server Default User (decision 045) once the
// machine is observed with no OS — `ready` (released) or `allocated` (reserved, nothing running).
// The account it names belonged to the previous installation, whether that was released through
// swallow or directly in the provisioner; keeping it would make automation log in to the next OS as
// an account that may not exist there. Every other state, including `deployed` and `rescue`, keeps
// it, because the installed OS is still on the machine.
func (uc *ReconcileUseCase) clearDefaultUserWithoutOS(
	ctx context.Context,
	server *serverdomain.Server,
	machine *provisioningdomain.Machine,
) error {
	if server.DefaultUser == "" || machine == nil {
		return nil
	}
	switch machine.Status {
	case provisioningdomain.MachineStatusReady, provisioningdomain.MachineStatusAllocated:
	default:
		return nil
	}
	if err := uc.servers.SetDefaultUser(ctx, server.ID, ""); err != nil {
		return err
	}
	server.DefaultUser = ""
	return nil
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
func apply(server *serverdomain.Server, source serverdomain.Source, machine *provisioningdomain.Machine, integrationID string, resolveImageName deployedImageNamer, resolveAutomaticTags automaticTagResolver) {
	now := time.Now().UTC()

	server.Source = source
	server.Hardware = hardwareOf(machine)
	// GPUs are carried forward, not set from the machine. They come from the inventory
	// sweep on its own cadence, so a machine listing has none; overwriting Observed
	// wholesale would wipe what the sweep found. The Mongo repository mirrors this by
	// keeping the stored gpus field out of the reconcile Upsert.
	gpus := server.Observed.GPUs
	// Tags are observed as the provisioner reports them; the automatic subset is classified so the
	// controller converges only the manual tags the spec owns (decision 071).
	automaticTags := resolveAutomaticTags(machine.Tags, server.Observed.AutomaticTags)
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
		AutomaticTags:        automaticTags,
	}
	image := resolveImageName(machine)
	server.Provisioning = &serverdomain.ProvisioningStatus{
		State:                    string(machine.Status),
		StateSince:               serverdomain.NextStateSince(server.Provisioning, string(machine.Status), now),
		ProviderState:            machine.ProviderStatus,
		ErrorDescription:         machine.ErrorDescription,
		PowerState:               string(machine.PowerState),
		OSSystem:                 machine.OSSystem,
		DistroSeries:             machine.DistroSeries,
		DeployedImageName:        image.Name,
		DeployedImageDefaultUser: image.DefaultUser,
		Ephemeral:                projectedEphemeral(machine),
		HWEKernel:                machine.HWEKernel,
		Locked:                   machine.Locked,
		InspectionRequired:       machine.InspectionRequired,
		InspectionRequiredReason: machine.InspectionRequiredReason,
		CommissioningStatus:      machine.CommissioningStatus,
		TestingStatus:            machine.TestingStatus,
		IntegrationID:            integrationID,
		ObservedAt:               now,
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
