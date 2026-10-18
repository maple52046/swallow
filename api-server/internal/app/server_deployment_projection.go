package app

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	operationdomain "github.com/maple52046/swallow/internal/operation/domain"
	serverdomain "github.com/maple52046/swallow/internal/server/domain"
)

const legacyDeploymentProjectionWorkers = 8

// serverDeploymentStepObserver keeps the Server read model aligned with durable Step
// transitions, including cancellation before a provider activity starts.
type serverDeploymentStepObserver struct {
	servers serverdomain.ServerRepository
}

func (o serverDeploymentStepObserver) ObserveStep(ctx context.Context, operationID string, step operationdomain.Task) error {
	serverID := firstTargetServer(step)
	if serverID == "" {
		return nil
	}
	if step.Kind == "release-os" {
		if step.Status == operationdomain.TaskSucceeded {
			return o.servers.SetDeployment(ctx, serverID, nil)
		}
		return nil
	}
	if step.Kind != "provision-os" {
		return nil
	}

	state, terminal, ok := deploymentStateFromStep(step.Status)
	if !ok {
		return nil
	}
	now := time.Now().UTC()
	startedAt := now
	server, err := o.servers.FindByID(ctx, serverID)
	if err != nil {
		return err
	}
	// A completed OS deployment must not be un-done by cancelling the enclosing Workflow. The
	// deployment axis is this Server's own OS deployment (provision-os) outcome, separate from
	// the platform: a platform deploy that is canceled after this Server's provision-os already
	// reached a terminal outcome (for example the jobbed parent finalizing cancellation from a
	// stale step snapshot) would otherwise regress deployment succeeded/failed to canceled and
	// hide the real OS result. The cancellation is carried by the Workflow status and the
	// Platform lifecycle, not by this per-Server axis. The guard is scoped to the same
	// operation/step/attempt, so a fresh attempt or a different operation still applies, and a
	// genuinely in-flight (deploying/verifying) or attention-pending deploy can still be canceled.
	if step.Status == operationdomain.TaskCanceled || step.Status == operationdomain.TaskSkipped {
		if current := server.Deployment; current != nil &&
			current.OperationID == operationID && current.StepID == step.ID &&
			current.Attempt == step.Attempt &&
			(current.State == serverdomain.DeploymentSucceeded || current.State == serverdomain.DeploymentFailed) {
			return nil
		}
	}
	if step.StartedAt != nil && !step.StartedAt.IsZero() {
		startedAt = *step.StartedAt
	} else if current := server.Deployment; current != nil &&
		current.OperationID == operationID && current.StepID == step.ID &&
		current.Attempt == step.Attempt {
		startedAt = current.StartedAt
	}
	// Carry the stable error code onto the axis alongside stage and message: it is the
	// machine-readable reason a client keys guidance off (for example
	// deployment_ssh_unreachable), and dropping it left the deployment failure with only a
	// human message and no code to branch on.
	code, stage, reason := "", "", ""
	if step.Error != nil {
		code = step.Error.Code
		stage = step.Error.Stage
		reason = step.Error.Message
	}
	var finishedAt *time.Time
	if terminal {
		finishedAt = step.FinishedAt
		if finishedAt == nil {
			finishedAt = &now
		}
	}
	return o.servers.SetDeployment(ctx, serverID, &serverdomain.DeploymentStatus{
		State: state, OperationID: operationID, StepID: step.ID, Attempt: step.Attempt,
		Code: code, Stage: stage, StatusReason: reason, StartedAt: startedAt,
		FinishedAt: finishedAt, UpdatedAt: now,
	})
}

func deploymentStateFromStep(status operationdomain.TaskStatus) (serverdomain.DeploymentState, bool, bool) {
	switch status {
	case operationdomain.TaskPending, operationdomain.TaskRunning,
		operationdomain.TaskWaitingExternal, operationdomain.TaskWaitingDependency:
		return serverdomain.DeploymentDeploying, false, true
	case operationdomain.TaskSucceeded:
		return serverdomain.DeploymentSucceeded, true, true
	case operationdomain.TaskFailed:
		return serverdomain.DeploymentFailed, true, true
	case operationdomain.TaskRequiresAttention:
		return serverdomain.DeploymentRequiresAttention, true, true
	case operationdomain.TaskCanceled, operationdomain.TaskSkipped:
		return serverdomain.DeploymentCanceled, true, true
	default:
		return "", false, false
	}
}

// repairStuckDeploymentProjections settles Server Deployment results that still report
// in-flight work (deploying or verifying) although the Workflow they name has already ended.
//
// The Deployment result is written only from durable provision-os Step transitions, so a
// projection the workflow failed to deliver leaves it in-flight forever: provisioning reconcile
// deliberately never clears an active deployment, and no later Step of that Workflow will run.
// This sweep is the safety net for that gap. It only acts when the named Workflow is terminal,
// which is what keeps it from racing an in-flight Operation, and it never touches the
// provider-owned provisioning axis.
//
// Each candidate is settled from the durable Task: a Task that already recorded an outcome is
// projected as-is, and a Task left non-terminal by a canceled Workflow is projected as canceled.
// A Workflow that ended any other way without a Task outcome says nothing about this Server's OS
// result, so that Server is left for the operator instead of being guessed. A missing Workflow or
// Task is skipped for the same reason. Per-Server failures are joined and returned so one bad
// record never stops the rest of the sweep; callers treat the error as a log-and-retry signal.
func repairStuckDeploymentProjections(
	ctx context.Context,
	servers serverdomain.ServerRepository,
	operations operationdomain.WorkflowRepository,
) error {
	if servers == nil || operations == nil {
		return nil
	}
	// Limit 0 lists every Server: the in-flight filter below is applied in memory because the
	// repository has no deployment-state filter, and a reconcile pass already lists the fleet.
	result, err := servers.List(ctx, serverdomain.ListFilter{IncludeAbsent: true})
	if err != nil {
		return fmt.Errorf("list servers for deployment repair: %w", err)
	}
	observer := serverDeploymentStepObserver{servers: servers}
	var joined error
	for _, server := range result.Servers {
		if server.Deployment == nil || !deploymentInFlight(server.Deployment.State) {
			continue
		}
		if err := repairStuckDeployment(ctx, servers, operations, observer, server.ID, *server.Deployment); err != nil {
			joined = errors.Join(joined, fmt.Errorf("repair deployment of server %s: %w", server.ID, err))
		}
	}
	return joined
}

// repairStuckDeployment settles one Server's in-flight Deployment result when its Workflow is
// terminal. The Server is re-read right before the write and skipped unless it still carries
// the same in-flight Operation/Step/attempt, so a deploy accepted after the list was taken is
// not overwritten. The remaining read-then-write window is accepted: a newer Operation's own
// Step projections rewrite the result on its next transition.
func repairStuckDeployment(
	ctx context.Context,
	servers serverdomain.ServerRepository,
	operations operationdomain.WorkflowRepository,
	observer serverDeploymentStepObserver,
	serverID string,
	deployment serverdomain.DeploymentStatus,
) error {
	operation, err := operations.FindByID(ctx, deployment.OperationID)
	if errors.Is(err, operationdomain.ErrWorkflowNotV3) || errors.Is(err, operationdomain.ErrWorkflowNotFound) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("find workflow %s: %w", deployment.OperationID, err)
	}
	if !operation.Status.Terminal() {
		return nil
	}
	var task *operationdomain.Task
	for index := range operation.Steps {
		if operation.Steps[index].ID == deployment.StepID {
			task = &operation.Steps[index]
			break
		}
	}
	if task == nil {
		return nil
	}
	step, ok := settledDeploymentStep(operation, *task, deployment)
	if !ok {
		return nil
	}
	current, err := servers.FindByID(ctx, serverID)
	if errors.Is(err, serverdomain.ErrServerNotFound) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("re-read server: %w", err)
	}
	if !sameInFlightDeployment(current.Deployment, deployment) {
		return nil
	}
	return observer.ObserveStep(ctx, operation.ID, step)
}

// settledDeploymentStep decides the Step outcome to project for an in-flight Deployment whose
// Workflow is terminal, and reports false when no outcome can be stated honestly.
//
// A Task outcome recorded for the in-flight attempt or a later one wins. Otherwise only a
// canceled Workflow implies an outcome: its Task can no longer advance, so it is projected as
// canceled under the Server's own attempt. Clearing StartedAt when the attempt is replaced makes
// the observer keep the Server's recorded start time instead of another attempt's.
func settledDeploymentStep(
	operation *operationdomain.Workflow,
	task operationdomain.Task,
	deployment serverdomain.DeploymentStatus,
) (operationdomain.Task, bool) {
	if _, terminal, ok := deploymentStateFromStep(task.Status); ok && terminal && task.Attempt >= deployment.Attempt {
		return task, true
	}
	if operation.Status != operationdomain.WorkflowCanceled {
		return operationdomain.Task{}, false
	}
	task.Status = operationdomain.TaskCanceled
	task.Error = nil
	if task.Attempt != deployment.Attempt {
		task.Attempt = deployment.Attempt
		task.StartedAt = nil
	}
	if task.FinishedAt == nil {
		task.FinishedAt = operation.FinishedAt
	}
	return task, true
}

// deploymentInFlight reports whether a Deployment result still claims work is running. Only these
// states can be stranded by a lost projection; every other state is already an outcome.
func deploymentInFlight(state serverdomain.DeploymentState) bool {
	return state == serverdomain.DeploymentDeploying || state == serverdomain.DeploymentVerifying
}

// sameInFlightDeployment reports whether current is still the in-flight Deployment the repair
// sweep decided on, matching by Operation, Step, and attempt.
func sameInFlightDeployment(current *serverdomain.DeploymentStatus, observed serverdomain.DeploymentStatus) bool {
	return current != nil && deploymentInFlight(current.State) &&
		current.OperationID == observed.OperationID &&
		current.StepID == observed.StepID &&
		current.Attempt == observed.Attempt
}

type legacyDeploymentProjectionCandidate struct {
	operation *operationdomain.Workflow
	provision operationdomain.Task
	wait      operationdomain.Task
}

// reconcileLegacyDeploymentProjections repairs the Server read model for Platform
// Operations created before readiness moved into each provision-os Step.
//
// It reads the aggregate wait-for-ssh Step only as a workflow-shape marker. Failed
// targets are determined by fresh Server observations and bounded TCP probes, never by
// parsing historical error text. The provider is not mutated.
func reconcileLegacyDeploymentProjections(
	ctx context.Context,
	operations operationdomain.WorkflowRepository,
	executor providerStepExecutor,
) error {
	if operations == nil || executor.servers == nil {
		return nil
	}
	candidates := []legacyDeploymentProjectionCandidate{}
	claimed := map[string]bool{}
	for offset := 0; ; offset += 100 {
		items, total, err := operations.List(ctx, operationdomain.WorkflowFilter{
			Kind: operationdomain.WorkflowKindDeployKubernetes, Offset: offset, Limit: 100,
		})
		if err != nil {
			return fmt.Errorf("list legacy deployment operations: %w", err)
		}
		for _, operation := range items {
			wait, ok := legacyWaitForSSHStep(operation.Steps)
			if !ok {
				continue
			}
			for _, provision := range operation.Steps {
				serverID := firstTargetServer(provision)
				if provision.Kind != "provision-os" || serverID == "" || claimed[serverID] {
					continue
				}
				claimed[serverID] = true
				server, findErr := executor.servers.FindByID(ctx, serverID)
				if findErr != nil {
					return fmt.Errorf("find legacy deployment target %s: %w", serverID, findErr)
				}
				if server.Deployment == nil {
					candidates = append(candidates, legacyDeploymentProjectionCandidate{
						operation: operation, provision: provision, wait: wait,
					})
				}
			}
		}
		if offset+len(items) >= total || len(items) == 0 {
			break
		}
	}
	if len(candidates) == 0 {
		return nil
	}

	jobs := make(chan legacyDeploymentProjectionCandidate)
	errorsByTarget := make(chan error, len(candidates))
	workers := legacyDeploymentProjectionWorkers
	if len(candidates) < workers {
		workers = len(candidates)
	}
	var group sync.WaitGroup
	group.Add(workers)
	for range workers {
		go func() {
			defer group.Done()
			for candidate := range jobs {
				if err := executor.projectLegacyDeployment(ctx, candidate); err != nil {
					errorsByTarget <- err
				}
			}
		}()
	}
	for _, candidate := range candidates {
		select {
		case jobs <- candidate:
		case <-ctx.Done():
			close(jobs)
			group.Wait()
			return ctx.Err()
		}
	}
	close(jobs)
	group.Wait()
	close(errorsByTarget)
	var joined error
	for err := range errorsByTarget {
		joined = errors.Join(joined, err)
	}
	return joined
}

func legacyWaitForSSHStep(steps []operationdomain.Task) (operationdomain.Task, bool) {
	for _, step := range steps {
		if step.Kind == "wait-for-ssh" {
			return step, true
		}
	}
	return operationdomain.Task{}, false
}

func (e providerStepExecutor) projectLegacyDeployment(ctx context.Context, candidate legacyDeploymentProjectionCandidate) error {
	serverID := firstTargetServer(candidate.provision)
	server, err := e.servers.FindByID(ctx, serverID)
	if err != nil {
		return fmt.Errorf("find legacy deployment target %s: %w", serverID, err)
	}
	// A new Operation projection always wins over this one-time compatibility repair.
	if server.Deployment != nil {
		return nil
	}
	if candidate.provision.Status != operationdomain.TaskSucceeded {
		return (serverDeploymentStepObserver{servers: e.servers}).ObserveStep(
			ctx, candidate.operation.ID, candidate.provision,
		)
	}

	state := serverdomain.DeploymentVerifying
	stage, reason := "", ""
	finishedAt := candidate.wait.FinishedAt
	switch candidate.wait.Status {
	case operationdomain.TaskSucceeded:
		state = serverdomain.DeploymentSucceeded
	case operationdomain.TaskFailed:
		readiness, inspectErr := e.inspectDeploymentReadiness(ctx, serverID, candidate.operation.SiteID)
		if inspectErr != nil {
			state = serverdomain.DeploymentRequiresAttention
			stage = "ssh_readiness"
			reason = inspectErr.Error()
			break
		}
		if readiness.reachable {
			state = serverdomain.DeploymentSucceeded
			break
		}
		failure := deploymentReadinessFailed(readiness)
		state = serverdomain.DeploymentFailed
		stage = failure.Error.Stage
		reason = failure.Error.Message
	case operationdomain.TaskRequiresAttention:
		state = serverdomain.DeploymentRequiresAttention
		if candidate.wait.Error != nil {
			stage = candidate.wait.Error.Stage
			reason = candidate.wait.Error.Message
		}
	case operationdomain.TaskCanceled, operationdomain.TaskSkipped:
		state = serverdomain.DeploymentCanceled
	}

	now := time.Now().UTC()
	startedAt := candidate.operation.RequestedAt
	if candidate.provision.StartedAt != nil {
		startedAt = *candidate.provision.StartedAt
	}
	return e.servers.SetDeployment(ctx, serverID, &serverdomain.DeploymentStatus{
		State: state, OperationID: candidate.operation.ID,
		StepID: candidate.provision.ID, Attempt: candidate.provision.Attempt,
		Stage: stage, StatusReason: reason, StartedAt: startedAt,
		FinishedAt: finishedAt, UpdatedAt: now,
	})
}
