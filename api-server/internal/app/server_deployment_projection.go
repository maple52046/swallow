package app

import (
	"context"
	"time"

	serverdomain "github.com/maple52046/swallow/internal/core/server/domain"
	workflowdomain "github.com/maple52046/swallow/internal/core/workflow/domain"
)

// serverDeploymentStepObserver keeps the Server read model aligned with durable Step
// transitions, including cancellation before a provider activity starts. The server-deployment
// and server-deployment-cleared completions use it for Workflows that declare them.
type serverDeploymentStepObserver struct {
	servers serverdomain.ServerRepository
}

// projectDeployment writes a provision-os Task's transition onto its Server's Deployment result:
// pending and running are deploying, and each outcome is the matching result. It is the
// server-deployment completion's projection.
func (o serverDeploymentStepObserver) projectDeployment(ctx context.Context, operationID string, step workflowdomain.Task) error {
	serverID := firstTargetServer(step)
	if serverID == "" {
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
	// operation and step, and to an attempt no newer than the recorded outcome: a jobbed parent
	// cancels from its start snapshot (attempt 1) after a Job retry finished a later attempt, so
	// requiring the exact attempt let that stale cancel through. A newer attempt or a different
	// operation still applies, and a genuinely in-flight (deploying/verifying) or
	// attention-pending deploy can still be canceled.
	if step.Status == workflowdomain.TaskCanceled || step.Status == workflowdomain.TaskSkipped {
		if current := server.Deployment; current != nil &&
			current.OperationID == operationID && current.StepID == step.ID &&
			step.Attempt <= current.Attempt &&
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

func deploymentStateFromStep(status workflowdomain.TaskStatus) (serverdomain.DeploymentState, bool, bool) {
	switch status {
	case workflowdomain.TaskPending, workflowdomain.TaskRunning,
		workflowdomain.TaskWaitingExternal, workflowdomain.TaskWaitingDependency:
		return serverdomain.DeploymentDeploying, false, true
	case workflowdomain.TaskSucceeded:
		return serverdomain.DeploymentSucceeded, true, true
	case workflowdomain.TaskFailed:
		return serverdomain.DeploymentFailed, true, true
	case workflowdomain.TaskRequiresAttention:
		return serverdomain.DeploymentRequiresAttention, true, true
	case workflowdomain.TaskCanceled, workflowdomain.TaskSkipped:
		return serverdomain.DeploymentCanceled, true, true
	default:
		return "", false, false
	}
}
