package app

import (
	"context"
	"reflect"
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

func TestGPUOperatorTaskDeclaresItsStateCompletion(t *testing.T) {
	task := gpuOperatorTestTask("present", workflowdomain.TaskPending)
	if task.ID != gpuOperatorStepID || task.Job != jobConfigureGPUOperator || task.Executor != workflowdomain.RunnerKindAnsible {
		t.Errorf("gpuOperatorTask = %+v, want the configure-gpu-operator ansible Task", task)
	}
	if len(task.DependsOn) != 1 || task.DependsOn[0] != "validate-platform" {
		t.Errorf("gpuOperatorTask DependsOn = %v, want [validate-platform]", task.DependsOn)
	}
	if task.Parameters["playbook"] != gpuOperatorPlaybook {
		t.Errorf("gpuOperatorTask parameters = %v, want the playbook", task.Parameters)
	}
	want := []workflowdomain.Completion{{Type: completionGPUOperatorState, Params: map[string]any{
		completionParamPlatform: "p1", completionParamRecord: "deploy-1", completionParamMode: "present",
	}}}
	if !reflect.DeepEqual(task.Completion, want) {
		t.Errorf("gpuOperatorTask completion = %v, want %v", task.Completion, want)
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
			// The running Workflow is a rerun with a new id; the fence is the recorded intent.
			projectWith(t, workflowProjections(nil, nil, recorder, nil, nil), completionGPUOperatorState, "rerun-2",
				gpuOperatorTestTask(tc.mode, tc.status))
			if recorder.state != tc.want || recorder.platformID != "p1" || recorder.workflowID != "deploy-1" {
				t.Errorf("ObserveStep recorded %s on %s fenced %s, want %s on p1 fenced deploy-1",
					recorder.state, recorder.platformID, recorder.workflowID, tc.want)
			}
		})
	}
}

func TestGPUOperatorObserverIgnoresOtherTasksAndPending(t *testing.T) {
	recorder := &stateRecorder{}
	other := workflowdomain.Task{ID: installPlatformStepID, Kind: "ansible-playbook", Status: workflowdomain.TaskSucceeded,
		Parameters: map[string]any{"playbook": "deploy-kubernetes"}}
	for _, task := range []workflowdomain.Task{other, gpuOperatorTestTask("present", workflowdomain.TaskPending)} {
		projectWith(t, workflowProjections(nil, nil, recorder, nil, nil), completionGPUOperatorState, "deploy-1", task)
	}
	if recorder.calls != 0 {
		t.Errorf("ObserveStep wrote %d states, want none for a k0s Task or a pending GPU Task", recorder.calls)
	}
}
