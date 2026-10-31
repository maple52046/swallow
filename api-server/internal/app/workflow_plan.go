package app

import (
	"context"

	"github.com/maple52046/swallow/internal/adapters/executor/temporalworkflow"
	workflowdomain "github.com/maple52046/swallow/internal/core/workflow/domain"
)

// This file is the one place that registers what a Workflow plan may declare (decision 072): the
// Task kinds launchers create, with the Runner that executes each and how a rerun and a resume
// treat it, and the compensation types. Every process builds the same catalog, so the API accepts
// only plans the worker and the Ansible executor can run.

// compensationReturnCanceledDeployment returns a deployment a cancel interrupted to Ready
// (decision 060). Deploy launchers declare it on provision-os.
const compensationReturnCanceledDeployment = "return-canceled-deployment"

// workflowPlanCatalog returns the registered plan catalog.
func workflowPlanCatalog() workflowdomain.PlanCatalog {
	provisioner := func(rerun workflowdomain.RerunPolicy) workflowdomain.TaskKind {
		return workflowdomain.TaskKind{Runner: workflowdomain.RunnerKindProvisioner, Rerun: rerun, Resume: workflowdomain.ResumeObserve}
	}
	internal := func(rerun workflowdomain.RerunPolicy) workflowdomain.TaskKind {
		return workflowdomain.TaskKind{Runner: workflowdomain.RunnerKindInternal, Rerun: rerun, Resume: workflowdomain.ResumeObserve}
	}
	release := provisioner(workflowdomain.RerunConverge)
	release.ReleasesHosts = true
	return workflowdomain.PlanCatalog{
		TaskKinds: map[string]workflowdomain.TaskKind{
			// Ensure Tasks observe before acting on a later attempt, so a rerun runs them again.
			"provision-os":             provisioner(workflowdomain.RerunConverge),
			"release-os":               release,
			"recover-server":           provisioner(workflowdomain.RerunConverge),
			ensureBootMediaTaskKind:    internal(workflowdomain.RerunConverge),
			"wait-for-ssh":             internal(workflowdomain.RerunConverge),
			"validate-platform-health": internal(workflowdomain.RerunConverge),
			// One-time actions keep their outcome on a rerun.
			waitEnrollmentTaskKind:              provisioner(workflowdomain.RerunKeep),
			inspectTaskKind:                     provisioner(workflowdomain.RerunKeep),
			enrollVirtualMachineTaskKind:        provisioner(workflowdomain.RerunKeep),
			"complete-uninstall":                internal(workflowdomain.RerunKeep),
			"record-image-verification":         internal(workflowdomain.RerunKeep),
			"record-image-verification-failure": internal(workflowdomain.RerunKeep),
			softwareStepKindRecord:              internal(workflowdomain.RerunKeep),
			softwareStepKindClear:               internal(workflowdomain.RerunKeep),
			"noop":                              internal(workflowdomain.RerunKeep),
			// A playbook is idempotent, but its outcome cannot be proven after its executor stopped,
			// and a host a release-os released no longer runs the OS it needs.
			"ansible-playbook": {
				Runner: workflowdomain.RunnerKindAnsible, Rerun: workflowdomain.RerunConvergeOnLiveHosts,
				Resume: workflowdomain.ResumeAttention,
			},
		},
		Completions:   completionClasses(),
		Compensations: map[string]bool{compensationReturnCanceledDeployment: true},
	}
}

// returnCanceledDeployment declares the cancel compensation of a deploy's provision-os Task.
func returnCanceledDeployment() *workflowdomain.CancelBehavior {
	return &workflowdomain.CancelBehavior{Compensation: compensationReturnCanceledDeployment}
}

// declareJobs declares every Job steps name with retry, in first-appearance order.
func declareJobs(steps []workflowdomain.Task, retry workflowdomain.RetryGranularity) []workflowdomain.JobDeclaration {
	var jobs []workflowdomain.JobDeclaration
	seen := map[string]bool{}
	for _, step := range steps {
		if step.Job != "" && !seen[step.Job] {
			seen[step.Job] = true
			jobs = append(jobs, workflowdomain.JobDeclaration{Name: step.Job, Retry: retry})
		}
	}
	return jobs
}

// canceledDeploymentCompensation implements the return-canceled-deployment compensation with the
// provider Runner: it runs the Runner's return step for the interrupted Task's Server and turns the
// outcome into the timeline event the cancel records (decision 060).
type canceledDeploymentCompensation struct {
	runner temporalworkflow.StepLifecycleExecutor
}

var _ temporalworkflow.CompensationExecutor = canceledDeploymentCompensation{}

// Compensate implements temporalworkflow.CompensationExecutor.
func (c canceledDeploymentCompensation) Compensate(ctx context.Context, input temporalworkflow.StepExecutionInput) (string, string) {
	task := input.Step
	input.Step = workflowdomain.Task{
		ID: task.ID + "/" + temporalworkflow.ReturnCanceledDeploymentKind, Kind: temporalworkflow.ReturnCanceledDeploymentKind,
		Name: task.Name, Executor: task.Executor, Targets: task.Targets, Attempt: task.Attempt, Status: workflowdomain.TaskRunning,
	}
	result := c.runner.Execute(ctx, input)
	switch result.Status {
	case workflowdomain.TaskSucceeded:
		return "canceled_deployment_returned_to_ready", "The deployment was canceled; Swallow returned the Server to Ready."
	case workflowdomain.TaskSkipped:
		return "canceled_deployment_left_as_is", "The deployment was canceled; Swallow left the Server as it is. " + result.WaitingReason
	}
	reason := "The provider step did not finish."
	if result.Error != nil && result.Error.Message != "" {
		reason = result.Error.Message
	}
	return "canceled_deployment_return_failed",
		"The deployment was canceled, but Swallow could not return the Server to Ready: " + reason + " Recover the Server."
}
