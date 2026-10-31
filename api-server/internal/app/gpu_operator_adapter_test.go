package app

import (
	"context"
	"testing"
	"time"

	platformdomain "github.com/maple52046/swallow/internal/core/platform/domain"
	workflowapp "github.com/maple52046/swallow/internal/core/workflow/application"
	workflowdomain "github.com/maple52046/swallow/internal/core/workflow/domain"
)

// stateRecorder captures UpdateGPUOperatorState calls; SetGPUOperator is unused by the observer.
type stateRecorder struct {
	platformID, workflowID string
	state                  platformdomain.GPUOperatorState
	calls                  int
}

func (r *stateRecorder) SetGPUOperator(context.Context, string, *platformdomain.GPUOperator, platformdomain.GPUStackOwner) error {
	return nil
}

func (r *stateRecorder) UpdateGPUOperatorState(
	_ context.Context, platformID, workflowID string, state platformdomain.GPUOperatorState, _ time.Time,
) (bool, error) {
	r.platformID, r.workflowID, r.state = platformID, workflowID, state
	r.calls++
	return true, nil
}

func gpuOperatorTestTask(mode string, status workflowdomain.TaskStatus) workflowdomain.Task {
	prepared := &workflowapp.PreparedAnsibleStep{
		Playbook:  gpuOperatorPlaybook,
		ExtraVars: map[string]any{"swallow_amd_gpu_operator_state": mode},
		Targets:   []workflowdomain.ResourceReference{{Kind: "server", ID: "c1"}},
	}
	task := gpuOperatorTask(prepared, "p1", "deploy-1", []string{"validate-platform"}, "Install AMD GPU Operator")
	task.Status = status
	return task
}

func TestGPUOperatorTaskCarriesTheObserverIdentity(t *testing.T) {
	task := gpuOperatorTestTask("present", workflowdomain.TaskPending)
	if task.ID != gpuOperatorStepID || task.Job != jobConfigureGPUOperator || task.Executor != workflowdomain.RunnerKindAnsible {
		t.Errorf("gpuOperatorTask = %+v, want the configure-gpu-operator ansible Task", task)
	}
	if len(task.DependsOn) != 1 || task.DependsOn[0] != "validate-platform" {
		t.Errorf("gpuOperatorTask DependsOn = %v, want [validate-platform]", task.DependsOn)
	}
	params := task.Parameters
	if params[gpuOperatorStepParamMode] != "present" || params[gpuOperatorStepParamOwner] != "p1" ||
		params[gpuOperatorStepParamRecord] != "deploy-1" || params["playbook"] != gpuOperatorPlaybook {
		t.Errorf("gpuOperatorTask parameters = %v, want mode, platform, record, and playbook", params)
	}
}

func TestGPUOperatorObserverProjectsTaskStates(t *testing.T) {
	tests := []struct {
		name   string
		mode   string
		status workflowdomain.TaskStatus
		want   platformdomain.GPUOperatorState
	}{
		{name: "install running", mode: "present", status: workflowdomain.TaskRunning, want: platformdomain.GPUOperatorInstalling},
		{name: "install succeeded", mode: "present", status: workflowdomain.TaskSucceeded, want: platformdomain.GPUOperatorReady},
		{name: "install needs attention", mode: "present", status: workflowdomain.TaskRequiresAttention, want: platformdomain.GPUOperatorFailed},
		{name: "install canceled", mode: "present", status: workflowdomain.TaskCanceled, want: platformdomain.GPUOperatorFailed},
		{name: "uninstall running", mode: "absent", status: workflowdomain.TaskRunning, want: platformdomain.GPUOperatorUninstalling},
		{name: "uninstall succeeded", mode: "absent", status: workflowdomain.TaskSucceeded, want: platformdomain.GPUOperatorAbsent},
		{name: "uninstall failed", mode: "absent", status: workflowdomain.TaskFailed, want: platformdomain.GPUOperatorFailed},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			recorder := &stateRecorder{}
			observer := gpuOperatorStepObserver{recorder: recorder}
			// The running Workflow is a rerun with a new id; the fence is the recorded intent.
			if err := observer.ObserveStep(context.Background(), "rerun-2", gpuOperatorTestTask(tc.mode, tc.status)); err != nil {
				t.Fatalf("ObserveStep: %v", err)
			}
			if recorder.state != tc.want || recorder.platformID != "p1" || recorder.workflowID != "deploy-1" {
				t.Errorf("ObserveStep recorded %s on %s fenced %s, want %s on p1 fenced deploy-1",
					recorder.state, recorder.platformID, recorder.workflowID, tc.want)
			}
		})
	}
}

func TestGPUOperatorObserverIgnoresOtherTasksAndPending(t *testing.T) {
	recorder := &stateRecorder{}
	observer := gpuOperatorStepObserver{recorder: recorder}
	other := workflowdomain.Task{ID: installPlatformStepID, Kind: "ansible-playbook", Status: workflowdomain.TaskSucceeded,
		Parameters: map[string]any{"playbook": "deploy-kubernetes"}}
	for _, task := range []workflowdomain.Task{other, gpuOperatorTestTask("present", workflowdomain.TaskPending)} {
		if err := observer.ObserveStep(context.Background(), "deploy-1", task); err != nil {
			t.Fatalf("ObserveStep(%s): %v", task.ID, err)
		}
	}
	if recorder.calls != 0 {
		t.Errorf("ObserveStep wrote %d states, want none for a k0s Task or a pending GPU Task", recorder.calls)
	}
}

// TestPlatformDeploymentObserverLeavesTheGPUOperatorTaskToItsObserver pins that a GPU Operator
// Task finishing inside a deploy-kubernetes Workflow (its execution carries the Workflow's kind)
// does not run the k0s completion, which only the install-platform Task can satisfy.
func TestPlatformDeploymentObserverLeavesTheGPUOperatorTaskToItsObserver(t *testing.T) {
	observer := platformDeploymentObserver{}
	execution := &workflowdomain.AnsibleExecution{
		OperationID: "op-1", Kind: workflowdomain.WorkflowKindDeployKubernetes, PlatformID: "plat-1",
		StepID: gpuOperatorStepID,
	}
	if err := observer.AnsibleStepSucceeded(context.Background(), execution, workflowdomain.RunnerResult{}); err != nil {
		t.Fatalf("GPU Operator Task completion err = %v, want nil", err)
	}

	execution.StepID = installPlatformStepID
	if err := observer.AnsibleStepSucceeded(context.Background(), execution, workflowdomain.RunnerResult{}); err == nil {
		t.Fatal("k0s install without a credential succeeded, want the missing-credential error")
	}
}
