// Package application holds the Managed Software use cases: install, uninstall, list assignments,
// and read the catalog. It composes the existing Workflow engine through a launcher port rather than owning
// any orchestration, keeping software deployment aligned with platform deployment
// (docs/development/software-deployment.md, decision 038).
package application

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	platformdomain "github.com/maple52046/swallow/internal/core/platform/domain"
	serverdomain "github.com/maple52046/swallow/internal/core/server/domain"
	softwaredomain "github.com/maple52046/swallow/internal/core/software/domain"
	"github.com/maple52046/swallow/internal/core/store"
	workflowdomain "github.com/maple52046/swallow/internal/core/workflow/domain"
)

// ErrInvalidRequest wraps every request-shape validation failure so delivery maps it to 400.
// State and uniqueness conflicts use the domain sentinels (ErrMutuallyExclusive,
// ErrKubernetesMember) or the operation layer's busy/locked errors surfaced by the launcher.
var ErrInvalidRequest = errors.New("invalid software request")

// SoftwareLaunch is the trusted, server-side intent handed to the launcher. Roles are per
// serverId; TrustedVars are the assembled swallow_-prefixed variables the playbook consumes and
// the client cannot forge.
type SoftwareLaunch struct {
	Kind softwaredomain.Kind
	// Label is the kind's user-facing name, for the Workflow's intent and Task names.
	Label         string
	ServerIDs     []string
	RolesByServer map[string][]softwaredomain.Role
	TrustedVars   map[string]any
	RequestedBy   string
}

// SoftwareLauncher composes the software Workflow (prepare-hosts -> configure-<kind> ->
// record-software). It is implemented in the app layer so the software context does not import the
// operation orchestration internals directly.
//
// It is split so an install or uninstall is accepted in one store transaction without external
// calls (decision 069): PrepareInstall and PrepareUninstall do the reads (lock check, playbook and
// target resolution) and write nothing; PersistWorkflow writes the prepared Workflow and runs inside
// the service's transaction, which also writes the Software Assignments. Busy, lock, and automation
// errors are returned unchanged from whichever step detects them.
type SoftwareLauncher interface {
	PrepareInstall(ctx context.Context, launch SoftwareLaunch) (PreparedSoftwareWorkflow, error)
	PrepareUninstall(ctx context.Context, launch SoftwareLaunch) (PreparedSoftwareWorkflow, error)
	PersistWorkflow(ctx context.Context, prepared PreparedSoftwareWorkflow) (workflowID string, err error)
}

// PreparedSoftwareWorkflow is a software Workflow the launcher has planned but not written; only
// that launcher can persist it. WorkflowID is the id it will have, which the pending assignments
// name.
type PreparedSoftwareWorkflow interface {
	WorkflowID() string
}

// MembershipChecker reports whether a Server is a Kubernetes Platform member, which gates the
// container-runtime kinds whose containerd would collide with k0s. Implemented in the app layer
// against the platform repository.
type MembershipChecker interface {
	IsKubernetesMember(ctx context.Context, serverID string) (bool, error)
}

// ExporterOwnership reports whether swallow's host automation owns a Server's Prometheus exporters
// (its Exporter Ownership), which gates a standalone install of the host-exporter kinds. It is a
// port so the software context never reads the Server's Platform policy; the app layer resolves it
// from the Server's lock and its Platform's exporter owner.
type ExporterOwnership interface {
	HostAutomationOwnsExporters(ctx context.Context, server *serverdomain.Server) (bool, error)
}

// PlatformNames resolves a claiming Platform's display name and type, so a refusal for a
// Platform-claimed Server names the Platform. It is a port so the software context never reads the
// platform context's model; an error leaves the refusal naming the Platform by id.
type PlatformNames interface {
	PlatformName(ctx context.Context, platformID string) (name, platformType string, err error)
}

// SoftwareService owns Managed Software policy and drives the launcher. It never talks to Temporal
// directly; it validates intent, records the swallow-owned assignment, and delegates orchestration.
type SoftwareService struct {
	transactor   store.Transactor
	catalog      *softwaredomain.Catalog
	accelerators platformdomain.Accelerators
	assignments  softwaredomain.AssignmentRepository
	servers      serverdomain.ServerRepository
	membership   MembershipChecker
	exporters    ExporterOwnership
	launcher     SoftwareLauncher
	// names is optional; see AttachPlatformNames.
	names PlatformNames
}

// NewSoftwareService constructs the Managed Software use cases. transactor is required: an
// install's or uninstall's Workflow and its assignment changes are written together. catalog is the
// build's Software modules and accelerators its Accelerator modules. exporters is required by the
// host-exporter kinds' install check. The launcher is attached later (like the platform launcher) to keep the
// operation orchestration wiring in the app layer.
func NewSoftwareService(
	transactor store.Transactor,
	catalog *softwaredomain.Catalog,
	accelerators platformdomain.Accelerators,
	assignments softwaredomain.AssignmentRepository,
	servers serverdomain.ServerRepository,
	membership MembershipChecker,
	exporters ExporterOwnership,
) *SoftwareService {
	return &SoftwareService{
		transactor: transactor, catalog: catalog, accelerators: accelerators,
		assignments: assignments, servers: servers, membership: membership, exporters: exporters,
	}
}

// AttachLauncher wires the Workflow-composing launcher. Install and Uninstall require it.
func (s *SoftwareService) AttachLauncher(launcher SoftwareLauncher) { s.launcher = launcher }

// AttachPlatformNames lets a refusal for a Platform-claimed Server name the Platform. Without it
// the refusal names the Platform by id.
func (s *SoftwareService) AttachPlatformNames(names PlatformNames) { s.names = names }

// Catalog returns the installable software kinds of this build and their rules.
func (s *SoftwareService) Catalog() []softwaredomain.CatalogEntry {
	return s.catalog.Entries()
}

// Label is a kind's user-facing name ("Docker CE"), or the kind itself when it is unknown.
func (s *SoftwareService) Label(kind softwaredomain.Kind) string {
	return s.catalog.Label(kind)
}

// ListAssignments returns Software Assignments matching the filter.
func (s *SoftwareService) ListAssignments(ctx context.Context, filter softwaredomain.AssignmentFilter) ([]*softwaredomain.Assignment, error) {
	return s.assignments.List(ctx, filter)
}

// InstallTarget is one Server and the roles it should take for the software kind.
type InstallTarget struct {
	ServerID string
	Roles    []softwaredomain.Role
}

// InstallInput is the accepted intent for POST /software/assignments.
type InstallInput struct {
	Kind        softwaredomain.Kind
	Targets     []InstallTarget
	Spec        map[string]any
	RequestedBy string
}

// UninstallInput is the accepted intent for POST /software/uninstall.
type UninstallInput struct {
	Kind        softwaredomain.Kind
	ServerIDs   []string
	RequestedBy string
}

// ComposedInstall is a validated software install that another Workflow carries as its own
// configure-<kind> Job (decision 038 §5, decision 063): the composing launcher runs the software
// playbook with TrustedVars and then the record step, and calls RecordComposedInstall once its
// Workflow exists. Spec is the normalized spec the assignments record.
type ComposedInstall struct {
	Kind          softwaredomain.Kind
	ServerIDs     []string
	RolesByServer map[string][]softwaredomain.Role
	Spec          map[string]any
	TrustedVars   map[string]any
}

// installPlan is the validated, normalized intent shared by Install and PlanComposedInstall.
type installPlan struct {
	software      softwaredomain.Software
	entry         softwaredomain.CatalogEntry
	serverIDs     []string
	rolesByServer map[string][]softwaredomain.Role
	spec          map[string]any
}

// Install validates the request against the catalog, the target Servers' state, mutual exclusion,
// required kinds, Kubernetes membership, Platform Claims, Exporter Ownership, and GPU inventory, plans the install
// Workflow, and then writes the Workflow and a pending assignment per target in one transaction,
// so a refused request (a busy or locked target) leaves neither. It returns the Workflow
// (operation) id.
func (s *SoftwareService) Install(ctx context.Context, input InstallInput) (string, error) {
	if s.launcher == nil {
		return "", fmt.Errorf("%w: software orchestration is unavailable", ErrInvalidRequest)
	}
	plan, err := s.planInstall(ctx, input, false)
	if err != nil {
		return "", err
	}

	launch := SoftwareLaunch{
		Kind: input.Kind, Label: plan.entry.Label, ServerIDs: plan.serverIDs, RolesByServer: plan.rolesByServer,
		TrustedVars: buildInstallVars(plan.software, input.Kind, plan.rolesByServer, plan.spec), RequestedBy: input.RequestedBy,
	}
	prepared, err := s.launcher.PrepareInstall(ctx, launch)
	if err != nil {
		return "", err
	}
	// The install Workflow's record-software-assignment step flips the pending assignments to
	// installed on success; a failed install step is marked failed by the step observer.
	var workflowID string
	err = s.transactor.InTransaction(ctx, func(ctx context.Context) error {
		id, err := s.launcher.PersistWorkflow(ctx, prepared)
		if err != nil {
			return err
		}
		workflowID = id
		return s.recordPending(ctx, input.Kind, plan.serverIDs, plan.rolesByServer, plan.spec, id)
	})
	if err != nil {
		return "", err
	}
	return workflowID, nil
}

// PlanComposedInstall validates a software install that a platform deployment composes into its
// own Workflow, without writing anything. It applies the same catalog, role, spec, mutual
// exclusion, Kubernetes-member, and GPU rules as Install, but not the deployed-state, Requires,
// Platform Claim, or Exporter Ownership checks: the composing Workflow provisions its targets and
// installs the required kinds in earlier Jobs, it is the Platform that claims them, it decides who
// owns the exporters of the Servers it deploys or uninstalls, and its ansible Tasks are still
// accepted only through the operation layer's own checks.
func (s *SoftwareService) PlanComposedInstall(ctx context.Context, input InstallInput) (ComposedInstall, error) {
	plan, err := s.planInstall(ctx, input, true)
	if err != nil {
		return ComposedInstall{}, err
	}
	return ComposedInstall{
		Kind: input.Kind, ServerIDs: plan.serverIDs, RolesByServer: plan.rolesByServer, Spec: plan.spec,
		TrustedVars: buildInstallVars(plan.software, input.Kind, plan.rolesByServer, plan.spec),
	}, nil
}

// RecordComposedInstall writes the pending Software Assignments of a composed install once the
// composing Workflow (workflowID) exists, mirroring what Install writes after its own launch; the
// composed record step then marks them installed, and a failed install Task marks them failed.
func (s *SoftwareService) RecordComposedInstall(ctx context.Context, install ComposedInstall, workflowID string) error {
	return s.recordPending(ctx, install.Kind, install.ServerIDs, install.RolesByServer, install.Spec, workflowID)
}

// planInstall validates and normalizes an install. composed skips the checks that a composing
// Workflow satisfies by its own Job order (targets deployed, required kinds installed) and the
// Platform Claim and Exporter Ownership refusals, which exist to leave a Platform's Servers to that
// Platform.
func (s *SoftwareService) planInstall(ctx context.Context, input InstallInput, composed bool) (installPlan, error) {
	software, ok := s.catalog.Lookup(input.Kind)
	if !ok {
		return installPlan{}, fmt.Errorf("%w: %s", softwaredomain.ErrUnknownKind, input.Kind)
	}
	entry := software.Entry()
	if len(input.Targets) == 0 {
		return installPlan{}, fmt.Errorf("%w: at least one target Server is required", ErrInvalidRequest)
	}

	rolesByServer := make(map[string][]softwaredomain.Role, len(input.Targets))
	serverIDs := make([]string, 0, len(input.Targets))
	seen := map[string]bool{}
	for _, target := range input.Targets {
		if strings.TrimSpace(target.ServerID) == "" {
			return installPlan{}, fmt.Errorf("%w: each target requires a serverId", ErrInvalidRequest)
		}
		if seen[target.ServerID] {
			return installPlan{}, fmt.Errorf("%w: duplicate target %s", ErrInvalidRequest, target.ServerID)
		}
		seen[target.ServerID] = true
		if err := validateRoles(entry, target.Roles); err != nil {
			return installPlan{}, err
		}
		rolesByServer[target.ServerID] = normalizeRoles(target.Roles)
		serverIDs = append(serverIDs, target.ServerID)
	}
	sort.Strings(serverIDs)

	if err := s.catalog.CheckSpec(input.Kind, input.Spec); err != nil {
		return installPlan{}, err
	}
	if err := software.ValidateSpec(rolesByServer, input.Spec); err != nil {
		return installPlan{}, err
	}
	// The normalized spec is both what the playbook receives and what the assignment records, so
	// the record never disagrees with what was applied (an omitted enableApi is recorded as true,
	// an omitted Prometheus or Grafana version as the pinned default).
	spec := software.NormalizeSpec(input.Spec)

	// Precondition checks give clean, specific errors before a Workflow is created. The launcher's
	// PrepareAnsibleStep re-checks the deployed state and lock at execution acceptance, so a race
	// after this point still cannot start work against an ineligible host.
	for _, serverID := range serverIDs {
		server, err := s.servers.FindByID(ctx, serverID)
		if err != nil {
			return installPlan{}, err
		}
		if !composed && (server.Absent || server.Provisioning == nil || server.Provisioning.State != "deployed") {
			// The same state conflict PrepareAnsibleStep reports on a race, so both answer 409.
			return installPlan{}, fmt.Errorf("%w: %s is not deployed", workflowdomain.ErrTargetStateInvalid, server.DisplayName())
		}
		if !composed {
			if err := s.refuseClaimed(ctx, entry, server, "install"); err != nil {
				return installPlan{}, err
			}
			if err := s.refuseForeignExporters(ctx, entry, server); err != nil {
				return installPlan{}, err
			}
		}
		if entry.RequiredAccelerator != "" && !s.accelerators.HasComputeGPU(entry.RequiredAccelerator, server.Observed.GPUs) {
			return installPlan{}, fmt.Errorf("%w: %s lists no %s compute GPU in its GPU Inventory, which %s needs",
				softwaredomain.ErrTargetIneligible, server.DisplayName(), s.accelerators.Label(entry.RequiredAccelerator), entry.Label)
		}
		if entry.RefusedForKubernetesMembers && s.membership != nil {
			member, err := s.membership.IsKubernetesMember(ctx, serverID)
			if err != nil {
				return installPlan{}, err
			}
			if member {
				return installPlan{}, fmt.Errorf("%w: %s is a Kubernetes platform member", softwaredomain.ErrKubernetesMember, server.DisplayName())
			}
		}
		if err := s.checkMutualExclusion(ctx, entry, serverID); err != nil {
			return installPlan{}, err
		}
		if err := s.checkServerReferences(ctx, software, spec, server.Source.SiteID); err != nil {
			return installPlan{}, err
		}
	}
	if err := s.resolveContainerRuntime(ctx, entry, serverIDs, spec, composed); err != nil {
		return installPlan{}, err
	}
	return installPlan{software: software, entry: entry, serverIDs: serverIDs, rolesByServer: rolesByServer, spec: spec}, nil
}

// resolveContainerRuntime settles spec.containerRuntime for a kind installed by its container
// method and records it in spec. A composing platform names the runtime it installs in an earlier
// Job (an omitted value defaults to the catalog's first runtime). A standalone install uses the
// runtime already installed on the targets: every target must have the same one in state
// `installed`, and a requested runtime must match it; otherwise ErrRequiredSoftwareMissing. The
// native method takes no runtime.
func (s *SoftwareService) resolveContainerRuntime(
	ctx context.Context,
	entry softwaredomain.CatalogEntry,
	serverIDs []string,
	spec map[string]any,
	composed bool,
) error {
	if len(entry.InstallMethods) == 0 || softwaredomain.InstallMethod(softwaredomain.SpecString(spec, softwaredomain.SpecInstallMethod)) != softwaredomain.InstallMethodContainer {
		return nil
	}
	requested := softwaredomain.Kind(softwaredomain.SpecString(spec, softwaredomain.SpecContainerRuntime))
	if composed {
		if requested == "" {
			requested = entry.ContainerRuntimes[0]
		}
		spec[softwaredomain.SpecContainerRuntime] = string(requested)
		return nil
	}
	resolved := softwaredomain.Kind("")
	for _, serverID := range serverIDs {
		runtime, err := s.installedRuntime(ctx, entry, serverID)
		if err != nil {
			return err
		}
		if runtime == "" {
			return fmt.Errorf("%w: the %s container method needs %s installed on %s",
				softwaredomain.ErrRequiredSoftwareMissing, entry.Kind, softwaredomain.KindList(entry.ContainerRuntimes), serverID)
		}
		if resolved != "" && runtime != resolved {
			return fmt.Errorf("%w: targets run different container runtimes (%s and %s); install them separately",
				softwaredomain.ErrSpecInvalid, resolved, runtime)
		}
		resolved = runtime
	}
	if requested != "" && requested != resolved {
		return fmt.Errorf("%w: spec.%s is %s but the targets run %s",
			softwaredomain.ErrRequiredSoftwareMissing, softwaredomain.SpecContainerRuntime, requested, resolved)
	}
	spec[softwaredomain.SpecContainerRuntime] = string(resolved)
	return nil
}

// refuseClaimed returns ErrPlatformClaimed when entry refuses claimed Servers and server carries a
// Platform Claim: that Platform installs and removes the kind on its Servers (decision 068). verb is
// the refused action ("install" or "uninstall").
func (s *SoftwareService) refuseClaimed(ctx context.Context, entry softwaredomain.CatalogEntry, server *serverdomain.Server, verb string) error {
	if !entry.RefusedForClaimedServers || server.PlatformClaim == nil {
		return nil
	}
	platform := "Platform " + server.PlatformClaim.PlatformID
	if s.names != nil {
		if name, platformType, err := s.names.PlatformName(ctx, server.PlatformClaim.PlatformID); err == nil && name != "" {
			platform = strings.TrimSpace(platformType + " Platform " + name)
		}
	}
	return fmt.Errorf("%w: %s belongs to %s, which manages %s on its Servers; cannot %s it directly",
		softwaredomain.ErrPlatformClaimed, server.DisplayName(), platform, entry.Label, verb)
}

// refuseForeignExporters returns ErrTargetIneligible when entry is a host exporter and swallow's
// host automation does not own the Server's exporters: a Kubernetes Platform's exporters hold the
// same port, and a locked Server is left alone.
func (s *SoftwareService) refuseForeignExporters(ctx context.Context, entry softwaredomain.CatalogEntry, server *serverdomain.Server) error {
	if !entry.HostExporter {
		return nil
	}
	owned, err := s.exporters.HostAutomationOwnsExporters(ctx, server)
	if err != nil {
		return err
	}
	if !owned {
		return fmt.Errorf("%w: %s's exporters are not installed by swallow's host automation (its Platform owns them, or the Server is locked)",
			softwaredomain.ErrTargetIneligible, server.DisplayName())
	}
	return nil
}

// installedRuntime returns the entry's container runtime that is `installed` on the Server, or ""
// when none is. The runtimes are mutually exclusive, so at most one can be installed.
func (s *SoftwareService) installedRuntime(ctx context.Context, entry softwaredomain.CatalogEntry, serverID string) (softwaredomain.Kind, error) {
	for _, runtime := range entry.ContainerRuntimes {
		existing, err := s.assignments.FindByServerAndKind(ctx, serverID, runtime)
		if errors.Is(err, softwaredomain.ErrAssignmentNotFound) {
			continue
		}
		if err != nil {
			return "", err
		}
		if existing.State == softwaredomain.StateInstalled {
			return runtime, nil
		}
	}
	return "", nil
}

// checkServerReferences requires every Server a kind's spec names (softwaredomain.ServerReferencer)
// to be a Server of the target's Site, because the playbook resolves their addresses from that
// Site's inventory snapshot. A missing Server is ErrServerNotFound; one at another Site is a spec
// error.
func (s *SoftwareService) checkServerReferences(ctx context.Context, software softwaredomain.Software, spec map[string]any, siteID string) error {
	referencer, ok := software.(softwaredomain.ServerReferencer)
	if !ok {
		return nil
	}
	for _, ref := range referencer.ServerReferences(spec) {
		referenced, err := s.servers.FindByID(ctx, ref.ServerID)
		if err != nil {
			return err
		}
		if referenced.Source.SiteID != siteID {
			return fmt.Errorf("%w: spec.%s entry %s is not at the target's Site", softwaredomain.ErrSpecInvalid, ref.Field, referenced.DisplayName())
		}
	}
	return nil
}

// recordPending upserts a pending assignment per target naming workflowID.
func (s *SoftwareService) recordPending(
	ctx context.Context,
	kind softwaredomain.Kind,
	serverIDs []string,
	rolesByServer map[string][]softwaredomain.Role,
	spec map[string]any,
	workflowID string,
) error {
	now := time.Now().UTC()
	for _, serverID := range serverIDs {
		server, err := s.servers.FindByID(ctx, serverID)
		if err != nil {
			return err
		}
		assignment := &softwaredomain.Assignment{
			ServerID: serverID, SiteID: server.Source.SiteID, Kind: kind,
			Roles: rolesByServer[serverID], Spec: softwaredomain.CloneSpec(spec),
			State: softwaredomain.StatePending, LastWorkflowID: workflowID,
			LastAppliedAt: s.previousAppliedAt(ctx, serverID, kind),
			CreatedAt:     now, UpdatedAt: now,
		}
		if err := s.assignments.Upsert(ctx, assignment); err != nil {
			return err
		}
	}
	return nil
}

// Uninstall validates that each target has a live assignment of the kind (and, for a kind that
// refuses claimed Servers, no Platform Claim: its Platform removes it), plans the uninstall
// Workflow, and writes it together with the assignments' uninstalling state in one transaction.
// The clear-software-assignment step marks them absent on success.
func (s *SoftwareService) Uninstall(ctx context.Context, input UninstallInput) (string, error) {
	if s.launcher == nil {
		return "", fmt.Errorf("%w: software orchestration is unavailable", ErrInvalidRequest)
	}
	software, ok := s.catalog.Lookup(input.Kind)
	if !ok {
		return "", fmt.Errorf("%w: %s", softwaredomain.ErrUnknownKind, input.Kind)
	}
	entry := software.Entry()
	if len(input.ServerIDs) == 0 {
		return "", fmt.Errorf("%w: at least one serverId is required", ErrInvalidRequest)
	}

	rolesByServer := make(map[string][]softwaredomain.Role, len(input.ServerIDs))
	serverIDs := make([]string, 0, len(input.ServerIDs))
	seen := map[string]bool{}
	var spec map[string]any
	for _, serverID := range input.ServerIDs {
		if seen[serverID] {
			continue
		}
		seen[serverID] = true
		assignment, err := s.assignments.FindByServerAndKind(ctx, serverID, input.Kind)
		if err != nil {
			return "", err
		}
		if assignment.State == softwaredomain.StateAbsent {
			return "", fmt.Errorf("%w: %s has no %s assignment", softwaredomain.ErrAssignmentNotFound, serverID, input.Kind)
		}
		if entry.RefusedForClaimedServers {
			// A Server that no longer exists has no claim to honour; the operation layer refuses it.
			server, err := s.servers.FindByID(ctx, serverID)
			if err != nil && !errors.Is(err, serverdomain.ErrServerNotFound) {
				return "", err
			}
			if err == nil {
				if err := s.refuseClaimed(ctx, entry, server, "uninstall"); err != nil {
					return "", err
				}
			}
		}
		rolesByServer[serverID] = assignment.Roles
		if spec == nil {
			spec = assignment.Spec
		}
		serverIDs = append(serverIDs, serverID)
	}
	sort.Strings(serverIDs)

	launch := SoftwareLaunch{
		Kind: input.Kind, Label: entry.Label, ServerIDs: serverIDs, RolesByServer: rolesByServer,
		TrustedVars: buildInstallVars(software, input.Kind, rolesByServer, spec), RequestedBy: input.RequestedBy,
	}
	prepared, err := s.launcher.PrepareUninstall(ctx, launch)
	if err != nil {
		return "", err
	}
	var workflowID string
	err = s.transactor.InTransaction(ctx, func(ctx context.Context) error {
		id, err := s.launcher.PersistWorkflow(ctx, prepared)
		if err != nil {
			return err
		}
		workflowID = id
		for _, serverID := range serverIDs {
			if err := s.assignments.SetState(ctx, serverID, input.Kind, softwaredomain.StateUninstalling, id, nil); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	return workflowID, nil
}

// previousAppliedAt carries the last successful apply across a re-apply of an existing assignment
// (for example turning Docker CE's enableApi on), so the record keeps saying the software was
// applied while the new ensure is pending or if it fails. An absent record restarts from nil: the
// software is gone from disk, so nothing was applied. A read failure also yields nil — losing the
// timestamp is preferable to failing an install the launcher already accepted.
func (s *SoftwareService) previousAppliedAt(ctx context.Context, serverID string, kind softwaredomain.Kind) *time.Time {
	existing, err := s.assignments.FindByServerAndKind(ctx, serverID, kind)
	if err != nil || existing.State == softwaredomain.StateAbsent {
		return nil
	}
	return existing.LastAppliedAt
}

// checkMutualExclusion refuses installing a kind when a conflicting kind is already present
// (non-absent) on the Server.
func (s *SoftwareService) checkMutualExclusion(ctx context.Context, entry softwaredomain.CatalogEntry, serverID string) error {
	for _, exclusive := range entry.MutuallyExclusiveWith {
		existing, err := s.assignments.FindByServerAndKind(ctx, serverID, exclusive)
		if err != nil {
			if errors.Is(err, softwaredomain.ErrAssignmentNotFound) {
				continue
			}
			return err
		}
		if existing.State != softwaredomain.StateAbsent {
			return fmt.Errorf("%w: %s is present, cannot install %s", softwaredomain.ErrMutuallyExclusive, exclusive, entry.Kind)
		}
	}
	return nil
}

func validateRoles(entry softwaredomain.CatalogEntry, roles []softwaredomain.Role) error {
	if len(entry.Roles) == 0 {
		if len(roles) > 0 {
			return fmt.Errorf("%w: %s takes no roles", softwaredomain.ErrInvalidRole, entry.Kind)
		}
		return nil
	}
	if len(roles) == 0 {
		return fmt.Errorf("%w: %s requires at least one role", softwaredomain.ErrInvalidRole, entry.Kind)
	}
	for _, role := range roles {
		if !entry.AllowsRole(role) {
			return fmt.Errorf("%w: %q is not a valid role for %s", softwaredomain.ErrInvalidRole, role, entry.Kind)
		}
	}
	return nil
}

// buildInstallVars assembles the trusted swallow_-prefixed variables the playbook consumes: the kind
// and each target's roles, and the kind's own from its Software. The caller passes them through the
// operation layer as trusted vars, so a client cannot forge them.
func buildInstallVars(software softwaredomain.Software, kind softwaredomain.Kind, rolesByServer map[string][]softwaredomain.Role, spec map[string]any) map[string]any {
	vars := software.InstallVars(rolesByServer, spec)
	if vars == nil {
		vars = map[string]any{}
	}
	vars["swallow_software_kind"] = string(kind)
	roles := make(map[string][]string, len(rolesByServer))
	for serverID, list := range rolesByServer {
		roles[serverID] = rolesToStrings(list)
	}
	vars["swallow_software_roles"] = roles
	return vars
}

func normalizeRoles(roles []softwaredomain.Role) []softwaredomain.Role {
	if len(roles) == 0 {
		return nil
	}
	out := make([]softwaredomain.Role, 0, len(roles))
	seen := map[softwaredomain.Role]bool{}
	for _, role := range roles {
		if seen[role] {
			continue
		}
		seen[role] = true
		out = append(out, role)
	}
	return out
}

func rolesToStrings(roles []softwaredomain.Role) []string {
	out := make([]string, 0, len(roles))
	for _, role := range roles {
		out = append(out, string(role))
	}
	return out
}
