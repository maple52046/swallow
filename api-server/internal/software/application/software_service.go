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

// SoftwareService owns Managed Software policy and drives the launcher. It never talks to Temporal
// directly; it validates intent, records the swallow-owned assignment, and delegates orchestration.
type SoftwareService struct {
	assignments softwaredomain.AssignmentRepository
	servers     serverdomain.ServerRepository
	membership  MembershipChecker
	launcher    SoftwareLauncher
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

// Install validates the request against the catalog, the target Servers' state, mutual exclusion,
// and Kubernetes membership, then launches the install Workflow and records a pending assignment
// per target. It returns the Workflow (operation) id.
func (s *SoftwareService) Install(ctx context.Context, input InstallInput) (string, error) {
	if s.launcher == nil {
		return "", fmt.Errorf("%w: software orchestration is unavailable", ErrInvalidRequest)
	}
	entry, ok := softwaredomain.LookupKind(input.Kind)
	if !ok {
		return "", fmt.Errorf("%w: %s", softwaredomain.ErrUnknownKind, input.Kind)
	}
	if len(input.Targets) == 0 {
		return "", fmt.Errorf("%w: at least one target Server is required", ErrInvalidRequest)
	}

	rolesByServer := make(map[string][]softwaredomain.Role, len(input.Targets))
	serverIDs := make([]string, 0, len(input.Targets))
	seen := map[string]bool{}
	for _, target := range input.Targets {
		if strings.TrimSpace(target.ServerID) == "" {
			return "", fmt.Errorf("%w: each target requires a serverId", ErrInvalidRequest)
		}
		if seen[target.ServerID] {
			return "", fmt.Errorf("%w: duplicate target %s", ErrInvalidRequest, target.ServerID)
		}
		seen[target.ServerID] = true
		if err := validateRoles(entry, target.Roles); err != nil {
			return "", err
		}
		rolesByServer[target.ServerID] = normalizeRoles(target.Roles)
		serverIDs = append(serverIDs, target.ServerID)
	}
	sort.Strings(serverIDs)

	if err := validateSpec(entry, rolesByServer, input.Spec); err != nil {
		return "", err
	}
	// The normalized spec is both what the playbook receives and what the assignment records, so
	// the record never disagrees with what was applied (an omitted enableApi is recorded as true).
	spec := normalizeSpec(entry.Kind, input.Spec)

	// Precondition checks give clean, specific errors before a Workflow is created. The launcher's
	// PrepareAnsibleStep re-checks the deployed state and lock at execution acceptance, so a race
	// after this point still cannot start work against an ineligible host.
	for _, serverID := range serverIDs {
		server, err := s.servers.FindByID(ctx, serverID)
		if err != nil {
			return "", err
		}
		if server.Absent || server.Provisioning == nil || server.Provisioning.State != "deployed" {
			return "", fmt.Errorf("%w: %s is not deployed", ErrInvalidRequest, server.DisplayName())
		}
		if entry.RefusedForKubernetesMembers && s.membership != nil {
			member, err := s.membership.IsKubernetesMember(ctx, serverID)
			if err != nil {
				return "", err
			}
			if member {
				return "", fmt.Errorf("%w: %s is a Kubernetes platform member", softwaredomain.ErrKubernetesMember, server.DisplayName())
			}
		}
		if err := s.checkMutualExclusion(ctx, entry, serverID); err != nil {
			return "", err
		}
	}

	launch := SoftwareLaunch{
		Kind: input.Kind, ServerIDs: serverIDs, RolesByServer: rolesByServer,
		TrustedVars: buildInstallVars(input.Kind, rolesByServer, spec), RequestedBy: input.RequestedBy,
	}
	workflowID, err := s.launcher.LaunchInstall(ctx, launch)
	if err != nil {
		return "", err
	}

	// Record a pending assignment per target after the Workflow is accepted. The install
	// Workflow's record-software-assignment step flips these to installed on success; a failed
	// install step is marked failed by the step observer. Writing after a successful launch keeps
	// a rejected request (busy/locked target) from leaving orphaned assignments.
	now := time.Now().UTC()
	for _, serverID := range serverIDs {
		server, err := s.servers.FindByID(ctx, serverID)
		if err != nil {
			return workflowID, err
		}
		assignment := &softwaredomain.Assignment{
			ServerID: serverID, SiteID: server.Source.SiteID, Kind: input.Kind,
			Roles: rolesByServer[serverID], Spec: cloneSpec(spec),
			State: softwaredomain.StatePending, LastWorkflowID: workflowID,
			LastAppliedAt: s.previousAppliedAt(ctx, serverID, input.Kind),
			CreatedAt:     now, UpdatedAt: now,
		}
		if err := s.assignments.Upsert(ctx, assignment); err != nil {
			return workflowID, err
		}
	}
	return workflowID, nil
}

// Uninstall validates that each target has a live assignment of the kind, launches the uninstall
// Workflow, and marks the assignments uninstalling. The clear-software-assignment step marks them
// absent on success.
func (s *SoftwareService) Uninstall(ctx context.Context, input UninstallInput) (string, error) {
	if s.launcher == nil {
		return "", fmt.Errorf("%w: software orchestration is unavailable", ErrInvalidRequest)
	}
	if _, ok := softwaredomain.LookupKind(input.Kind); !ok {
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
	}
	return nil
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
	}
	return vars
}

// normalizeSpec applies the kind-specific defaults to an already validated install spec and returns
// a copy. Docker CE's enableApi defaults to true (decision 043), recorded explicitly so a later read
// can tell "enabled by default" from a legacy record that predates the variant.
func normalizeSpec(kind softwaredomain.Kind, spec map[string]any) map[string]any {
	out := cloneSpec(spec)
	if kind != softwaredomain.KindDockerCE {
		return out
	}
	if out == nil {
		out = map[string]any{}
	}
	if value, ok := out[softwaredomain.SpecEnableAPI]; !ok || value == nil {
		out[softwaredomain.SpecEnableAPI] = true
	}
	return out
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
