package application

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/maple52046/swallow/internal/core/convergence"
	"github.com/maple52046/swallow/internal/core/convergence/convergencetest"
	platformdomain "github.com/maple52046/swallow/internal/core/platform/domain"
	serverdomain "github.com/maple52046/swallow/internal/core/server/domain"
	"github.com/maple52046/swallow/internal/core/store"
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
	if platformdomain.SettingChanged(*applied, desired, "gpuOperator") {
		changes.Actions = append(changes.Actions, convergence.PlannedAction{Target: platform, Action: "change-gpu-operator", Destructive: true})
		changes.Realization = platformdomain.RealizeGPUOperator
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
		if (filter.Resource.Kind == "" || plan.Resource.Kind == filter.Resource.Kind) &&
			(filter.Resource.ID == "" || plan.Resource.ID == filter.Resource.ID) &&
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

// recordedWorkflows is the Workflow reader of the harness: a Workflow's outcome is what the test sets.
type recordedWorkflows struct{ outcomes map[string]string }

func (recordedWorkflows) PlatformOfWorkflow(context.Context, string) (string, error) {
	return "", nil
}

func (w recordedWorkflows) WorkflowOutcome(_ context.Context, _, workflowID string) (string, error) {
	return w.outcomes[workflowID], nil
}

// recordingCanceler reports the Workflows in unfinished as still running and records each cancel
// request.
type recordingCanceler struct {
	unfinished map[string]bool
	canceled   []string
}

func (c *recordingCanceler) CancelUnfinished(_ context.Context, _, workflowID string) (bool, error) {
	if !c.unfinished[workflowID] {
		return false, nil
	}
	c.canceled = append(c.canceled, workflowID)
	return true, nil
}

// recordingRequester counts the convergence requests a spec write makes for each controller.
type recordingRequester struct{ requests map[string]int }

func (r *recordingRequester) Request(_ context.Context, controller string) error {
	r.requests[controller]++
	return nil
}

type specHarness struct {
	specs      *PlatformSpecService
	controller *PlatformController
	plans      *convergence.ChangePlanService
	policies   *convergencetest.Policies
	requester  *recordingRequester
	launcher   *recordingLauncher
	uninstalls *recordingUninstallLauncher
	platforms  *deployFakePlatformRepo
	servers    *deployFakeServerRepo
	lifecycle  *deployFakeLifecycleReader
	workflows  recordedWorkflows
	canceler   *recordingCanceler
}

func newSpecHarness(record *platformdomain.DeploymentRecord) specHarness {
	servers := append(haServers(), deployedServer("w2", "lab-compute-2", "site-1", "192.168.100.5"),
		deployedServer("w3", "lab-compute-3", "site-1", "192.168.100.6"))
	deploy, launcher, platforms, serverRepo := newClaimingDeployHarness(servers...)
	launcher.launchIDs = true
	lifecycle := &deployFakeLifecycleReader{snapshots: map[string]platformdomain.LifecycleSnapshot{}}
	deploy.platformService.lifecycle = lifecycle
	modules := map[platformdomain.PlatformType]platformdomain.PlatformModule{platformdomain.PlatformTypeKubernetes: testPlatformModule{}}
	uninstalls := &recordingUninstallLauncher{}
	uninstall := NewUninstallService(platforms, serverRepo, lifecycle, uninstalls, storedLockGuard{servers: serverRepo})
	planner := NewPlatformPlanner(platforms, lifecycle, deploy, uninstall, nil, modules)
	policies := &convergencetest.Policies{}
	plans, err := convergence.NewChangePlanService(convergence.ChangePlanServiceDeps{
		Transactor: &storetest.Transactor{}, Plans: &memoryPlans{}, Policies: policies, Planners: []convergence.Planner{planner},
	})
	if err != nil {
		panic(err)
	}
	specs := &memorySpecs{repo: platforms}
	workflows := recordedWorkflows{outcomes: map[string]string{}}
	canceler := &recordingCanceler{unfinished: map[string]bool{}}
	requester := &recordingRequester{requests: map[string]int{}}
	return specHarness{
		specs:      NewPlatformSpecService(&storetest.Transactor{}, platforms, specs, serverRepo, deploy, deploy.platformService, plans, requester, modules),
		controller: NewPlatformController(platforms, specs, serverRepo, serverRepo, lifecycle, fixedRecords{record: record}, workflows, canceler, plans, planner, modules),
		plans:      plans, policies: policies, requester: requester, launcher: launcher, uninstalls: uninstalls, platforms: platforms,
		servers: serverRepo, lifecycle: lifecycle, workflows: workflows, canceler: canceler,
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

// A change realized by a Workflow that is neither a deploy nor an uninstall (a GPU Operator change) is
// applied once that Workflow succeeds, though the Platform's lifecycle never sees it.
func TestAChangeRealizedOutsideTheLifecycleIsAppliedWhenItsWorkflowSucceeds(t *testing.T) {
	h := newSpecHarness(nil)
	platform := h.installed(t)
	changed := platform.Spec.Clone()
	changed.Settings["gpuOperator"] = map[string]any{"enabled": true}
	generation, err := (&memorySpecs{repo: h.platforms}).WriteSpec(context.Background(), platform.ID, platform.Generation, changed)
	if err != nil {
		t.Fatalf("WriteSpec() error = %v", err)
	}
	platform.Status.Realizing = &platformdomain.PlatformRealizing{
		Generation: generation, WorkflowID: "wf-gpu", Spec: changed.Clone(), StartedAt: time.Now().UTC(),
	}

	h.reconcile(t, platform.ID)
	if platform.Status.ObservedGeneration == generation {
		t.Fatal("the change was applied before its Workflow finished")
	}
	h.workflows.outcomes["wf-gpu"] = "succeeded"
	h.reconcile(t, platform.ID)
	if platform.Status.ObservedGeneration != generation || platform.Status.Realizing != nil ||
		!reflect.DeepEqual(platform.Status.Applied.Settings["gpuOperator"], changed.Settings["gpuOperator"]) {
		t.Errorf("status = observed %d realizing %+v applied %v, want generation %d applied",
			platform.Status.ObservedGeneration, platform.Status.Realizing, platform.Status.Applied.Settings, generation)
	}
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
	if target := result.Plan.Actions[0].Target; target != platformRef(result.Platform.ID) {
		t.Errorf("install target = %+v, want the created Platform", target)
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

// A create its user approved stores its install approved, wakes the controller once committed, and
// the next reconcile starts the deploy without a separate approval. While a hold keeps it from
// starting, the Platform reports converging, not awaiting approval.
func TestCreateApprovedWithTheRequestStartsTheInstall(t *testing.T) {
	h := newSpecHarness(nil)
	result, err := h.specs.Create(context.Background(), CreatePlatformSpecInput{
		SiteID: "site-1", Name: "lab-k0s", Type: platformdomain.PlatformTypeKubernetes, Spec: labSpec(), RequestedBy: "admin", ApprovedBy: "admin",
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if result.Plan.State != convergence.PlanApproved || result.Plan.DecidedBy != "admin" {
		t.Fatalf("plan = %s decided by %q, want approved by admin", result.Plan.State, result.Plan.DecidedBy)
	}
	if got := h.requester.requests[PlatformControllerName]; got != 1 {
		t.Errorf("platform convergence requests = %d, want 1 after the approved create", got)
	}
	h.lifecycle.snapshots[result.Platform.ID] = platformdomain.LifecycleSnapshot{State: platformdomain.PlatformLifecycleDeploying}
	h.reconcile(t, result.Platform.ID)
	if realized, _ := convergence.FindCondition(h.platforms.platforms[result.Platform.ID].Status.Conditions, convergence.ConditionRealized); realized.Reason != convergence.ReasonConverging {
		t.Errorf("held approved plan: realized reason = %q, want %q", realized.Reason, convergence.ReasonConverging)
	}
	delete(h.lifecycle.snapshots, result.Platform.ID)
	h.reconcile(t, result.Platform.ID)
	if h.launcher.launches != 1 || h.launcher.launched.Platform.ID != result.Platform.ID {
		t.Fatalf("launches = %d, want the install deployed without a separate approval", h.launcher.launches)
	}
}

// With the safety switch on, a create its user approved still waits for approval and says why;
// the controller is woken only to report it, and nothing is launched.
func TestTheSafetySwitchKeepsAnApprovedCreateWaiting(t *testing.T) {
	h := newSpecHarness(nil)
	h.policies.RequireSeparateApproval()
	result, err := h.specs.Create(context.Background(), CreatePlatformSpecInput{
		SiteID: "site-1", Name: "lab-k0s", Type: platformdomain.PlatformTypeKubernetes, Spec: labSpec(), RequestedBy: "admin", ApprovedBy: "admin",
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if result.Plan.State != convergence.PlanPendingApproval || !strings.Contains(result.Plan.StateReason, "policy") {
		t.Fatalf("plan = %s %q, want pending because of the policy", result.Plan.State, result.Plan.StateReason)
	}
	if got := h.requester.requests[PlatformControllerName]; got != 1 {
		t.Errorf("platform convergence requests = %d, want 1 so the waiting plan is reported at once", got)
	}
	h.reconcile(t, result.Platform.ID)
	if h.launcher.launches != 0 {
		t.Errorf("launches = %d, want none before approval", h.launcher.launches)
	}
	realized, _ := convergence.FindCondition(h.platforms.platforms[result.Platform.ID].Status.Conditions, convergence.ConditionRealized)
	if realized.Reason != convergence.ReasonAwaitingApproval || !strings.Contains(realized.Message, result.Plan.StateReason) {
		t.Errorf("realized = %q %q, want awaiting approval naming why", realized.Reason, realized.Message)
	}
}

// An uninstall its user approved starts at the next reconcile. An uninstall left waiting is approved
// by repeating the delete with the same removal and approval.
func TestDeleteApprovedWithTheRequestStartsTheUninstall(t *testing.T) {
	t.Run("approved with the delete", func(t *testing.T) {
		h := newSpecHarness(nil)
		platform := h.installed(t)
		result, err := h.specs.Delete(context.Background(), platform.ID, &platformdomain.PlatformRemoval{ReleaseServers: true}, "admin")
		if err != nil {
			t.Fatalf("Delete() error = %v", err)
		}
		if result.Plan.State != convergence.PlanApproved {
			t.Fatalf("plan = %s, want approved with the delete", result.Plan.State)
		}
		h.reconcile(t, platform.ID)
		if h.uninstalls.launch == nil || !h.uninstalls.launch.ReleaseServers {
			t.Errorf("uninstall launch = %+v, want one that releases the members", h.uninstalls.launch)
		}
	})
	t.Run("approved by repeating the delete", func(t *testing.T) {
		h := newSpecHarness(nil)
		platform := h.installed(t)
		first, err := h.specs.Delete(context.Background(), platform.ID, &platformdomain.PlatformRemoval{ReleaseServers: true}, "")
		if err != nil || first.Plan.State != convergence.PlanPendingApproval {
			t.Fatalf("Delete() = %+v, %v; want a waiting uninstall", first, err)
		}
		generation := first.Platform.Generation
		h.reconcile(t, platform.ID)
		if h.uninstalls.launch != nil {
			t.Fatalf("an uninstall nobody approved was launched")
		}
		again, err := h.specs.Delete(context.Background(), platform.ID, &platformdomain.PlatformRemoval{ReleaseServers: true}, "admin")
		if err != nil {
			t.Fatalf("Delete(again) error = %v", err)
		}
		if again.Plan.ID != first.Plan.ID || again.Plan.State != convergence.PlanApproved {
			t.Fatalf("Delete(again) plan = %s %s, want %s approved", again.Plan.ID, again.Plan.State, first.Plan.ID)
		}
		if got := h.platforms.platforms[platform.ID].Generation; got != generation {
			t.Errorf("generation = %d, want %d: approving the waiting plan writes no spec", got, generation)
		}
		h.reconcile(t, platform.ID)
		if h.uninstalls.launch == nil || !h.uninstalls.launch.ReleaseServers {
			t.Errorf("uninstall launch = %+v, want the stored removal that releases the members", h.uninstalls.launch)
		}
	})
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
	result, err := h.specs.UpdateSpec(context.Background(), platform.ID, 1, spec, "")
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

// A spec write is validated as a deployment, so adding a member that runs from RAM to a Platform
// whose members run from disk is refused before anything is written or claimed: the convergent
// redeploy would otherwise fail in install-platform.
func TestAddingARAMDeployedMemberToADiskPlatformIsRefused(t *testing.T) {
	h := newSpecHarness(nil)
	platform := h.installed(t)
	h.servers.servers["w2"].Provisioning.Ephemeral = true
	spec := platform.Spec.Clone()
	spec.Members = append(spec.Members, platformdomain.PlatformMember{ServerID: "w2", Roles: []string{"worker"}})

	if _, err := h.specs.UpdateSpec(context.Background(), platform.ID, 1, spec, ""); !errors.Is(err, platformdomain.ErrInvalidDeployment) {
		t.Fatalf("UpdateSpec() error = %v, want ErrInvalidDeployment", err)
	}
	if got := h.platforms.platforms[platform.ID].Generation; got != 1 {
		t.Errorf("generation = %d, want the refused spec not written", got)
	}
	if got := heldBy(h.servers, "w2"); got != "" {
		t.Errorf("w2 claim = %q, want none", got)
	}
	h.reconcile(t, platform.ID)
	if h.launcher.launches != 1 {
		t.Errorf("launches = %d, want only the install", h.launcher.launches)
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
	if _, err := h.specs.UpdateSpec(context.Background(), platform.ID, 1, spec, ""); err != nil {
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
	result, err := h.specs.UpdateSpec(context.Background(), platform.ID, 1, removed, "admin")
	if err != nil {
		t.Fatalf("UpdateSpec(remove member) error = %v", err)
	}
	if result.Plan == nil || len(result.Plan.Blockers) != 1 || result.Plan.Blockers[0] != unsupportedBlocker("remove-member") {
		t.Fatalf("plan = %+v, want one remove-member blocker", result.Plan)
	}
	if result.Plan.State != convergence.PlanPendingApproval {
		t.Errorf("blocked plan approved with the write = %s, want it left waiting", result.Plan.State)
	}
	if _, err := h.plans.Approve(context.Background(), result.Plan.ID, "admin"); !errors.Is(err, convergence.ErrChangePlanBlocked) {
		t.Errorf("Approve(blocked) error = %v, want ErrChangePlanBlocked", err)
	}

	refused := platform.Spec.Clone()
	refused.Settings = maps.Clone(refused.Settings)
	refused.Settings["timeSyncCheck"] = "skip"
	if _, err := h.specs.UpdateSpec(context.Background(), platform.ID, 2, refused, ""); !errors.Is(err, ErrUnsupportedChange) {
		t.Errorf("UpdateSpec(unsupported non-destructive) error = %v, want ErrUnsupportedChange", err)
	}
	immutable := platform.Spec.Clone()
	immutable.Settings = maps.Clone(immutable.Settings)
	immutable.Settings["apiVip"] = "192.168.100.201"
	if _, err := h.specs.UpdateSpec(context.Background(), platform.ID, 2, immutable, ""); !errors.Is(err, platformdomain.ErrImmutableSpecField) {
		t.Errorf("UpdateSpec(immutable) error = %v, want ErrImmutableSpecField", err)
	}
	if _, err := h.specs.UpdateSpec(context.Background(), platform.ID, 1, platform.Spec.Clone(), ""); !errors.Is(err, platformdomain.ErrGenerationConflict) {
		t.Errorf("UpdateSpec(stale generation) error = %v, want ErrGenerationConflict", err)
	}
	if got := h.platforms.platforms[platform.ID].Generation; got != 2 {
		t.Errorf("generation = %d, want only the proposed change written", got)
	}
}

// Deleting a Platform whose install never started removes it at once and frees its members;
// deleting an installed one proposes its uninstall, which the approved plan launches.
func TestDeleteDeclaresThePlatformAbsent(t *testing.T) {
	h := newSpecHarness(nil)
	created, err := h.specs.Create(context.Background(), CreatePlatformSpecInput{
		SiteID: "site-1", Name: "draft", Type: platformdomain.PlatformTypeKubernetes, Spec: labSpec(),
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if _, err := h.specs.Delete(context.Background(), created.Platform.ID, nil, ""); err != nil {
		t.Fatalf("Delete(never installed) error = %v", err)
	}
	if _, ok := h.platforms.platforms[created.Platform.ID]; ok || heldBy(h.servers, "c1") != "" {
		t.Fatalf("the never-installed Platform or its claims remain")
	}
	if plan, _ := h.plans.Get(context.Background(), created.Plan.ID); plan.State != convergence.PlanSuperseded {
		t.Errorf("install plan state = %s, want superseded", plan.State)
	}

	platform := h.installed(t)
	result, err := h.specs.Delete(context.Background(), platform.ID, &platformdomain.PlatformRemoval{ReleaseServers: true}, "")
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
	wantEffects := []string{"4 members are released to the provisioner, which removes their operating systems; " +
		"their disks are not erased and their static IP bindings are kept. The Platform record is deleted once the release succeeds."}
	if !slices.Equal(result.Plan.Effects, wantEffects) {
		t.Errorf("plan effects = %q, want %q", result.Plan.Effects, wantEffects)
	}
}

// A Platform whose deployment started may have changed its members, so deleting it proposes its
// uninstall instead of deleting the record, whether the deployment still runs or failed. The approved
// plan uninstalls over a running deployment its user agreed to cancel, and supersedes a failed one.
func TestDeleteUninstallsAPlatformWhoseDeploymentStarted(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status string
		state  platformdomain.PlatformLifecycleState
	}{
		{name: "deploying", status: "running", state: platformdomain.PlatformLifecycleDeploying},
		{name: "deploy failed", status: operationFailed, state: platformdomain.PlatformLifecycleDeployFailed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newSpecHarness(nil)
			created, err := h.specs.Create(context.Background(), CreatePlatformSpecInput{
				SiteID: "site-1", Name: "lab-k0s", Type: platformdomain.PlatformTypeKubernetes, Spec: labSpec(), RequestedBy: "admin", ApprovedBy: "admin",
			})
			if err != nil {
				t.Fatalf("Create() error = %v", err)
			}
			id := created.Platform.ID
			h.reconcile(t, id)
			realizing := h.platforms.platforms[id].Status.Realizing
			if realizing == nil {
				t.Fatal("the approved install did not start")
			}
			h.lifecycle.snapshots[id] = platformdomain.LifecycleSnapshot{
				Origin: platformdomain.PlatformOriginDeployed, State: tc.state,
				Deployment: &platformdomain.LifecycleOperation{
					ID: realizing.WorkflowID, Status: tc.status, RequestedAt: realizing.StartedAt, TargetServerIDs: realizing.Spec.MemberIDs(),
				},
			}

			removal := &platformdomain.PlatformRemoval{ReleaseServers: true, CancelActiveWorkflows: true}
			result, err := h.specs.Delete(context.Background(), id, removal, "admin")
			if err != nil {
				t.Fatalf("Delete() error = %v", err)
			}
			if result.Platform == nil || result.Plan == nil || result.Plan.Actions[0].Action != "uninstall" || result.Plan.State != convergence.PlanApproved {
				t.Fatalf("Delete() = %+v, want the Platform with its uninstall approved", result)
			}
			if _, ok := h.platforms.platforms[id]; !ok {
				t.Fatal("the record was deleted before its uninstall")
			}
			h.reconcile(t, id)
			if launch := h.uninstalls.launch; launch == nil || !launch.CancelActiveWorkflows || !launch.ReleaseServers || !launch.DeleteRecord {
				t.Errorf("uninstall launch = %+v, want one that cancels the deployment, releases the members, and deletes the record", launch)
			}
		})
	}
}

// uninstallReports records the Platform's realizing uninstall in its lifecycle with status, after
// the deployment that installed it, as the lifecycle reader would.
func (h specHarness) uninstallReports(t *testing.T, platformID, status string, state platformdomain.PlatformLifecycleState) {
	t.Helper()
	realizing := h.platforms.platforms[platformID].Status.Realizing
	if realizing == nil {
		t.Fatal("no uninstall is realizing the spec")
	}
	snapshot := h.lifecycle.snapshots[platformID]
	snapshot.State = state
	snapshot.Uninstall = &platformdomain.LifecycleOperation{
		ID: realizing.WorkflowID, Status: status, RequestedAt: snapshot.Deployment.RequestedAt.Add(time.Minute),
	}
	h.lifecycle.snapshots[platformID] = snapshot
}

// Deleting a Platform already marked absent: the same removal returns the waiting plan, and a
// different one proposes the uninstall again with it. With no plan waiting, because it was rejected or
// its uninstall failed, the uninstall is proposed again; while the uninstall runs nothing changes; and
// a Platform whose uninstall succeeded is deleted at once.
func TestDeletingAnAbsentPlatformAgain(t *testing.T) {
	ctx := context.Background()
	t.Run("the same removal returns the waiting plan", func(t *testing.T) {
		h := newSpecHarness(nil)
		platform := h.installed(t)
		first, err := h.specs.Delete(ctx, platform.ID, nil, "")
		if err != nil {
			t.Fatalf("Delete() error = %v", err)
		}
		generation := platform.Generation
		// Release options mean nothing unless the members are released.
		again, err := h.specs.Delete(ctx, platform.ID, &platformdomain.PlatformRemoval{ReleaseOptions: platformdomain.ServerReleaseOptions{Erase: true}}, "")
		if err != nil {
			t.Fatalf("Delete(again) error = %v", err)
		}
		if again.Plan == nil || again.Plan.ID != first.Plan.ID || platform.Generation != generation {
			t.Errorf("Delete(again) plan %+v at generation %d, want %s at generation %d", again.Plan, platform.Generation, first.Plan.ID, generation)
		}
	})
	t.Run("a different removal replaces the waiting plan", func(t *testing.T) {
		h := newSpecHarness(nil)
		platform := h.installed(t)
		first, err := h.specs.Delete(ctx, platform.ID, nil, "")
		if err != nil {
			t.Fatalf("Delete() error = %v", err)
		}
		second, err := h.specs.Delete(ctx, platform.ID, &platformdomain.PlatformRemoval{ReleaseServers: true}, "")
		if err != nil {
			t.Fatalf("Delete(release) error = %v", err)
		}
		if second.Plan == nil || second.Plan.ID == first.Plan.ID || second.Plan.Generation != first.Plan.Generation+1 {
			t.Fatalf("Delete(release) plan = %+v, want a new plan at generation %d", second.Plan, first.Plan.Generation+1)
		}
		if len(second.Plan.Effects) == 0 || !strings.Contains(second.Plan.Effects[0], "released to the provisioner") {
			t.Errorf("new plan effects = %q, want the release", second.Plan.Effects)
		}
		if old, _ := h.plans.Get(ctx, first.Plan.ID); old.State != convergence.PlanSuperseded {
			t.Errorf("first plan state = %s, want superseded", old.State)
		}
		if removal := platform.Spec.Removal; removal == nil || !removal.ReleaseServers {
			t.Errorf("stored removal = %+v, want the release", removal)
		}
	})
	t.Run("a rejected uninstall is proposed again", func(t *testing.T) {
		h := newSpecHarness(nil)
		platform := h.installed(t)
		first, err := h.specs.Delete(ctx, platform.ID, nil, "")
		if err != nil {
			t.Fatalf("Delete() error = %v", err)
		}
		if _, err := h.plans.Reject(ctx, first.Plan.ID, "admin"); err != nil {
			t.Fatalf("Reject() error = %v", err)
		}
		h.reconcile(t, platform.ID)
		realized, _ := convergence.FindCondition(platform.Status.Conditions, convergence.ConditionRealized)
		if realized.Reason != ReasonPlanRequired || !strings.Contains(realized.Message, "delete the Platform again") {
			t.Errorf("realized = %q %q, want plan_required telling the user to delete again", realized.Reason, realized.Message)
		}
		again, err := h.specs.Delete(ctx, platform.ID, nil, "admin")
		if err != nil {
			t.Fatalf("Delete(again) error = %v", err)
		}
		if again.Plan == nil || again.Plan.ID == first.Plan.ID || again.Plan.State != convergence.PlanApproved {
			t.Fatalf("Delete(again) plan = %+v, want a new plan approved with the request", again.Plan)
		}
		h.reconcile(t, platform.ID)
		if h.uninstalls.launch == nil {
			t.Error("the uninstall proposed again was not launched")
		}
	})
	t.Run("a failed uninstall is proposed again and replaces it", func(t *testing.T) {
		h := newSpecHarness(nil)
		platform := h.installed(t)
		if _, err := h.specs.Delete(ctx, platform.ID, nil, "admin"); err != nil {
			t.Fatalf("Delete() error = %v", err)
		}
		h.reconcile(t, platform.ID)
		failed := platform.Status.Realizing
		if failed == nil || h.uninstalls.launch == nil {
			t.Fatal("the approved uninstall did not start")
		}
		h.uninstallReports(t, platform.ID, operationRequiresAttention, platformdomain.PlatformLifecycleUninstallFailed)
		h.canceler.unfinished[failed.WorkflowID] = true
		h.reconcile(t, platform.ID)
		h.uninstalls.launch = nil

		again, err := h.specs.Delete(ctx, platform.ID, nil, "admin")
		if err != nil {
			t.Fatalf("Delete(again) error = %v", err)
		}
		if again.Plan == nil || again.Plan.State != convergence.PlanApproved || again.Plan.Generation != failed.Generation+1 {
			t.Fatalf("Delete(again) plan = %+v, want an approved plan at generation %d", again.Plan, failed.Generation+1)
		}
		h.reconcile(t, platform.ID)
		if !slices.Equal(h.canceler.canceled, []string{failed.WorkflowID}) || h.uninstalls.launch != nil {
			t.Fatalf("canceled %v launch %+v, want the failed uninstall canceled before a new one", h.canceler.canceled, h.uninstalls.launch)
		}
		h.canceler.unfinished[failed.WorkflowID] = false
		h.reconcile(t, platform.ID)
		if launch := h.uninstalls.launch; launch == nil || launch.RetryOfOperationID != failed.WorkflowID {
			t.Errorf("uninstall launch = %+v, want a retry of %s", launch, failed.WorkflowID)
		}
	})
	t.Run("a running uninstall is left as it is", func(t *testing.T) {
		h := newSpecHarness(nil)
		platform := h.installed(t)
		if _, err := h.specs.Delete(ctx, platform.ID, nil, "admin"); err != nil {
			t.Fatalf("Delete() error = %v", err)
		}
		h.reconcile(t, platform.ID)
		h.uninstallReports(t, platform.ID, "running", platformdomain.PlatformLifecycleUninstalling)
		generation := platform.Generation
		result, err := h.specs.Delete(ctx, platform.ID, &platformdomain.PlatformRemoval{ReleaseServers: true}, "admin")
		if err != nil {
			t.Fatalf("Delete(again) error = %v", err)
		}
		if result.Platform == nil || result.Plan != nil {
			t.Fatalf("Delete(again) = %+v, want the Platform without a plan", result)
		}
		if platform.Generation != generation || platform.Spec.Removal != nil {
			t.Errorf("generation %d removal %+v, want generation %d unchanged", platform.Generation, platform.Spec.Removal, generation)
		}
	})
	t.Run("an uninstalled Platform is deleted at once", func(t *testing.T) {
		h := newSpecHarness(nil)
		platform := &platformdomain.Platform{
			ID: "legacy", SiteID: "site-1", Name: "legacy", Type: platformdomain.PlatformTypeKubernetes, Generation: 1,
			Spec: &platformdomain.PlatformSpec{Desired: platformdomain.PlatformAbsent, Members: labSpec().Members},
		}
		h.platforms.platforms[platform.ID] = platform
		at := time.Now().UTC()
		h.lifecycle.snapshots[platform.ID] = platformdomain.LifecycleSnapshot{
			Origin: platformdomain.PlatformOriginDeployed, State: platformdomain.PlatformLifecycleUninstalled,
			Deployment: &platformdomain.LifecycleOperation{ID: "deploy-1", Status: operationSucceeded, RequestedAt: at},
			Uninstall:  &platformdomain.LifecycleOperation{ID: "uninstall-1", Status: operationSucceeded, RequestedAt: at.Add(time.Minute)},
		}
		result, err := h.specs.Delete(ctx, platform.ID, nil, "admin")
		if err != nil {
			t.Fatalf("Delete() error = %v", err)
		}
		if _, ok := h.platforms.platforms[platform.ID]; ok || result.Platform != nil || result.Plan != nil {
			t.Errorf("Delete() = %+v, want the record deleted without a plan", result)
		}
	})
}

// A record deleted while the controller did not see it go (the worker was down, or its change stream
// was reset) still has its waiting plan withdrawn at the next resync, since Keys names the Platform of
// every unfinished plan.
func TestResyncWithdrawsThePlanOfADeletedRecord(t *testing.T) {
	ctx := context.Background()
	h := newSpecHarness(nil)
	created, err := h.specs.Create(ctx, CreatePlatformSpecInput{
		SiteID: "site-1", Name: "draft", Type: platformdomain.PlatformTypeKubernetes, Spec: labSpec(),
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	id := created.Platform.ID
	if err := h.platforms.Delete(ctx, id); err != nil {
		t.Fatalf("delete the record: %v", err)
	}
	if err := h.servers.ReleasePlatformClaims(ctx, id); err != nil {
		t.Fatalf("release the claims: %v", err)
	}

	keys, err := h.controller.Keys(ctx)
	if err != nil || !slices.Contains(keys, id) {
		t.Fatalf("Keys() = %v, %v; want the deleted Platform that still has a plan", keys, err)
	}
	h.reconcile(t, id)
	if plan, _ := h.plans.Get(ctx, created.Plan.ID); plan.State != convergence.PlanSuperseded {
		t.Errorf("plan state = %s, want superseded once the controller saw the record gone", plan.State)
	}
}

// An uninstall that releases its members says, before approval, how many are released, that their
// operating system goes, what happens to their disks and static IP bindings, and that the record is
// deleted; an uninstall that keeps its members names no release.
func TestUninstallPlanEffectsDescribeTheRelease(t *testing.T) {
	members := func(ids ...string) []platformdomain.PlatformMember {
		out := make([]platformdomain.PlatformMember, len(ids))
		for i, id := range ids {
			out[i] = platformdomain.PlatformMember{ServerID: id, Roles: []string{"worker"}}
		}
		return out
	}
	tests := []struct {
		name    string
		members []platformdomain.PlatformMember
		removal *platformdomain.PlatformRemoval
		want    []string
	}{
		{
			name:    "kept members name no release",
			members: members("s1", "s2"),
			removal: &platformdomain.PlatformRemoval{},
			want:    nil,
		},
		{
			name:    "a released member keeps its disks and static IP bindings by default",
			members: members("s1"),
			removal: &platformdomain.PlatformRemoval{ReleaseServers: true},
			want: []string{"1 member is released to the provisioner, which removes its operating system; " +
				"its disks are not erased and its static IP bindings are kept. The Platform record is deleted once the release succeeds."},
		},
		{
			name:    "released members with erase and unbind",
			members: members("s1", "s2", "s3"),
			removal: &platformdomain.PlatformRemoval{ReleaseServers: true, ReleaseOptions: platformdomain.ServerReleaseOptions{
				Erase: true, QuickErase: true, UnbindStaticIPs: true,
			}},
			want: []string{"3 members are released to the provisioner, which removes their operating systems; " +
				"their disks are erased and their static IP bindings are removed. The Platform record is deleted once the release succeeds."},
		},
		{
			name:    "a secure erase alone erases",
			members: members("s1", "s2"),
			removal: &platformdomain.PlatformRemoval{ReleaseServers: true, ReleaseOptions: platformdomain.ServerReleaseOptions{SecureErase: true}},
			want: []string{"2 members are released to the provisioner, which removes their operating systems; " +
				"their disks are erased and their static IP bindings are kept. The Platform record is deleted once the release succeeds."},
		},
		{
			name:    "canceling Workflows follows the release",
			members: members("s1", "s2"),
			removal: &platformdomain.PlatformRemoval{ReleaseServers: true, CancelActiveWorkflows: true},
			want: []string{
				"2 members are released to the provisioner, which removes their operating systems; " +
					"their disks are not erased and their static IP bindings are kept. The Platform record is deleted once the release succeeds.",
				"unfinished Workflows holding the members are canceled first",
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			spec := platformdomain.PlatformSpec{Desired: platformdomain.PlatformAbsent, Members: tc.members, Removal: tc.removal}
			got := platformDraft("p1", 2, time.Now(), spec, platformdomain.PlatformChanges{}, nil).Effects
			if !slices.Equal(got, tc.want) {
				t.Errorf("platformDraft(removal %+v, %d members).Effects = %q, want %q", *tc.removal, len(tc.members), got, tc.want)
			}
		})
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

// An uninstalled Platform whose record remains has realized its absent spec, so it reads converged
// instead of waiting for a Change Plan nothing needs.
func TestAnUninstalledPlatformIsConverged(t *testing.T) {
	record := &platformdomain.DeploymentRecord{Request: map[string]any{
		"k0sVersion":      "v1.36.3+k0s.2",
		"roleAssignments": []any{map[string]any{"serverId": "c1", "role": "control-plane"}},
	}}
	h := newSpecHarness(record)
	platform := &platformdomain.Platform{ID: "legacy", SiteID: "site-1", Name: "legacy", Type: platformdomain.PlatformTypeKubernetes}
	h.platforms.platforms[platform.ID] = platform
	at := time.Now().UTC()
	h.lifecycle.snapshots[platform.ID] = platformdomain.LifecycleSnapshot{
		Origin: platformdomain.PlatformOriginDeployed, State: platformdomain.PlatformLifecycleUninstalled,
		Deployment: &platformdomain.LifecycleOperation{ID: "deploy-1", Status: operationSucceeded, RequestedAt: at},
		Uninstall:  &platformdomain.LifecycleOperation{ID: "uninstall-1", Status: operationSucceeded, RequestedAt: at.Add(time.Minute)},
	}
	h.reconcile(t, platform.ID)
	h.reconcile(t, platform.ID)
	if platform.Spec == nil || platform.Spec.Desired != platformdomain.PlatformAbsent {
		t.Fatalf("adopted spec = %+v, want absent", platform.Spec)
	}
	if realized, _ := convergence.FindCondition(platform.Status.Conditions, convergence.ConditionRealized); realized.Reason != convergence.ReasonConverged {
		t.Errorf("realized = %q %q, want converged", realized.Reason, realized.Message)
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
	if _, err := h.specs.UpdateSpec(context.Background(), platform.ID, 1, spec, ""); err != nil {
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

// A newer spec supersedes a failed realization; one that still waits for the user holds its
// targets, so the controller cancels it and starts the newer generation only once it ended.
func TestControllerCancelsASupersededRealizationThatStillWaits(t *testing.T) {
	h := newSpecHarness(nil)
	platform := h.installed(t)
	grown := platform.Spec.Clone()
	grown.Members = append(grown.Members, platformdomain.PlatformMember{ServerID: "w2", Roles: []string{"worker"}})
	if _, err := h.specs.UpdateSpec(context.Background(), platform.ID, 1, grown, ""); err != nil {
		t.Fatalf("UpdateSpec(add w2) error = %v", err)
	}
	h.reconcile(t, platform.ID)
	stuck := platform.Status.Realizing
	h.lifecycle.snapshots[platform.ID] = platformdomain.LifecycleSnapshot{
		Origin: platformdomain.PlatformOriginDeployed, State: platformdomain.PlatformLifecycleDeployFailed,
		Deployment: &platformdomain.LifecycleOperation{ID: stuck.WorkflowID, Status: operationFailed, RequestedAt: stuck.StartedAt},
	}
	h.canceler.unfinished[stuck.WorkflowID] = true
	launches := h.launcher.launches

	newer := grown.Clone()
	newer.Members = append(newer.Members, platformdomain.PlatformMember{ServerID: "w3", Roles: []string{"worker"}})
	if _, err := h.specs.UpdateSpec(context.Background(), platform.ID, 2, newer, ""); err != nil {
		t.Fatalf("UpdateSpec(add w3) error = %v", err)
	}
	h.reconcile(t, platform.ID)
	if len(h.canceler.canceled) != 1 || h.canceler.canceled[0] != stuck.WorkflowID {
		t.Fatalf("canceled = %v, want the superseded %s", h.canceler.canceled, stuck.WorkflowID)
	}
	if h.launcher.launches != launches {
		t.Fatalf("launches = %d, want none while the superseded Workflow holds the targets", h.launcher.launches)
	}

	h.canceler.unfinished[stuck.WorkflowID] = false
	h.reconcile(t, platform.ID)
	if h.launcher.launches != launches+1 {
		t.Errorf("launches = %d, want the newer generation started once the old Workflow ended", h.launcher.launches)
	}
	if platform.Status.Realizing == nil || platform.Status.Realizing.Generation != 3 {
		t.Errorf("realizing = %+v, want generation 3", platform.Status.Realizing)
	}
}

// A member whose join failed can be taken back out: the failed generation was never applied, so the
// spec written back converges at once, the failed Workflow stops counting, and the member's claim is
// released.
func TestRevertingAFailedMemberConvergesAndReleasesIt(t *testing.T) {
	h := newSpecHarness(nil)
	platform := h.installed(t)
	original := platform.Spec.Clone()
	grown := original.Clone()
	grown.Members = append(grown.Members, platformdomain.PlatformMember{ServerID: "w2", Roles: []string{"worker"}})
	if _, err := h.specs.UpdateSpec(context.Background(), platform.ID, 1, grown, ""); err != nil {
		t.Fatalf("UpdateSpec(add) error = %v", err)
	}
	h.reconcile(t, platform.ID)
	realizing := platform.Status.Realizing
	h.lifecycle.snapshots[platform.ID] = platformdomain.LifecycleSnapshot{
		Origin: platformdomain.PlatformOriginDeployed, State: platformdomain.PlatformLifecycleDeployFailed,
		Deployment: &platformdomain.LifecycleOperation{ID: realizing.WorkflowID, Status: operationFailed, RequestedAt: realizing.StartedAt},
	}
	h.reconcile(t, platform.ID)
	if heldBy(h.servers, "w2") != platform.ID {
		t.Fatalf("w2 lost its claim while the failed Workflow still names it")
	}

	result, err := h.specs.UpdateSpec(context.Background(), platform.ID, 2, original, "")
	if err != nil || result.Plan != nil {
		t.Fatalf("UpdateSpec(revert) = %+v, %v; want a write without a plan", result, err)
	}
	h.reconcile(t, platform.ID)
	if platform.Status.Realizing != nil || platform.Status.ObservedGeneration != 3 {
		t.Errorf("status = %+v, want generation 3 applied and nothing realizing", platform.Status)
	}
	if realized, _ := convergence.FindCondition(platform.Status.Conditions, convergence.ConditionRealized); realized.Reason != convergence.ReasonConverged {
		t.Errorf("realized = %+v, want converged", realized)
	}
	if got := heldBy(h.servers, "w2"); got != "" {
		t.Errorf("w2 claim = %q, want it released", got)
	}
	if h.launcher.launches != 2 {
		t.Errorf("launches = %d, want no Workflow for the revert", h.launcher.launches)
	}
}

// An approved Change Plan wakes the controller through its convergence request, which maps to every
// Platform; another controller's request maps to none.
func TestPlanApprovalWakesThePlatformController(t *testing.T) {
	h := newSpecHarness(nil)
	created, err := h.specs.Create(context.Background(), CreatePlatformSpecInput{
		SiteID: "site-1", Name: "lab-k0s", Type: platformdomain.PlatformTypeKubernetes, Spec: labSpec(),
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	keys, err := h.controller.KeysForChange(context.Background(), store.Change{Collection: convergence.RequestCollection, Key: PlatformControllerName})
	if err != nil || !slices.Contains(keys, created.Platform.ID) {
		t.Errorf("keys = %v, %v; want the created Platform", keys, err)
	}
	if keys, _ := h.controller.KeysForChange(context.Background(), store.Change{Collection: convergence.RequestCollection, Key: "ssh-keys"}); len(keys) != 0 {
		t.Errorf("another controller's request mapped to %v", keys)
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
