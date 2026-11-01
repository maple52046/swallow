package application

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"testing"
	"time"

	"github.com/maple52046/swallow/internal/core/convergence"
	platformdomain "github.com/maple52046/swallow/internal/core/platform/domain"
	serverdomain "github.com/maple52046/swallow/internal/core/server/domain"
	"github.com/maple52046/swallow/internal/core/store/storetest"
)

// testPlatformModule is a Kubernetes-shaped platform hook for core tests: each member has one role,
// control-plane or worker, and the k0s fields are settings. Adding a member converges; removing one
// or changing k0sVersion is destructive and unsupported; changing timeSyncCheck is non-destructive
// and unsupported; apiVip is immutable.
type testPlatformModule struct{}

func (testPlatformModule) PlatformType() platformdomain.PlatformType {
	return platformdomain.PlatformTypeKubernetes
}

func (testPlatformModule) DeployRequest(spec platformdomain.PlatformSpec) map[string]any {
	body := maps.Clone(spec.Settings)
	if body == nil {
		body = map[string]any{}
	}
	assignments := []any{}
	for _, member := range spec.Members {
		assignments = append(assignments, map[string]any{"serverId": member.ServerID, "role": member.Roles[0]})
	}
	body["roleAssignments"] = assignments
	return body
}

func (testPlatformModule) SpecFromDeployRequest(body map[string]any) (platformdomain.PlatformSpec, error) {
	spec := platformdomain.PlatformSpec{Desired: platformdomain.PlatformPresent, Settings: map[string]any{}}
	for key, value := range body {
		if key != "roleAssignments" && key != "gpuStackOwner" {
			spec.Settings[key] = value
		}
	}
	items, _ := body["roleAssignments"].([]any)
	for _, item := range items {
		assignment := item.(map[string]any)
		spec.Members = append(spec.Members, platformdomain.PlatformMember{
			ServerID: assignment["serverId"].(string), Roles: []string{assignment["role"].(string)},
		})
	}
	return spec, nil
}

func (testPlatformModule) Changes(applied *platformdomain.PlatformSpec, desired platformdomain.PlatformSpec) (platformdomain.PlatformChanges, error) {
	platform := convergence.ResourceRef{Kind: "platform"}
	if desired.Desired == platformdomain.PlatformAbsent {
		return platformdomain.PlatformChanges{Actions: []convergence.PlannedAction{{Target: platform, Action: "uninstall", Destructive: true}},
			Realization: platformdomain.RealizeUninstall}, nil
	}
	if applied == nil {
		return platformdomain.PlatformChanges{Actions: []convergence.PlannedAction{{Target: platform, Action: "install", Destructive: true}},
			Realization: platformdomain.RealizeDeploy}, nil
	}
	if platformdomain.SettingChanged(*applied, desired, "apiVip") {
		return platformdomain.PlatformChanges{}, fmt.Errorf("%w: apiVip", platformdomain.ErrImmutableSpecField)
	}
	var changes platformdomain.PlatformChanges
	added, removed, _ := platformdomain.MemberChanges(*applied, desired)
	for _, member := range added {
		changes.Actions = append(changes.Actions, convergence.PlannedAction{Target: convergence.ResourceRef{Kind: "server", ID: member.ServerID}, Action: "add-member"})
		changes.Realization = platformdomain.RealizeDeploy
	}
	for _, member := range removed {
		changes.Actions = append(changes.Actions, convergence.PlannedAction{Target: convergence.ResourceRef{Kind: "server", ID: member.ServerID}, Action: "remove-member", Destructive: true})
		changes.Unsupported = append(changes.Unsupported, "remove-member")
	}
	if platformdomain.SettingChanged(*applied, desired, "k0sVersion") {
		changes.Actions = append(changes.Actions, convergence.PlannedAction{Target: platform, Action: "upgrade", Destructive: true})
		changes.Unsupported = append(changes.Unsupported, "upgrade")
	}
	if platformdomain.SettingChanged(*applied, desired, "timeSyncCheck") {
		changes.Actions = append(changes.Actions, convergence.PlannedAction{Target: platform, Action: "change-time-sync"})
		changes.Unsupported = append(changes.Unsupported, "change-time-sync")
	}
	return changes, nil
}

func (testPlatformModule) ReportsMembership(member platformdomain.PlatformMember) bool {
	return slices.Contains(member.Roles, "worker")
}

// memorySpecs is the spec store over the in-memory Platform repository.
type memorySpecs struct{ repo *deployFakePlatformRepo }

func (s *memorySpecs) WriteSpec(_ context.Context, platformID string, expected int64, spec platformdomain.PlatformSpec) (int64, error) {
	platform, ok := s.repo.platforms[platformID]
	if !ok {
		return 0, platformdomain.ErrPlatformNotFound
	}
	if platform.Generation != expected {
		return 0, platformdomain.ErrGenerationConflict
	}
	stored := spec.Clone()
	platform.Spec, platform.Generation = &stored, expected+1
	return platform.Generation, nil
}

func (s *memorySpecs) WriteStatus(_ context.Context, platformID string, status platformdomain.PlatformStatus) error {
	platform, ok := s.repo.platforms[platformID]
	if !ok {
		return platformdomain.ErrPlatformNotFound
	}
	platform.Status = status
	return nil
}

// memoryPlans is an in-memory Change Plan repository.
type memoryPlans struct{ plans []*convergence.ChangePlan }

func (r *memoryPlans) Create(_ context.Context, plan *convergence.ChangePlan) error {
	if _, err := r.FindUnfinished(context.Background(), plan.Resource); err == nil {
		return convergence.ErrChangePlanConflict
	}
	stored := *plan
	r.plans = append(r.plans, &stored)
	return nil
}

func (r *memoryPlans) FindByID(_ context.Context, id string) (*convergence.ChangePlan, error) {
	for _, plan := range r.plans {
		if plan.ID == id {
			copied := *plan
			return &copied, nil
		}
	}
	return nil, convergence.ErrChangePlanNotFound
}

func (r *memoryPlans) FindUnfinished(_ context.Context, resource convergence.ResourceRef) (*convergence.ChangePlan, error) {
	for _, plan := range r.plans {
		if plan.Resource == resource && plan.State.Unfinished() {
			copied := *plan
			return &copied, nil
		}
	}
	return nil, convergence.ErrChangePlanNotFound
}

func (r *memoryPlans) List(_ context.Context, filter convergence.ChangePlanFilter) ([]*convergence.ChangePlan, error) {
	var out []*convergence.ChangePlan
	for i := len(r.plans) - 1; i >= 0; i-- {
		plan := r.plans[i]
		if (filter.Resource == convergence.ResourceRef{} || plan.Resource == filter.Resource) &&
			(len(filter.States) == 0 || slices.Contains(filter.States, plan.State)) {
			copied := *plan
			out = append(out, &copied)
		}
	}
	return out, nil
}

func (r *memoryPlans) ListExpired(context.Context, time.Time) ([]*convergence.ChangePlan, error) {
	return nil, nil
}

func (r *memoryPlans) Transition(_ context.Context, id string, from []convergence.ChangePlanState, to convergence.ChangePlanState, transition convergence.PlanTransition) error {
	for _, plan := range r.plans {
		if plan.ID == id {
			if !slices.Contains(from, plan.State) {
				return convergence.ErrChangePlanState
			}
			plan.State, plan.WorkflowID, plan.DecidedBy = to, transition.WorkflowID, transition.DecidedBy
			return nil
		}
	}
	return convergence.ErrChangePlanNotFound
}

// fixedRecords returns one deployment record for every Platform.
type fixedRecords struct {
	record *platformdomain.DeploymentRecord
}

func (r fixedRecords) LatestDeploymentRecord(context.Context, *platformdomain.Platform) (platformdomain.DeploymentRecord, bool, error) {
	if r.record == nil {
		return platformdomain.DeploymentRecord{}, false, nil
	}
	return *r.record, true, nil
}

// readTestRequest reads the Kubernetes deploy request testPlatformModule renders.
func readTestRequest(raw []byte) (DeployPlatformInput, error) {
	var body struct {
		SiteID          string `json:"siteId"`
		Name            string `json:"name"`
		K0sVersion      string `json:"k0sVersion"`
		APIVIP          string `json:"apiVip"`
		RoleAssignments []struct {
			ServerID string `json:"serverId"`
			Role     string `json:"role"`
		} `json:"roleAssignments"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		return DeployPlatformInput{}, err
	}
	input := DeployPlatformInput{
		SiteID: body.SiteID, Name: body.Name, Type: platformdomain.PlatformTypeKubernetes, GPUStackOwner: "provisioning",
		Spec: platformdomain.DeploymentSpec{K0sVersion: body.K0sVersion, APIVIP: body.APIVIP},
	}
	for _, assignment := range body.RoleAssignments {
		input.Spec.RoleAssignments = append(input.Spec.RoleAssignments, platformdomain.RoleAssignment{ServerID: assignment.ServerID, Role: platformdomain.NodeRole(assignment.Role)})
	}
	return input, nil
}

type noWorkflowPlatforms struct{}

func (noWorkflowPlatforms) PlatformOfWorkflow(context.Context, string) (string, error) {
	return "", nil
}

type specHarness struct {
	specs      *PlatformSpecService
	controller *PlatformController
	plans      *convergence.ChangePlanService
	launcher   *recordingLauncher
	uninstalls *recordingUninstallLauncher
	platforms  *deployFakePlatformRepo
	servers    *deployFakeServerRepo
	lifecycle  *deployFakeLifecycleReader
}

func newSpecHarness(record *platformdomain.DeploymentRecord) specHarness {
	servers := append(haServers(), deployedServer("w2", "lab-compute-2", "site-1", "192.168.100.5"))
	deploy, launcher, platforms, serverRepo := newClaimingDeployHarness(servers...)
	launcher.launchIDs = true
	deploy.platformService.AttachPlatformClaims(serverRepo)
	deploy.AttachRequestReader(readTestRequest)
	lifecycle := &deployFakeLifecycleReader{snapshots: map[string]platformdomain.LifecycleSnapshot{}}
	modules := map[platformdomain.PlatformType]platformdomain.PlatformModule{platformdomain.PlatformTypeKubernetes: testPlatformModule{}}
	uninstalls := &recordingUninstallLauncher{}
	uninstall := NewUninstallService(platforms, serverRepo, lifecycle, uninstalls)
	planner := NewPlatformPlanner(platforms, lifecycle, deploy, uninstall, nil, modules)
	plans := convergence.NewChangePlanService(&storetest.Transactor{}, &memoryPlans{}, nil, nil, 0, planner)
	specs := &memorySpecs{repo: platforms}
	return specHarness{
		specs:      NewPlatformSpecService(&storetest.Transactor{}, platforms, specs, serverRepo, deploy, deploy.platformService, plans, nil, modules),
		controller: NewPlatformController(platforms, specs, serverRepo, serverRepo, lifecycle, fixedRecords{record: record}, noWorkflowPlatforms{}, plans, planner, modules),
		plans:      plans, launcher: launcher, uninstalls: uninstalls, platforms: platforms, servers: serverRepo, lifecycle: lifecycle,
	}
}

func labSpec() platformdomain.PlatformSpec {
	return platformdomain.PlatformSpec{
		Members: []platformdomain.PlatformMember{
			{ServerID: "c1", Roles: []string{"control-plane"}}, {ServerID: "c2", Roles: []string{"control-plane"}},
			{ServerID: "c3", Roles: []string{"control-plane"}}, {ServerID: "w1", Roles: []string{"worker"}},
		},
		Settings: map[string]any{"k0sVersion": "v1.36.3+k0s.2", "apiVip": "192.168.100.200"},
	}
}

func (h specHarness) reconcile(t *testing.T, platformID string) {
	t.Helper()
	if err := h.controller.Reconcile(context.Background(), convergence.Request{Key: platformID}); err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
}

// succeed records the Platform's realizing Workflow as succeeded, as the lifecycle reader would.
func (h specHarness) succeed(t *testing.T, platformID string) {
	t.Helper()
	realizing := h.platforms.platforms[platformID].Status.Realizing
	if realizing == nil {
		t.Fatal("no Workflow is realizing the spec")
	}
	h.lifecycle.snapshots[platformID] = platformdomain.LifecycleSnapshot{
		Origin: platformdomain.PlatformOriginDeployed, State: platformdomain.PlatformLifecycleActive,
		Deployment: &platformdomain.LifecycleOperation{
			ID: realizing.WorkflowID, Status: operationSucceeded, RequestedAt: realizing.StartedAt,
			TargetServerIDs: realizing.Spec.MemberIDs(),
		},
	}
	h.reconcile(t, platformID)
}

// installed creates the lab Platform and carries its install out.
func (h specHarness) installed(t *testing.T) *platformdomain.Platform {
	t.Helper()
	created, err := h.specs.Create(context.Background(), CreatePlatformSpecInput{
		SiteID: "site-1", Name: "lab-k0s", Type: platformdomain.PlatformTypeKubernetes, Spec: labSpec(), RequestedBy: "admin",
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if _, err := h.plans.Approve(context.Background(), created.Plan.ID, "admin"); err != nil {
		t.Fatalf("Approve() error = %v", err)
	}
	h.reconcile(t, created.Platform.ID)
	h.succeed(t, created.Platform.ID)
	return h.platforms.platforms[created.Platform.ID]
}

// Creating a Platform validates it, stores it at generation 1 with its members claimed, and
// proposes its install; nothing is launched before the plan is approved.
func TestCreateProposesTheInstallAndClaimsTheMembers(t *testing.T) {
	h := newSpecHarness(nil)
	result, err := h.specs.Create(context.Background(), CreatePlatformSpecInput{
		SiteID: "site-1", Name: "lab-k0s", Type: platformdomain.PlatformTypeKubernetes, Spec: labSpec(), RequestedBy: "admin",
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if result.Plan == nil || result.Plan.State != convergence.PlanPendingApproval || result.Plan.Actions[0].Action != "install" {
		t.Fatalf("plan = %+v, want a pending install", result.Plan)
	}
	stored := h.platforms.platforms[result.Platform.ID]
	if stored.Generation != 1 || stored.Spec == nil || stored.Status.Applied != nil {
		t.Errorf("stored generation %d spec %v applied %v, want generation 1, a spec, nothing applied", stored.Generation, stored.Spec, stored.Status.Applied)
	}
	for _, id := range []string{"c1", "c2", "c3", "w1"} {
		if got := heldBy(h.servers, id); got != result.Platform.ID {
			t.Errorf("%s claim = %q, want %q", id, got, result.Platform.ID)
		}
	}
	if h.launcher.launches != 0 {
		t.Errorf("launches = %d, want none before approval", h.launcher.launches)
	}
	h.reconcile(t, result.Platform.ID)
	if h.launcher.launches != 0 {
		t.Errorf("an unapproved plan launched %d Workflows", h.launcher.launches)
	}
	if realized, _ := convergence.FindCondition(h.platforms.platforms[result.Platform.ID].Status.Conditions, convergence.ConditionRealized); realized.Reason != convergence.ReasonAwaitingApproval {
		t.Errorf("realized reason = %q, want %q", realized.Reason, convergence.ReasonAwaitingApproval)
	}
}

// An approved install deploys onto the stored Platform, and its success records the spec as applied.
func TestApprovedInstallDeploysOntoTheStoredPlatform(t *testing.T) {
	h := newSpecHarness(nil)
	platform := h.installed(t)
	if len(h.platforms.platforms) != 1 {
		t.Errorf("platforms = %d, want the one created", len(h.platforms.platforms))
	}
	if h.launcher.launched == nil || h.launcher.launched.Platform.ID != platform.ID {
		t.Fatalf("launched %+v, want a deploy of %s", h.launcher.launched, platform.ID)
	}
	if platform.Status.Applied == nil || platform.Status.ObservedGeneration != 1 || platform.Status.Realizing != nil {
		t.Errorf("status = %+v, want generation 1 applied", platform.Status)
	}
	if !convergence.IsTrue(platform.Status.Conditions, convergence.ConditionRealized) {
		t.Errorf("realized is not true: %+v", platform.Status.Conditions)
	}
}

// Adding a member is non-destructive: the spec is written without a plan and the controller runs a
// convergent deploy over every member onto the same Platform.
func TestAddingAMemberConvergesWithoutAPlan(t *testing.T) {
	h := newSpecHarness(nil)
	platform := h.installed(t)
	spec := platform.Spec.Clone()
	spec.Members = append(spec.Members, platformdomain.PlatformMember{ServerID: "w2", Roles: []string{"worker"}})
	result, err := h.specs.UpdateSpec(context.Background(), platform.ID, 1, spec)
	if err != nil {
		t.Fatalf("UpdateSpec() error = %v", err)
	}
	if result.Plan != nil || result.Platform.Generation != 2 {
		t.Fatalf("plan %v generation %d, want no plan at generation 2", result.Plan, result.Platform.Generation)
	}
	if got := heldBy(h.servers, "w2"); got != platform.ID {
		t.Errorf("w2 claim = %q, want %q", got, platform.ID)
	}
	h.reconcile(t, platform.ID)
	if h.launcher.launches != 2 || !slices.Contains(h.launcher.launched.TargetServerIDs, "w2") {
		t.Fatalf("launches %d targets %v, want a second deploy that includes w2", h.launcher.launches, h.launcher.launched.TargetServerIDs)
	}
	if len(h.platforms.platforms) != 1 {
		t.Errorf("platforms = %d, want the convergent deploy to reuse the Platform", len(h.platforms.platforms))
	}
	h.reconcile(t, platform.ID)
	if h.launcher.launches != 2 {
		t.Errorf("launches = %d, want the running generation not started again", h.launcher.launches)
	}
	h.succeed(t, platform.ID)
	if applied := h.platforms.platforms[platform.ID].Status.Applied; applied == nil || len(applied.Members) != 5 {
		t.Errorf("applied = %+v, want the five members", applied)
	}
}

// A spec is read without its inline user data, so writing it back with the same mode and no value
// keeps the stored user data.
func TestSpecWriteKeepsStoredUserData(t *testing.T) {
	h := newSpecHarness(nil)
	platform := h.installed(t)
	platform.Spec.MachinePreparation = platformdomain.MachinePreparation{UserDataMode: "replace", UserData: "#cloud-config\n"}
	spec := platform.Spec.Clone()
	spec.MachinePreparation.UserData = ""
	spec.Members = append(spec.Members, platformdomain.PlatformMember{ServerID: "w2", Roles: []string{"worker"}})
	if _, err := h.specs.UpdateSpec(context.Background(), platform.ID, 1, spec); err != nil {
		t.Fatalf("UpdateSpec() error = %v", err)
	}
	if got := h.platforms.platforms[platform.ID].Spec.MachinePreparation.UserData; got != "#cloud-config\n" {
		t.Errorf("user data = %q, want the stored value kept", got)
	}
}

// A destructive change swallow cannot carry out yet is proposed with a blocker, which keeps the plan
// from being approved; a non-destructive one it cannot carry out is refused; an immutable field and
// a stale generation are refused.
func TestSpecWritesSwallowCannotCarryOut(t *testing.T) {
	h := newSpecHarness(nil)
	platform := h.installed(t)

	removed := platform.Spec.Clone()
	removed.Members = append(removed.Members[:3], platformdomain.PlatformMember{ServerID: "w2", Roles: []string{"worker"}})
	result, err := h.specs.UpdateSpec(context.Background(), platform.ID, 1, removed)
	if err != nil {
		t.Fatalf("UpdateSpec(remove member) error = %v", err)
	}
	if result.Plan == nil || len(result.Plan.Blockers) != 1 || result.Plan.Blockers[0] != unsupportedBlocker("remove-member") {
		t.Fatalf("plan = %+v, want one remove-member blocker", result.Plan)
	}
	if _, err := h.plans.Approve(context.Background(), result.Plan.ID, "admin"); !errors.Is(err, convergence.ErrChangePlanBlocked) {
		t.Errorf("Approve(blocked) error = %v, want ErrChangePlanBlocked", err)
	}

	refused := platform.Spec.Clone()
	refused.Settings = maps.Clone(refused.Settings)
	refused.Settings["timeSyncCheck"] = "skip"
	if _, err := h.specs.UpdateSpec(context.Background(), platform.ID, 2, refused); !errors.Is(err, ErrUnsupportedChange) {
		t.Errorf("UpdateSpec(unsupported non-destructive) error = %v, want ErrUnsupportedChange", err)
	}
	immutable := platform.Spec.Clone()
	immutable.Settings = maps.Clone(immutable.Settings)
	immutable.Settings["apiVip"] = "192.168.100.201"
	if _, err := h.specs.UpdateSpec(context.Background(), platform.ID, 2, immutable); !errors.Is(err, platformdomain.ErrImmutableSpecField) {
		t.Errorf("UpdateSpec(immutable) error = %v, want ErrImmutableSpecField", err)
	}
	if _, err := h.specs.UpdateSpec(context.Background(), platform.ID, 1, platform.Spec.Clone()); !errors.Is(err, platformdomain.ErrGenerationConflict) {
		t.Errorf("UpdateSpec(stale generation) error = %v, want ErrGenerationConflict", err)
	}
	if got := h.platforms.platforms[platform.ID].Generation; got != 2 {
		t.Errorf("generation = %d, want only the proposed change written", got)
	}
}

// Deleting a Platform whose install was never approved removes it at once and frees its members;
// deleting an installed one proposes its uninstall, which the approved plan launches.
func TestDeleteDeclaresThePlatformAbsent(t *testing.T) {
	h := newSpecHarness(nil)
	created, err := h.specs.Create(context.Background(), CreatePlatformSpecInput{
		SiteID: "site-1", Name: "draft", Type: platformdomain.PlatformTypeKubernetes, Spec: labSpec(),
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if _, err := h.specs.Delete(context.Background(), created.Platform.ID, nil); err != nil {
		t.Fatalf("Delete(never installed) error = %v", err)
	}
	if _, ok := h.platforms.platforms[created.Platform.ID]; ok || heldBy(h.servers, "c1") != "" {
		t.Fatalf("the never-installed Platform or its claims remain")
	}
	if plan, _ := h.plans.Get(context.Background(), created.Plan.ID); plan.State != convergence.PlanSuperseded {
		t.Errorf("install plan state = %s, want superseded", plan.State)
	}

	platform := h.installed(t)
	result, err := h.specs.Delete(context.Background(), platform.ID, &platformdomain.PlatformRemoval{ReleaseServers: true})
	if err != nil {
		t.Fatalf("Delete(installed) error = %v", err)
	}
	if result.Plan == nil || result.Plan.Actions[0].Action != "uninstall" {
		t.Fatalf("plan = %+v, want an uninstall", result.Plan)
	}
	if _, err := h.plans.Approve(context.Background(), result.Plan.ID, "admin"); err != nil {
		t.Fatalf("Approve() error = %v", err)
	}
	h.reconcile(t, platform.ID)
	if h.uninstalls.launch == nil || !h.uninstalls.launch.DeleteRecord || !h.uninstalls.launch.ReleaseServers {
		t.Fatalf("uninstall launch = %+v, want one that releases the members and deletes the record", h.uninstalls.launch)
	}
	if len(result.Plan.Effects) != 1 {
		t.Errorf("plan effects = %v, want the release named", result.Plan.Effects)
	}
}

// A Platform deployed before specs existed is adopted from its deployment record at its first
// reconcile: an active one has its spec applied at generation 1.
func TestControllerAdoptsADeployedPlatform(t *testing.T) {
	record := &platformdomain.DeploymentRecord{Request: map[string]any{
		"k0sVersion": "v1.36.3+k0s.2", "apiVip": "192.168.100.200", "apiVipPrefix": 24,
		"roleAssignments": []any{map[string]any{"serverId": "c1", "role": "control-plane"}, map[string]any{"serverId": "w1", "role": "worker"}},
	}}
	h := newSpecHarness(record)
	platform := &platformdomain.Platform{ID: "legacy", SiteID: "site-1", Name: "legacy", Type: platformdomain.PlatformTypeKubernetes}
	h.platforms.platforms[platform.ID] = platform
	h.lifecycle.snapshots[platform.ID] = platformdomain.LifecycleSnapshot{
		Origin: platformdomain.PlatformOriginDeployed, State: platformdomain.PlatformLifecycleActive,
		Deployment: &platformdomain.LifecycleOperation{ID: "deploy-1", Status: operationSucceeded},
	}
	h.reconcile(t, platform.ID)
	if platform.Generation != 1 || platform.Spec == nil || len(platform.Spec.Members) != 2 {
		t.Fatalf("adopted generation %d spec %+v, want generation 1 with two members", platform.Generation, platform.Spec)
	}
	if platform.Spec.Settings["apiVip"] != "192.168.100.200" || platform.Spec.Settings["apiVipPrefix"] != 24 {
		t.Errorf("adopted settings = %v", platform.Spec.Settings)
	}
	if platform.Status.Applied == nil || platform.Status.ObservedGeneration != 1 {
		t.Errorf("status = %+v, want the adopted spec applied", platform.Status)
	}
	h.reconcile(t, platform.ID)
	if heldBy(h.servers, "c1") != platform.ID || heldBy(h.servers, "w1") != platform.ID {
		t.Errorf("adopted members are not claimed")
	}
}

// A realizing Workflow that failed is not started again for the same generation, and the members
// the platform does not report are drift.
func TestControllerDoesNotRestartAFailedWorkflowAndReportsDrift(t *testing.T) {
	h := newSpecHarness(nil)
	platform := h.installed(t)
	drifted, _ := convergence.FindCondition(platform.Status.Conditions, convergence.ConditionDrifted)
	if drifted.Status != convergence.ConditionTrue {
		t.Errorf("drifted = %+v, want w1 reported missing", drifted)
	}
	spec := platform.Spec.Clone()
	spec.Members = append(spec.Members, platformdomain.PlatformMember{ServerID: "w2", Roles: []string{"worker"}})
	if _, err := h.specs.UpdateSpec(context.Background(), platform.ID, 1, spec); err != nil {
		t.Fatalf("UpdateSpec() error = %v", err)
	}
	h.reconcile(t, platform.ID)
	realizing := platform.Status.Realizing
	h.lifecycle.snapshots[platform.ID] = platformdomain.LifecycleSnapshot{
		Origin: platformdomain.PlatformOriginDeployed, State: platformdomain.PlatformLifecycleDeployFailed,
		Deployment: &platformdomain.LifecycleOperation{ID: realizing.WorkflowID, Status: operationFailed, RequestedAt: realizing.StartedAt},
	}
	h.reconcile(t, platform.ID)
	if h.launcher.launches != 2 {
		t.Errorf("launches = %d, want the failed generation not started again", h.launcher.launches)
	}
	realized, _ := convergence.FindCondition(platform.Status.Conditions, convergence.ConditionRealized)
	if realized.Reason != convergence.ReasonFailed {
		t.Errorf("realized = %+v, want failed", realized)
	}
}

var _ serverdomain.PlatformClaimStore = (*deployFakeServerRepo)(nil)

// heldBy is the Platform that claims a Server in the in-memory repository, "" for none.
func heldBy(repo *deployFakeServerRepo, id string) string {
	if claim := repo.servers[id].PlatformClaim; claim != nil {
		return claim.PlatformID
	}
	return ""
}
