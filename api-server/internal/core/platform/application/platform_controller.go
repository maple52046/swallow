package application

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"strings"
	"time"

	"github.com/maple52046/swallow/internal/core/convergence"
	platformdomain "github.com/maple52046/swallow/internal/core/platform/domain"
	serverdomain "github.com/maple52046/swallow/internal/core/server/domain"
	"github.com/maple52046/swallow/internal/core/store"
)

// ReasonPlanRequired is the Platform reason for a destructive change that has no Change Plan waiting
// any more (it was rejected or expired); writing the spec again proposes a new one.
const ReasonPlanRequired = "plan_required"

// controllerRequester is who the Workflows the platform controller starts are requested by.
const controllerRequester = "platform-controller"

// The lifecycle operation statuses the controller tells apart.
const (
	operationSucceeded = "succeeded"
	operationFailed    = "failed"
	operationCanceled  = "canceled"
)

// WorkflowPlatformReader names the Platform a Workflow belongs to ("" for none or an unknown
// Workflow), so a Workflow's progress wakes that Platform.
type WorkflowPlatformReader interface {
	PlatformOfWorkflow(ctx context.Context, workflowID string) (string, error)
}

// PlatformController converges Platforms to their specs (desired-state-model §5.2). Reconciling one
// Platform it:
//   - adopts it from its deployment when it has no spec (AdoptedSpec), at generation 1;
//   - claims the members of a present spec;
//   - records the realizing Workflow's spec as applied once that Workflow, or a rerun of it,
//     succeeded;
//   - unless a Workflow of the Platform is running, carries out its approved Change Plan, or starts
//     the Workflow of a non-destructive change for a generation it has not started yet;
//   - judges realized, and drifted from the members the platform reports.
//
// A Platform whose record is gone has its claims released. A failed Workflow is not started again
// for the same generation: an operator reruns it or writes a new spec.
type PlatformController struct {
	platforms platformdomain.PlatformRepository
	specs     platformdomain.PlatformSpecStore
	claims    serverdomain.PlatformClaimStore
	servers   serverdomain.ServerRepository
	lifecycle platformdomain.LifecycleReader
	records   platformdomain.DeploymentRecordReader
	workflows WorkflowPlatformReader
	plans     *convergence.ChangePlanService
	planner   *PlatformPlanner
	modules   map[platformdomain.PlatformType]platformdomain.PlatformModule
	now       func() time.Time
}

var _ convergence.Controller = (*PlatformController)(nil)

// NewPlatformController wires the controller; the planner carries out the Workflows.
func NewPlatformController(
	platforms platformdomain.PlatformRepository,
	specs platformdomain.PlatformSpecStore,
	claims serverdomain.PlatformClaimStore,
	servers serverdomain.ServerRepository,
	lifecycle platformdomain.LifecycleReader,
	records platformdomain.DeploymentRecordReader,
	workflows WorkflowPlatformReader,
	plans *convergence.ChangePlanService,
	planner *PlatformPlanner,
	modules map[platformdomain.PlatformType]platformdomain.PlatformModule,
) *PlatformController {
	return &PlatformController{
		platforms: platforms, specs: specs, claims: claims, servers: servers, lifecycle: lifecycle,
		records: records, workflows: workflows, plans: plans, planner: planner, modules: modules,
		now: func() time.Time { return time.Now().UTC() },
	}
}

// Name is "platform".
func (c *PlatformController) Name() string { return PlatformControllerName }

// Collections are the Platforms, whose membership sync also writes them, and the Workflows that
// realize them.
func (c *PlatformController) Collections() []string { return []string{"platforms", "workflows"} }

// KeysForChange maps a Platform change to its id and a Workflow change to its Platform.
func (c *PlatformController) KeysForChange(ctx context.Context, change store.Change) ([]string, error) {
	switch change.Collection {
	case "platforms":
		return []string{change.Key}, nil
	case "workflows":
		platformID, err := c.workflows.PlatformOfWorkflow(ctx, change.Key)
		if err != nil || platformID == "" {
			return nil, err
		}
		return []string{platformID}, nil
	}
	return nil, nil
}

// Keys are every Platform, and every Platform id a claim still names.
func (c *PlatformController) Keys(ctx context.Context) ([]string, error) {
	platforms, err := c.platforms.List(ctx, "")
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	keys := make([]string, 0, len(platforms))
	for _, platform := range platforms {
		seen[platform.ID] = true
		keys = append(keys, platform.ID)
	}
	if c.claims != nil {
		claiming, err := c.claims.ClaimingPlatformIDs(ctx)
		if err != nil {
			return nil, err
		}
		for _, id := range claiming {
			if !seen[id] {
				keys = append(keys, id)
			}
		}
	}
	return keys, nil
}

// Reconcile converges one Platform.
func (c *PlatformController) Reconcile(ctx context.Context, request convergence.Request) error {
	platform, err := c.platforms.FindByID(ctx, request.Key)
	if errors.Is(err, platformdomain.ErrPlatformNotFound) {
		if c.claims == nil {
			return nil
		}
		return c.claims.ReleasePlatformClaims(ctx, request.Key)
	}
	if err != nil {
		return err
	}
	module, ok := c.modules[platform.Type]
	if !ok {
		return nil
	}
	snapshots, err := c.lifecycle.Read(ctx, []string{platform.ID})
	if err != nil {
		return err
	}
	snapshot := snapshots[platform.ID]
	if platform.Spec == nil {
		return c.adopt(ctx, module, platform, snapshot)
	}
	spec := *platform.Spec
	if spec.Desired == platformdomain.PlatformPresent && snapshot.State != platformdomain.PlatformLifecycleUninstalled {
		c.claimMembers(ctx, platform.ID, spec)
	}

	status := platform.Status
	if status.Realizing == nil && status.Applied == nil && spec.Desired == platformdomain.PlatformPresent && snapshot.Deployment != nil {
		// A deployment accepted outside the controller (the deploy route) realizes the first spec.
		status.Realizing = &platformdomain.PlatformRealizing{
			Generation: platform.Generation, WorkflowID: snapshot.Deployment.ID, Spec: spec.Clone(), StartedAt: snapshot.Deployment.RequestedAt,
		}
	}
	failed := false
	if realizing := status.Realizing; realizing != nil {
		if latest := latestOperation(snapshot); latest != nil && (latest.ID == realizing.WorkflowID || latest.RequestedAt.After(realizing.StartedAt)) {
			switch latest.Status {
			case operationSucceeded:
				applied := realizing.Spec.Clone()
				status.Applied, status.ObservedGeneration, status.Realizing = &applied, realizing.Generation, nil
			case operationFailed, operationCanceled:
				failed = true
			}
		}
	}

	held := snapshot.State == platformdomain.PlatformLifecycleDeploying || snapshot.State == platformdomain.PlatformLifecycleUninstalling
	var actErr error
	if !held && !(failed && status.Realizing.Generation == platform.Generation) {
		before := status.Realizing
		actErr = c.act(ctx, module, platform, &status)
		failed = failed && status.Realizing == before
	}
	c.judge(ctx, module, platform, snapshot, &status, failed, actErr)
	if !reflect.DeepEqual(status, platform.Status) {
		if err := c.specs.WriteStatus(ctx, platform.ID, status); err != nil && !errors.Is(err, platformdomain.ErrPlatformNotFound) {
			return err
		}
	}
	return actErr
}

// act carries out the next step toward the spec: the approved Change Plan, or the Workflow of a
// non-destructive change. A change that needs no Workflow is applied at once.
func (c *PlatformController) act(ctx context.Context, module platformdomain.PlatformModule, platform *platformdomain.Platform, status *platformdomain.PlatformStatus) error {
	started := c.now()
	workflowID, err := c.plans.ApplyApproved(ctx, platformRef(platform.ID))
	if err != nil {
		return err
	}
	if workflowID != "" {
		status.Realizing = &platformdomain.PlatformRealizing{Generation: platform.Generation, WorkflowID: workflowID, Spec: platform.Spec.Clone(), StartedAt: started}
		return nil
	}
	changes, err := module.Changes(status.Applied, *platform.Spec)
	if err != nil {
		return err
	}
	if changes.Empty() {
		if platform.Spec.Desired == platformdomain.PlatformPresent && status.Applied != nil && !reflect.DeepEqual(*status.Applied, *platform.Spec) {
			applied := platform.Spec.Clone()
			status.Applied, status.ObservedGeneration = &applied, platform.Generation
		}
		return nil
	}
	if changes.Destructive() || len(changes.Unsupported) > 0 {
		return nil
	}
	if status.Realizing != nil && status.Realizing.Generation >= platform.Generation {
		return nil
	}
	switch changes.Realization {
	case platformdomain.RealizeNothing:
		applied := platform.Spec.Clone()
		status.Applied, status.ObservedGeneration = &applied, platform.Generation
		return nil
	case platformdomain.RealizeDeploy:
		prepared, err := PrepareSpecDeployment(ctx, c.planner.deploy, module, platform, controllerRequester)
		if err != nil {
			return err
		}
		if workflowID, err = c.planner.deploy.Persist(ctx, prepared); err != nil {
			return err
		}
	case platformdomain.RealizeGPUOperator:
		input, err := gpuOperatorInput(c.planner.deploy, module, platform, controllerRequester)
		if err != nil {
			return err
		}
		if c.planner.gpuOperators == nil {
			return errors.New("GPU Operator changes are unavailable")
		}
		result, err := c.planner.gpuOperators.Configure(ctx, *input)
		if err != nil {
			return err
		}
		workflowID = result.WorkflowID
	default:
		return fmt.Errorf("platform controller cannot start a %q Workflow", changes.Realization)
	}
	status.Realizing = &platformdomain.PlatformRealizing{Generation: platform.Generation, WorkflowID: workflowID, Spec: platform.Spec.Clone(), StartedAt: started}
	return nil
}

// judge sets the realized and drifted conditions for the Platform's current generation.
func (c *PlatformController) judge(
	ctx context.Context,
	module platformdomain.PlatformModule,
	platform *platformdomain.Platform,
	snapshot platformdomain.LifecycleSnapshot,
	status *platformdomain.PlatformStatus,
	failed bool,
	actErr error,
) {
	now := c.now()
	realized := convergence.Condition{Type: convergence.ConditionRealized, ObservedGeneration: platform.Generation}
	switch realizing := status.Realizing; {
	case failed:
		realized.Status, realized.Reason = convergence.ConditionFalse, convergence.ReasonFailed
		realized.Message = fmt.Sprintf("Workflow %s did not succeed; rerun it or change the spec", realizing.WorkflowID)
	case actErr != nil:
		realized.Status, realized.Reason, realized.Message = convergence.ConditionFalse, convergence.ReasonFailed, actErr.Error()
	case realizing != nil:
		realized.Status, realized.Reason = convergence.ConditionFalse, convergence.ReasonConverging
		realized.Message = fmt.Sprintf("Workflow %s carries out generation %d", realizing.WorkflowID, realizing.Generation)
	default:
		realized.Status, realized.Reason = convergence.ConditionTrue, convergence.ReasonConverged
		if changes, err := module.Changes(status.Applied, *platform.Spec); err != nil || !changes.Empty() {
			realized.Status, realized.Reason = convergence.ConditionFalse, ReasonPlanRequired
			realized.Message = "the change needs an approved Change Plan; write the spec again to propose one"
			plans, listErr := c.plans.List(ctx, convergence.ChangePlanFilter{
				Resource: platformRef(platform.ID), States: []convergence.ChangePlanState{convergence.PlanPendingApproval, convergence.PlanApproved},
			})
			if listErr == nil && len(plans) > 0 {
				realized.Reason = convergence.ReasonAwaitingApproval
				realized.Message = fmt.Sprintf("Change Plan %s waits for approval", plans[0].ID)
			}
		}
	}
	status.Conditions, _ = convergence.SetCondition(status.Conditions, realized, now)

	drifted := convergence.Condition{
		Type: convergence.ConditionDrifted, Status: convergence.ConditionFalse, Reason: convergence.ReasonInSync, ObservedGeneration: platform.Generation,
	}
	if status.Applied != nil && status.Realizing == nil && snapshot.State == platformdomain.PlatformLifecycleActive {
		if fields := c.membershipDrift(ctx, module, platform.ID, *status.Applied); len(fields) > 0 {
			drifted.Status, drifted.Reason, drifted.Message = convergence.ConditionTrue, convergence.ReasonDriftDetected, strings.Join(fields, "; ")
		}
	}
	status.Conditions, _ = convergence.SetCondition(status.Conditions, drifted, now)
}

// membershipDrift compares the members the platform reports with the applied spec: a Server that
// reports membership the spec does not name, and a member the platform should report but does not.
func (c *PlatformController) membershipDrift(ctx context.Context, module platformdomain.PlatformModule, platformID string, applied platformdomain.PlatformSpec) []string {
	if c.servers == nil {
		return nil
	}
	reporting, err := c.servers.List(ctx, serverdomain.ListFilter{PlatformID: platformID, IncludeAbsent: true})
	if err != nil {
		slog.Warn("platform drift: list members", "platformId", platformID, "error", err)
		return nil
	}
	reports := map[string]bool{}
	var fields []string
	for _, server := range reporting.Servers {
		reports[server.ID] = true
		if _, ok := applied.Member(server.ID); !ok {
			fields = append(fields, server.DisplayName()+" reports membership but is not a member")
		}
	}
	for _, member := range applied.Members {
		if reports[member.ServerID] || !module.ReportsMembership(member) {
			continue
		}
		name := member.ServerID
		if server, err := c.servers.FindByID(ctx, member.ServerID); err == nil {
			name = server.DisplayName()
		}
		fields = append(fields, name+" is a member but does not report membership")
	}
	return fields
}

// adopt gives a Platform deployed before specs existed its spec and status, at generation 1.
func (c *PlatformController) adopt(ctx context.Context, module platformdomain.PlatformModule, platform *platformdomain.Platform, snapshot platformdomain.LifecycleSnapshot) error {
	var record *platformdomain.DeploymentRecord
	if snapshot.Origin == platformdomain.PlatformOriginDeployed && c.records != nil {
		found, ok, err := c.records.LatestDeploymentRecord(ctx, platform)
		if err != nil {
			return err
		}
		if ok {
			record = &found
		}
	}
	spec, applied, err := AdoptedSpec(module, platform, snapshot, record)
	if err != nil {
		slog.Warn("platform adoption left a platform without a spec", "platformId", platform.ID, "error", err)
		return nil
	}
	generation, err := c.specs.WriteSpec(ctx, platform.ID, 0, spec)
	if errors.Is(err, platformdomain.ErrGenerationConflict) || errors.Is(err, platformdomain.ErrPlatformNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	status := platformdomain.PlatformStatus{Applied: applied}
	if applied != nil {
		status.ObservedGeneration = generation
	} else if spec.Desired == platformdomain.PlatformPresent && snapshot.Deployment != nil {
		status.Realizing = &platformdomain.PlatformRealizing{
			Generation: generation, WorkflowID: snapshot.Deployment.ID, Spec: spec.Clone(), StartedAt: snapshot.Deployment.RequestedAt,
		}
	}
	slog.Info("platform adopted", "platformId", platform.ID, "members", len(spec.Members), "desired", spec.Desired)
	return c.specs.WriteStatus(ctx, platform.ID, status)
}

// claimMembers claims each member for the Platform one at a time, so a Server that is gone or
// claimed by another Platform does not stop the others.
func (c *PlatformController) claimMembers(ctx context.Context, platformID string, spec platformdomain.PlatformSpec) {
	if c.claims == nil {
		return
	}
	for _, serverID := range spec.MemberIDs() {
		err := c.claims.ClaimForPlatform(ctx, platformID, []string{serverID}, c.now())
		switch {
		case err == nil, errors.Is(err, serverdomain.ErrServerNotFound):
		case errors.Is(err, serverdomain.ErrServerClaimed):
			slog.Warn("platform member is claimed by another platform", "platformId", platformID, "serverId", serverID)
		default:
			slog.Error("claim platform member", "platformId", platformID, "serverId", serverID, "error", err)
		}
	}
}

// latestOperation is the Platform's most recently requested deploy or uninstall Workflow.
func latestOperation(snapshot platformdomain.LifecycleSnapshot) *platformdomain.LifecycleOperation {
	latest := snapshot.Deployment
	if uninstall := snapshot.Uninstall; uninstall != nil && (latest == nil || uninstall.RequestedAt.After(latest.RequestedAt)) {
		latest = uninstall
	}
	return latest
}
