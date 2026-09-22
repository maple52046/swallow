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
