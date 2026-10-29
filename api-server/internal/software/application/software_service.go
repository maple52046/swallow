// Package application holds the Managed Software use cases: install, uninstall, list assignments,
// read the catalog, and the OS-lifecycle sweep that marks assignments absent when a Server leaves
// `deployed`. It composes the existing Workflow engine through a launcher port rather than owning
// any orchestration, keeping software deployment aligned with platform deployment
// (docs/development/software-deployment.md, decision 038).
package application

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"

	serverdomain "github.com/maple52046/swallow/internal/server/domain"
	softwaredomain "github.com/maple52046/swallow/internal/software/domain"
)

// ErrInvalidRequest wraps every request-shape validation failure so delivery maps it to 400.
// State and uniqueness conflicts use the domain sentinels (ErrMutuallyExclusive,
// ErrKubernetesMember) or the operation layer's busy/locked errors surfaced by the launcher.
var ErrInvalidRequest = errors.New("invalid software request")

// SoftwareLaunch is the trusted, server-side intent handed to the launcher. Roles are per
// serverId; TrustedVars are the assembled swallow_-prefixed variables the playbook consumes and
// the client cannot forge.
type SoftwareLaunch struct {
	Kind          softwaredomain.Kind
	ServerIDs     []string
	RolesByServer map[string][]softwaredomain.Role
	TrustedVars   map[string]any
	RequestedBy   string
}

// SoftwareLauncher composes the software Workflow (prepare-hosts -> configure-<kind> ->
// record-software) and returns its Workflow id. It is implemented in the app layer so the software
// context does not import the operation orchestration internals directly.
type SoftwareLauncher interface {
	LaunchInstall(ctx context.Context, launch SoftwareLaunch) (string, error)
	LaunchUninstall(ctx context.Context, launch SoftwareLaunch) (string, error)
}

// MembershipChecker reports whether a Server is a Kubernetes Platform member, which gates the
// container-runtime kinds whose containerd would collide with k0s. Implemented in the app layer
// against the platform repository.
type MembershipChecker interface {
	IsKubernetesMember(ctx context.Context, serverID string) (bool, error)
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
	assignments softwaredomain.AssignmentRepository
	servers     serverdomain.ServerRepository
	membership  MembershipChecker
	launcher    SoftwareLauncher
	// names is optional; see AttachPlatformNames.
	names PlatformNames
}

// NewSoftwareService constructs the Managed Software use cases. The launcher is attached later
// (like the platform launcher) to keep the operation orchestration wiring in the app layer.
func NewSoftwareService(
	assignments softwaredomain.AssignmentRepository,
	servers serverdomain.ServerRepository,
	membership MembershipChecker,
) *SoftwareService {
	return &SoftwareService{assignments: assignments, servers: servers, membership: membership}
}

// AttachLauncher wires the Workflow-composing launcher. Install and Uninstall require it.
func (s *SoftwareService) AttachLauncher(launcher SoftwareLauncher) { s.launcher = launcher }

// AttachPlatformNames lets a refusal for a Platform-claimed Server name the Platform. Without it
// the refusal names the Platform by id.
func (s *SoftwareService) AttachPlatformNames(names PlatformNames) { s.names = names }

// Catalog returns the fixed set of installable software kinds and their rules.
func (s *SoftwareService) Catalog() []softwaredomain.CatalogEntry {
	return softwaredomain.Catalog()
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
	entry         softwaredomain.CatalogEntry
	serverIDs     []string
	rolesByServer map[string][]softwaredomain.Role
	spec          map[string]any
}

// Install validates the request against the catalog, the target Servers' state, mutual exclusion,
// required kinds, Kubernetes membership, Platform Claims, and GPU inventory, then launches the
// install Workflow and records a pending assignment per target. It returns the Workflow (operation)
// id.
func (s *SoftwareService) Install(ctx context.Context, input InstallInput) (string, error) {
	if s.launcher == nil {
		return "", fmt.Errorf("%w: software orchestration is unavailable", ErrInvalidRequest)
	}
	plan, err := s.planInstall(ctx, input, false)
	if err != nil {
		return "", err
	}

	launch := SoftwareLaunch{
		Kind: input.Kind, ServerIDs: plan.serverIDs, RolesByServer: plan.rolesByServer,
		TrustedVars: buildInstallVars(input.Kind, plan.rolesByServer, plan.spec), RequestedBy: input.RequestedBy,
	}
	workflowID, err := s.launcher.LaunchInstall(ctx, launch)
	if err != nil {
		return "", err
	}

	// Record a pending assignment per target after the Workflow is accepted. The install
	// Workflow's record-software-assignment step flips these to installed on success; a failed
	// install step is marked failed by the step observer. Writing after a successful launch keeps
	// a rejected request (busy/locked target) from leaving orphaned assignments.
	return workflowID, s.recordPending(ctx, input.Kind, plan.serverIDs, plan.rolesByServer, plan.spec, workflowID)
}

// PlanComposedInstall validates a software install that a platform deployment composes into its
// own Workflow, without writing anything. It applies the same catalog, role, spec, mutual
// exclusion, Kubernetes-member, and GPU rules as Install, but not the deployed-state, Requires, or
// Platform Claim checks: the composing Workflow provisions its targets and installs the required
// kinds in earlier Jobs, it is the Platform that claims them, and its ansible Tasks are still
// accepted only through the operation layer's own checks.
func (s *SoftwareService) PlanComposedInstall(ctx context.Context, input InstallInput) (ComposedInstall, error) {
	plan, err := s.planInstall(ctx, input, true)
	if err != nil {
		return ComposedInstall{}, err
	}
	return ComposedInstall{
		Kind: input.Kind, ServerIDs: plan.serverIDs, RolesByServer: plan.rolesByServer, Spec: plan.spec,
		TrustedVars: buildInstallVars(input.Kind, plan.rolesByServer, plan.spec),
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
// Platform Claim refusal, which exists to leave a Platform's Servers to that Platform.
func (s *SoftwareService) planInstall(ctx context.Context, input InstallInput, composed bool) (installPlan, error) {
	entry, ok := softwaredomain.LookupKind(input.Kind)
	if !ok {
		return installPlan{}, fmt.Errorf("%w: %s", softwaredomain.ErrUnknownKind, input.Kind)
	}
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

	if err := validateSpec(entry, rolesByServer, input.Spec); err != nil {
		return installPlan{}, err
	}
	// The normalized spec is both what the playbook receives and what the assignment records, so
	// the record never disagrees with what was applied (an omitted enableApi is recorded as true,
	// an omitted Prometheus or Grafana version as the pinned default).
	spec := normalizeSpec(entry.Kind, input.Spec)

	// Precondition checks give clean, specific errors before a Workflow is created. The launcher's
	// PrepareAnsibleStep re-checks the deployed state and lock at execution acceptance, so a race
	// after this point still cannot start work against an ineligible host.
	for _, serverID := range serverIDs {
		server, err := s.servers.FindByID(ctx, serverID)
		if err != nil {
			return installPlan{}, err
		}
		if !composed && (server.Absent || server.Provisioning == nil || server.Provisioning.State != "deployed") {
			return installPlan{}, fmt.Errorf("%w: %s is not deployed", ErrInvalidRequest, server.DisplayName())
		}
		if !composed {
			if err := s.refuseClaimed(ctx, entry, server, "install"); err != nil {
				return installPlan{}, err
			}
		}
		if entry.RequiresAMDComputeGPU && !serverdomain.HasAMDComputeGPU(server.Observed.GPUs) {
			return installPlan{}, fmt.Errorf("%w: %s lists no AMD compute GPU in its GPU Inventory, which %s needs",
				softwaredomain.ErrTargetIneligible, server.DisplayName(), entry.Label)
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
		if err := s.checkScrapeServers(ctx, spec, server.Source.SiteID); err != nil {
			return installPlan{}, err
		}
	}
	if err := s.resolveContainerRuntime(ctx, entry, serverIDs, spec, composed); err != nil {
		return installPlan{}, err
	}
	return installPlan{entry: entry, serverIDs: serverIDs, rolesByServer: rolesByServer, spec: spec}, nil
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
	if len(entry.InstallMethods) == 0 || softwaredomain.InstallMethod(specString(spec, softwaredomain.SpecInstallMethod)) != softwaredomain.InstallMethodContainer {
		return nil
	}
	requested := softwaredomain.Kind(specString(spec, softwaredomain.SpecContainerRuntime))
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
				softwaredomain.ErrRequiredSoftwareMissing, entry.Kind, kindList(entry.ContainerRuntimes), serverID)
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

func kindList(kinds []softwaredomain.Kind) string {
	names := make([]string, len(kinds))
	for i, kind := range kinds {
		names[i] = string(kind)
	}
	return strings.Join(names, " or ")
}

// checkScrapeServers requires every Prometheus scrapeServerIds entry to be a Server of the target's
// Site, because the playbook resolves their addresses from that Site's inventory snapshot. A
// missing Server is ErrServerNotFound; one at another Site is a spec error.
func (s *SoftwareService) checkScrapeServers(ctx context.Context, spec map[string]any, siteID string) error {
	serverIDs, _ := specStringList(spec, softwaredomain.SpecScrapeServerIDs)
	for _, id := range serverIDs {
		scraped, err := s.servers.FindByID(ctx, id)
		if err != nil {
			return err
		}
		if scraped.Source.SiteID != siteID {
			return fmt.Errorf("%w: spec.%s entry %s is not at the target's Site", softwaredomain.ErrSpecInvalid, softwaredomain.SpecScrapeServerIDs, scraped.DisplayName())
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
			Roles: rolesByServer[serverID], Spec: cloneSpec(spec),
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
// refuses claimed Servers, no Platform Claim: its Platform removes it), launches the uninstall
// Workflow, and marks the assignments uninstalling. The clear-software-assignment step marks them
// absent on success.
func (s *SoftwareService) Uninstall(ctx context.Context, input UninstallInput) (string, error) {
	if s.launcher == nil {
		return "", fmt.Errorf("%w: software orchestration is unavailable", ErrInvalidRequest)
	}
	entry, ok := softwaredomain.LookupKind(input.Kind)
	if !ok {
		return "", fmt.Errorf("%w: %s", softwaredomain.ErrUnknownKind, input.Kind)
	}
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
		Kind: input.Kind, ServerIDs: serverIDs, RolesByServer: rolesByServer,
		TrustedVars: buildInstallVars(input.Kind, rolesByServer, spec), RequestedBy: input.RequestedBy,
	}
	workflowID, err := s.launcher.LaunchUninstall(ctx, launch)
	if err != nil {
		return "", err
	}
	for _, serverID := range serverIDs {
		if err := s.assignments.SetState(ctx, serverID, input.Kind, softwaredomain.StateUninstalling, workflowID, nil); err != nil {
			return workflowID, err
		}
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

// validateSpec enforces the kind-specific required fields and types before a Workflow is created.
func validateSpec(entry softwaredomain.CatalogEntry, rolesByServer map[string][]softwaredomain.Role, spec map[string]any) error {
	switch entry.Kind {
	case softwaredomain.KindDockerCE, softwaredomain.KindPodman:
		return validateRuntimeSpec(entry.Kind, spec)
	case softwaredomain.KindNFS:
		return validateNFSSpec(rolesByServer, spec)
	case softwaredomain.KindPrometheus:
		return validatePrometheusSpec(entry, spec)
	case softwaredomain.KindGrafana:
		return validateGrafanaSpec(entry, spec)
	case softwaredomain.KindDeviceMetricsExporter:
		// The version is pinned and recorded by normalizeSpec; there is nothing to choose yet.
		if len(spec) > 0 {
			return fmt.Errorf("%w: %s takes no spec fields", softwaredomain.ErrSpecInvalid, entry.Kind)
		}
	}
	return nil
}

// validatePrometheusSpec checks the install method, release version, listen address, retention,
// job name, and targets.
func validatePrometheusSpec(entry softwaredomain.CatalogEntry, spec map[string]any) error {
	if err := validateMethodAndVersion(entry, spec); err != nil {
		return err
	}
	for key, validate := range map[string]func(string) error{
		softwaredomain.SpecListen:    softwaredomain.ValidatePrometheusListen,
		softwaredomain.SpecRetention: softwaredomain.ValidatePrometheusRetention,
	} {
		value, present := spec[key]
		if !present || value == nil {
			continue
		}
		raw, ok := value.(string)
		if !ok {
			return fmt.Errorf("%w: spec.%s must be a string", softwaredomain.ErrSpecInvalid, key)
		}
		if strings.TrimSpace(raw) == "" {
			continue
		}
		if err := validate(strings.TrimSpace(raw)); err != nil {
			return err
		}
	}
	if value, present := spec[softwaredomain.SpecScrapeJob]; present && value != nil {
		job, ok := value.(string)
		if !ok {
			return fmt.Errorf("%w: spec.%s must be a string", softwaredomain.ErrSpecInvalid, softwaredomain.SpecScrapeJob)
		}
		if strings.TrimSpace(job) != "" {
			if err := softwaredomain.ValidateScrapeJob(strings.TrimSpace(job)); err != nil {
				return err
			}
		}
	}
	targets, err := specStringList(spec, softwaredomain.SpecScrapeTargets)
	if err != nil {
		return err
	}
	serverIDs, err := specStringList(spec, softwaredomain.SpecScrapeServerIDs)
	if err != nil {
		return err
	}
	if total := len(targets) + len(serverIDs); total > softwaredomain.MaxPrometheusScrapeTargets {
		return fmt.Errorf("%w: spec.%s and spec.%s have %d entries, at most %d", softwaredomain.ErrSpecInvalid,
			softwaredomain.SpecScrapeTargets, softwaredomain.SpecScrapeServerIDs, total, softwaredomain.MaxPrometheusScrapeTargets)
	}
	for _, target := range targets {
		if err := softwaredomain.ValidateScrapeTarget(target); err != nil {
			return err
		}
	}
	port, present, err := specPort(spec, softwaredomain.SpecScrapePort)
	if err != nil {
		return err
	}
	if len(serverIDs) > 0 && !present {
		return fmt.Errorf("%w: spec.%s requires spec.%s", softwaredomain.ErrSpecInvalid, softwaredomain.SpecScrapeServerIDs, softwaredomain.SpecScrapePort)
	}
	if present && len(serverIDs) == 0 {
		return fmt.Errorf("%w: spec.%s is only valid with spec.%s (port %d)", softwaredomain.ErrSpecInvalid, softwaredomain.SpecScrapePort, softwaredomain.SpecScrapeServerIDs, port)
	}
	return nil
}

// specPort reads an optional TCP port. JSON numbers decode as float64 and a composing launcher
// passes an int; both must be whole numbers in 1-65535.
func specPort(spec map[string]any, key string) (int, bool, error) {
	value, present := spec[key]
	if !present || value == nil {
		return 0, false, nil
	}
	var port float64
	switch typed := value.(type) {
	case int:
		port = float64(typed)
	case int32:
		port = float64(typed)
	case int64:
		port = float64(typed)
	case float64:
		port = typed
	default:
		return 0, true, fmt.Errorf("%w: spec.%s must be a number", softwaredomain.ErrSpecInvalid, key)
	}
	if port != float64(int(port)) || port < 1 || port > 65535 {
		return 0, true, fmt.Errorf("%w: spec.%s must be a whole number between 1 and 65535", softwaredomain.ErrSpecInvalid, key)
	}
	return int(port), true, nil
}

// validateGrafanaSpec checks the install method, release version, datasource URL, and bundles.
func validateGrafanaSpec(entry softwaredomain.CatalogEntry, spec map[string]any) error {
	if err := validateMethodAndVersion(entry, spec); err != nil {
		return err
	}
	if value, present := spec[softwaredomain.SpecPrometheusURL]; present && value != nil {
		raw, ok := value.(string)
		if !ok {
			return fmt.Errorf("%w: spec.%s must be a string", softwaredomain.ErrSpecInvalid, softwaredomain.SpecPrometheusURL)
		}
		if strings.TrimSpace(raw) != "" {
			if err := softwaredomain.ValidatePrometheusURL(strings.TrimSpace(raw)); err != nil {
				return err
			}
		}
	}
	dashboards, err := specStringList(spec, softwaredomain.SpecDashboards)
	if err != nil {
		return err
	}
	for _, dashboard := range dashboards {
		if err := softwaredomain.ValidateDashboardBundle(dashboard); err != nil {
			return err
		}
	}
	return nil
}

// validateMethodAndVersion checks the optional install method, container runtime, and release
// version of a kind with several install methods. A runtime is only meaningful for the container
// method and must be one of the kind's container runtimes.
func validateMethodAndVersion(entry softwaredomain.CatalogEntry, spec map[string]any) error {
	method := entry.InstallMethods[0]
	if value, present := spec[softwaredomain.SpecInstallMethod]; present && value != nil {
		raw, ok := value.(string)
		if !ok || !entry.Allows(softwaredomain.InstallMethod(strings.TrimSpace(raw))) {
			return fmt.Errorf("%w: spec.%s must be one of %v", softwaredomain.ErrSpecInvalid, softwaredomain.SpecInstallMethod, entry.InstallMethods)
		}
		method = softwaredomain.InstallMethod(strings.TrimSpace(raw))
	}
	if value, present := spec[softwaredomain.SpecContainerRuntime]; present && value != nil {
		raw, ok := value.(string)
		runtime := softwaredomain.Kind(strings.TrimSpace(raw))
		switch {
		case !ok:
			return fmt.Errorf("%w: spec.%s must be a string", softwaredomain.ErrSpecInvalid, softwaredomain.SpecContainerRuntime)
		case runtime != "" && method != softwaredomain.InstallMethodContainer:
			return fmt.Errorf("%w: spec.%s applies only to installMethod %s", softwaredomain.ErrSpecInvalid, softwaredomain.SpecContainerRuntime, softwaredomain.InstallMethodContainer)
		case runtime != "" && !entry.RunsContainersOn(runtime):
			return fmt.Errorf("%w: spec.%s must be %s", softwaredomain.ErrSpecInvalid, softwaredomain.SpecContainerRuntime, kindList(entry.ContainerRuntimes))
		}
	}
	value, present := spec[softwaredomain.SpecVersion]
	if !present || value == nil {
		return nil
	}
	raw, ok := value.(string)
	if !ok {
		return fmt.Errorf("%w: spec.%s must be a string", softwaredomain.ErrSpecInvalid, softwaredomain.SpecVersion)
	}
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	return softwaredomain.ValidateReleaseVersion(softwaredomain.NormalizeReleaseVersion(raw))
}

// specStringList reads an optional list of strings. JSON decodes arrays as []any, a composing
// launcher passes []string, and a spec read back from MongoDB holds the driver's own slice type;
// any slice is accepted, and an entry that is not a string is a validation error. Entries are
// trimmed and empty ones dropped.
func specStringList(spec map[string]any, key string) ([]string, error) {
	value, present := spec[key]
	if !present || value == nil {
		return nil, nil
	}
	list := reflect.ValueOf(value)
	if list.Kind() != reflect.Slice {
		return nil, fmt.Errorf("%w: spec.%s must be an array of strings", softwaredomain.ErrSpecInvalid, key)
	}
	out := make([]string, 0, list.Len())
	for i := 0; i < list.Len(); i++ {
		text, ok := list.Index(i).Interface().(string)
		if !ok {
			return nil, fmt.Errorf("%w: spec.%s must be an array of strings", softwaredomain.ErrSpecInvalid, key)
		}
		if trimmed := strings.TrimSpace(text); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out, nil
}

// validateRuntimeSpec checks the container-runtime spec types. A version must be a string, and
// Docker CE's enableApi must be a real boolean: accepting a string such as "false" and reading it
// as enabled would open the unauthenticated listener the operator declined.
func validateRuntimeSpec(kind softwaredomain.Kind, spec map[string]any) error {
	if value, ok := spec[softwaredomain.SpecVersion]; ok && value != nil {
		if _, isString := value.(string); !isString {
			return fmt.Errorf("%w: spec.%s must be a string", softwaredomain.ErrSpecInvalid, softwaredomain.SpecVersion)
		}
	}
	if kind != softwaredomain.KindDockerCE {
		return nil
	}
	if value, ok := spec[softwaredomain.SpecEnableAPI]; ok && value != nil {
		if _, isBool := value.(bool); !isBool {
			return fmt.Errorf("%w: spec.%s must be a boolean", softwaredomain.ErrSpecInvalid, softwaredomain.SpecEnableAPI)
		}
	}
	return nil
}

// validateNFSSpec requires an export path when any target takes the server role, and a source and
// mount path when any target takes the client role.
func validateNFSSpec(rolesByServer map[string][]softwaredomain.Role, spec map[string]any) error {
	hasServer, hasClient := false, false
	for _, roles := range rolesByServer {
		for _, role := range roles {
			switch role {
			case softwaredomain.RoleServer:
				hasServer = true
			case softwaredomain.RoleClient:
				hasClient = true
			}
		}
	}
	if hasServer && strings.TrimSpace(specString(spec, "exportPath")) == "" {
		return fmt.Errorf("%w: an NFS server role requires spec.exportPath", softwaredomain.ErrSpecInvalid)
	}
	if hasClient {
		if strings.TrimSpace(specString(spec, "source")) == "" {
			return fmt.Errorf("%w: an NFS client role requires spec.source", softwaredomain.ErrSpecInvalid)
		}
		if strings.TrimSpace(specString(spec, "mountPath")) == "" {
			return fmt.Errorf("%w: an NFS client role requires spec.mountPath", softwaredomain.ErrSpecInvalid)
		}
	}
	return nil
}

// buildInstallVars assembles the trusted swallow_-prefixed variables the playbook consumes. The
// caller passes them through the operation layer as trusted vars, so a client cannot forge them.
func buildInstallVars(kind softwaredomain.Kind, rolesByServer map[string][]softwaredomain.Role, spec map[string]any) map[string]any {
	vars := map[string]any{"swallow_software_kind": string(kind)}
	roles := make(map[string][]string, len(rolesByServer))
	for serverID, list := range rolesByServer {
		roles[serverID] = rolesToStrings(list)
	}
	vars["swallow_software_roles"] = roles

	switch kind {
	case softwaredomain.KindNFS:
		serverIDs, clientIDs := []string{}, []string{}
		for serverID, list := range rolesByServer {
			for _, role := range list {
				switch role {
				case softwaredomain.RoleServer:
					serverIDs = append(serverIDs, serverID)
				case softwaredomain.RoleClient:
					clientIDs = append(clientIDs, serverID)
				}
			}
		}
		sort.Strings(serverIDs)
		sort.Strings(clientIDs)
		vars["swallow_nfs_server_ids"] = serverIDs
		vars["swallow_nfs_client_ids"] = clientIDs
		setIfPresent(vars, "swallow_nfs_export_path", spec, "exportPath")
		setIfPresent(vars, "swallow_nfs_export_options", spec, "exportOptions")
		setIfPresent(vars, "swallow_nfs_client_source", spec, "source")
		setIfPresent(vars, "swallow_nfs_client_mount_path", spec, "mountPath")
		setIfPresent(vars, "swallow_nfs_client_mount_options", spec, "mountOptions")
	case softwaredomain.KindDockerCE:
		setIfPresent(vars, "swallow_docker_version", spec, softwaredomain.SpecVersion)
		// Always sent so the playbook converges the listener both ways: true adds it, false (and a
		// legacy spec without the key) removes swallow's drop-in. The port comes from the domain so
		// the playbook opens exactly what the explorer dials.
		vars["swallow_docker_enable_api"] = softwaredomain.DockerAPIEnabled(spec)
		vars["swallow_docker_api_port"] = softwaredomain.DockerEngineAPIPort
	case softwaredomain.KindPodman:
		setIfPresent(vars, "swallow_podman_version", spec, softwaredomain.SpecVersion)
	case softwaredomain.KindPrometheus:
		// The spec is normalized, so the method, version, and job are always present (and the
		// runtime for the container method) and the target list is never nil; the port comes from
		// the domain so a composing platform agrees with it.
		vars["swallow_prometheus_install_method"] = specString(spec, softwaredomain.SpecInstallMethod)
		vars["swallow_prometheus_container_runtime"] = specString(spec, softwaredomain.SpecContainerRuntime)
		vars["swallow_prometheus_version"] = specString(spec, softwaredomain.SpecVersion)
		vars["swallow_prometheus_listen"] = specString(spec, softwaredomain.SpecListen)
		vars["swallow_prometheus_retention"] = specString(spec, softwaredomain.SpecRetention)
		vars["swallow_prometheus_port"] = softwaredomain.PrometheusPort
		vars["swallow_prometheus_scrape_job"] = specString(spec, softwaredomain.SpecScrapeJob)
		targets, _ := specStringList(spec, softwaredomain.SpecScrapeTargets)
		vars["swallow_prometheus_scrape_targets"] = nonNilStrings(targets)
		// Server ids, not addresses: the playbook resolves each from the run's inventory snapshot.
		serverIDs, _ := specStringList(spec, softwaredomain.SpecScrapeServerIDs)
		vars["swallow_prometheus_scrape_server_ids"] = nonNilStrings(serverIDs)
		if port, present, _ := specPort(spec, softwaredomain.SpecScrapePort); present {
			vars["swallow_prometheus_scrape_port"] = port
		}
	case softwaredomain.KindGrafana:
		vars["swallow_grafana_install_method"] = specString(spec, softwaredomain.SpecInstallMethod)
		vars["swallow_grafana_container_runtime"] = specString(spec, softwaredomain.SpecContainerRuntime)
		vars["swallow_grafana_version"] = specString(spec, softwaredomain.SpecVersion)
		vars["swallow_grafana_port"] = softwaredomain.GrafanaPort
		// Empty tells the playbook to read the Prometheus on the same Server.
		vars["swallow_grafana_prometheus_url"] = strings.TrimSpace(specString(spec, softwaredomain.SpecPrometheusURL))
		vars["swallow_grafana_prometheus_port"] = softwaredomain.PrometheusPort
		dashboards, _ := specStringList(spec, softwaredomain.SpecDashboards)
		vars["swallow_grafana_dashboards"] = nonNilStrings(dashboards)
	case softwaredomain.KindDeviceMetricsExporter:
		// A record written by this release always carries the version; fall back for safety. A
		// composing Slurm deployment overlays its own job attribution and topology on these.
		version := strings.TrimSpace(specString(spec, softwaredomain.SpecVersion))
		if version == "" {
			version = softwaredomain.DeviceMetricsExporterVersion
		}
		vars["swallow_device_metrics_exporter_version"] = version
		vars["swallow_device_metrics_exporter_port"] = softwaredomain.DeviceMetricsExporterPort
		vars["swallow_device_metrics_exporter_job_attribution"] = string(softwaredomain.JobAttributionNone)
	}
	return vars
}

// normalizeSpec applies the kind-specific defaults to an already validated install spec and returns
// a copy. Docker CE's enableApi defaults to true (decision 043), recorded explicitly so a later read
// can tell "enabled by default" from a legacy record that predates the variant. Prometheus and
// Grafana record the pinned image tag they apply and their lists as plain string arrays. The Device
// Metrics Exporter takes no spec and records the pinned version it applies.
func normalizeSpec(kind softwaredomain.Kind, spec map[string]any) map[string]any {
	out := cloneSpec(spec)
	switch kind {
	case softwaredomain.KindDockerCE:
		if out == nil {
			out = map[string]any{}
		}
		if value, ok := out[softwaredomain.SpecEnableAPI]; !ok || value == nil {
			out[softwaredomain.SpecEnableAPI] = true
		}
	case softwaredomain.KindPrometheus:
		if out == nil {
			out = map[string]any{}
		}
		normalizeMethodAndVersion(kind, out, softwaredomain.DefaultPrometheusVersion)
		setDefaultString(out, softwaredomain.SpecListen, string(softwaredomain.PrometheusListenLoopback))
		setDefaultString(out, softwaredomain.SpecRetention, softwaredomain.DefaultPrometheusRetention)
		setDefaultString(out, softwaredomain.SpecScrapeJob, softwaredomain.DefaultPrometheusScrapeJob)
		targets, _ := specStringList(out, softwaredomain.SpecScrapeTargets)
		out[softwaredomain.SpecScrapeTargets] = nonNilStrings(targets)
		serverIDs, _ := specStringList(out, softwaredomain.SpecScrapeServerIDs)
		out[softwaredomain.SpecScrapeServerIDs] = nonNilStrings(serverIDs)
		if port, present, _ := specPort(out, softwaredomain.SpecScrapePort); present {
			out[softwaredomain.SpecScrapePort] = port
		}
	case softwaredomain.KindGrafana:
		if out == nil {
			out = map[string]any{}
		}
		normalizeMethodAndVersion(kind, out, softwaredomain.DefaultGrafanaVersion)
		out[softwaredomain.SpecPrometheusURL] = strings.TrimSpace(specString(out, softwaredomain.SpecPrometheusURL))
		dashboards, _ := specStringList(out, softwaredomain.SpecDashboards)
		out[softwaredomain.SpecDashboards] = nonNilStrings(dashboards)
	case softwaredomain.KindDeviceMetricsExporter:
		out = map[string]any{softwaredomain.SpecVersion: softwaredomain.DeviceMetricsExporterVersion}
	}
	return out
}

// normalizeMethodAndVersion records the install method (the catalog's default when omitted), drops a
// container runtime from a native install (the container method's runtime is resolved later by
// resolveContainerRuntime), and records the release version without a leading "v".
func normalizeMethodAndVersion(kind softwaredomain.Kind, spec map[string]any, defaultVersion string) {
	entry, _ := softwaredomain.LookupKind(kind)
	setDefaultString(spec, softwaredomain.SpecInstallMethod, string(entry.InstallMethods[0]))
	if softwaredomain.InstallMethod(specString(spec, softwaredomain.SpecInstallMethod)) != softwaredomain.InstallMethodContainer {
		delete(spec, softwaredomain.SpecContainerRuntime)
	} else {
		spec[softwaredomain.SpecContainerRuntime] = strings.TrimSpace(specString(spec, softwaredomain.SpecContainerRuntime))
	}
	setDefaultString(spec, softwaredomain.SpecVersion, defaultVersion)
	spec[softwaredomain.SpecVersion] = softwaredomain.NormalizeReleaseVersion(specString(spec, softwaredomain.SpecVersion))
}

// setDefaultString stores fallback under key when the value is absent, not a string, or blank, and
// trims a present value.
func setDefaultString(spec map[string]any, key, fallback string) {
	value := strings.TrimSpace(specString(spec, key))
	if value == "" {
		value = fallback
	}
	spec[key] = value
}

// nonNilStrings keeps an empty list an empty array (not null) in trusted vars and stored specs.
func nonNilStrings(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

func setIfPresent(vars map[string]any, key string, spec map[string]any, specKey string) {
	if value := strings.TrimSpace(specString(spec, specKey)); value != "" {
		vars[key] = value
	}
}

func specString(spec map[string]any, key string) string {
	if spec == nil {
		return ""
	}
	if value, ok := spec[key].(string); ok {
		return value
	}
	return ""
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

func cloneSpec(spec map[string]any) map[string]any {
	if spec == nil {
		return nil
	}
	out := make(map[string]any, len(spec))
	for key, value := range spec {
		out[key] = value
	}
	return out
}
