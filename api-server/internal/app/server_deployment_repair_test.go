package app

import (
	"context"
	"sort"
	"sync"
	"testing"
	"time"

	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/testsuite"

	"github.com/maple52046/swallow/internal/adapters/executor/temporalworkflow"
	serverdomain "github.com/maple52046/swallow/internal/core/server/domain"
	workflowdomain "github.com/maple52046/swallow/internal/core/workflow/domain"
)

// repairServerRepo is an in-memory fleet for the deployment repair sweep. beforeFind lets a test
// change a Server between the sweep's list and its pre-write re-read.
type repairServerRepo struct {
	serverdomain.ServerRepository
	mu         sync.Mutex
	servers    map[string]*serverdomain.Server
	writes     int
	beforeFind func(id string, server *serverdomain.Server)
}

func (r *repairServerRepo) List(context.Context, serverdomain.ListFilter) (serverdomain.ListResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	items := make([]*serverdomain.Server, 0, len(r.servers))
	for _, server := range r.servers {
		items = append(items, server)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return serverdomain.ListResult{Servers: items, Total: len(items)}, nil
}

func (r *repairServerRepo) FindByID(_ context.Context, id string) (*serverdomain.Server, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	server, ok := r.servers[id]
	if !ok {
		return nil, serverdomain.ErrServerNotFound
	}
	if r.beforeFind != nil {
		r.beforeFind(id, server)
	}
	return server, nil
}

func (r *repairServerRepo) SetDeployment(_ context.Context, id string, deployment *serverdomain.DeploymentStatus) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.servers[id].Deployment = deployment
	r.writes++
	return nil
}

// repairWorkflowRepo serves Workflow records by ID and reports an unknown ID the way the Mongo
// adapter does.
type repairWorkflowRepo struct {
	workflowdomain.WorkflowRepository
	workflows map[string]*workflowdomain.Workflow
}

func (r *repairWorkflowRepo) FindByID(_ context.Context, id string) (*workflowdomain.Workflow, error) {
	workflow, ok := r.workflows[id]
	if !ok {
		return nil, workflowdomain.ErrWorkflowNotV3
	}
	return workflow, nil
}

func TestRepairStuckDeploymentProjections(t *testing.T) {
	started := time.Date(2026, 10, 16, 17, 0, 0, 0, time.UTC)
	finished := started.Add(10 * time.Minute)
	inFlight := func(state serverdomain.DeploymentState) *serverdomain.DeploymentStatus {
		return &serverdomain.DeploymentStatus{
			State: state, OperationID: "operation-1", StepID: "provision-a", Attempt: 1, StartedAt: started,
		}
	}
	workflowWith := func(status workflowdomain.WorkflowStatus, task workflowdomain.TaskStatus, failure *workflowdomain.NormalizedError) *workflowdomain.Workflow {
		var finishedAt *time.Time
		if status.Terminal() {
			finishedAt = &finished
		}
		return &workflowdomain.Workflow{
			ID: "operation-1", Kind: workflowdomain.WorkflowKindDeployOS, Status: status, FinishedAt: finishedAt,
			Steps: []workflowdomain.Task{{
				ID: "provision-a", Kind: "provision-os", Status: task, Attempt: 1, Error: failure,
				Targets: []workflowdomain.ResourceReference{{Kind: "server", ID: "server-a"}},
			}},
		}
	}

	tests := []struct {
		name       string
		deployment *serverdomain.DeploymentStatus
		workflow   *workflowdomain.Workflow
		beforeFind func(id string, server *serverdomain.Server)
		wantState  serverdomain.DeploymentState
		wantOpID   string
		wantCode   string
		wantWrites int
	}{
		{
			name:       "canceled workflow settles a task left waiting on the provider",
			deployment: inFlight(serverdomain.DeploymentDeploying),
			workflow:   workflowWith(workflowdomain.WorkflowCanceled, workflowdomain.TaskWaitingExternal, nil),
			wantState:  serverdomain.DeploymentCanceled, wantOpID: "operation-1", wantWrites: 1,
		},
		{
			name:       "canceled workflow settles a verifying deployment",
			deployment: inFlight(serverdomain.DeploymentVerifying),
			workflow:   workflowWith(workflowdomain.WorkflowCanceled, workflowdomain.TaskRunning, nil),
			wantState:  serverdomain.DeploymentCanceled, wantOpID: "operation-1", wantWrites: 1,
		},
		{
			name:       "recorded task outcome is projected as-is",
			deployment: inFlight(serverdomain.DeploymentDeploying),
			workflow: workflowWith(workflowdomain.WorkflowFailed, workflowdomain.TaskFailed,
				&workflowdomain.NormalizedError{Code: "deployment_failed", Message: "Install failed."}),
			wantState: serverdomain.DeploymentFailed, wantOpID: "operation-1", wantCode: "deployment_failed", wantWrites: 1,
		},
		{
			name:       "running workflow is never touched",
			deployment: inFlight(serverdomain.DeploymentDeploying),
			workflow:   workflowWith(workflowdomain.WorkflowRunning, workflowdomain.TaskRunning, nil),
			wantState:  serverdomain.DeploymentDeploying, wantOpID: "operation-1",
		},
		{
			name:       "workflow parked for the operator is never touched",
			deployment: inFlight(serverdomain.DeploymentDeploying),
			workflow:   workflowWith(workflowdomain.WorkflowRequiresAttention, workflowdomain.TaskRunning, nil),
			wantState:  serverdomain.DeploymentDeploying, wantOpID: "operation-1",
		},
		{
			name:       "ended workflow without a task outcome is not guessed",
			deployment: inFlight(serverdomain.DeploymentDeploying),
			workflow:   workflowWith(workflowdomain.WorkflowFailed, workflowdomain.TaskRunning, nil),
			wantState:  serverdomain.DeploymentDeploying, wantOpID: "operation-1",
		},
		{
			name:       "missing workflow is not guessed",
			deployment: inFlight(serverdomain.DeploymentDeploying),
			wantState:  serverdomain.DeploymentDeploying, wantOpID: "operation-1",
		},
		{
			name: "settled deployment is never rewritten",
			deployment: &serverdomain.DeploymentStatus{
				State: serverdomain.DeploymentSucceeded, OperationID: "operation-1", StepID: "provision-a", Attempt: 1,
			},
			workflow:  workflowWith(workflowdomain.WorkflowCanceled, workflowdomain.TaskCanceled, nil),
			wantState: serverdomain.DeploymentSucceeded, wantOpID: "operation-1",
		},
		{
			name:       "deploy accepted after the list is not overwritten",
			deployment: inFlight(serverdomain.DeploymentDeploying),
			workflow:   workflowWith(workflowdomain.WorkflowCanceled, workflowdomain.TaskWaitingExternal, nil),
			beforeFind: func(_ string, server *serverdomain.Server) {
				server.Deployment = &serverdomain.DeploymentStatus{
					State: serverdomain.DeploymentDeploying, OperationID: "operation-2", StepID: "provision-a", Attempt: 1,
				}
			},
			wantState: serverdomain.DeploymentDeploying, wantOpID: "operation-2",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			servers := &repairServerRepo{
				servers:    map[string]*serverdomain.Server{"server-a": {ID: "server-a", Deployment: tc.deployment}},
				beforeFind: tc.beforeFind,
			}
			workflows := &repairWorkflowRepo{workflows: map[string]*workflowdomain.Workflow{}}
			if tc.workflow != nil {
				workflows.workflows[tc.workflow.ID] = tc.workflow
			}

			if err := repairStuckDeploymentProjections(context.Background(), servers, workflows); err != nil {
				t.Fatalf("repairStuckDeploymentProjections() error = %v", err)
			}
			got := servers.servers["server-a"].Deployment
			if got == nil {
				t.Fatalf("deployment = nil, want state %q", tc.wantState)
			}
			if got.State != tc.wantState || got.OperationID != tc.wantOpID {
				t.Errorf("deployment = (%q, %q), want (%q, %q)", got.State, got.OperationID, tc.wantState, tc.wantOpID)
			}
			if got.Code != tc.wantCode {
				t.Errorf("deployment code = %q, want %q", got.Code, tc.wantCode)
			}
			if servers.writes != tc.wantWrites {
				t.Errorf("deployment writes = %d, want %d", servers.writes, tc.wantWrites)
			}
			if tc.wantWrites > 0 {
				if !got.StartedAt.Equal(started) {
					t.Errorf("deployment startedAt = %v, want the Server's recorded start %v", got.StartedAt, started)
				}
				if got.FinishedAt == nil {
					t.Error("deployment finishedAt = nil, want a settled outcome to carry its finish time")
				}
			}
		})
	}
}

// TestRepairCanceledDeploymentFromRecordedOutcome reproduces lab-afde-mi308-1 and -2: their
// provision-os succeeded at attempt 6, but canceling the platform Workflow rewrote the Server
// result to canceled at attempt 1 (start and finish at the cancel time). The durable Task kept its
// outcome, so the sweep restores it; a canceled result whose Task did not record an outcome at the
// same or a later attempt stays canceled.
func TestRepairCanceledDeploymentFromRecordedOutcome(t *testing.T) {
	canceledAt := time.Date(2026, 10, 19, 6, 44, 3, 0, time.UTC)
	taskStarted := time.Date(2026, 10, 19, 5, 22, 58, 0, time.UTC)
	taskFinished := taskStarted.Add(time.Second)
	staleCancel := func(attempt int) *serverdomain.DeploymentStatus {
		return &serverdomain.DeploymentStatus{
			State: serverdomain.DeploymentCanceled, OperationID: "operation-1", StepID: "provision-a",
			Attempt: attempt, StartedAt: canceledAt, FinishedAt: &canceledAt,
		}
	}
	workflowWith := func(status workflowdomain.WorkflowStatus, task workflowdomain.TaskStatus, attempt int) *workflowdomain.Workflow {
		return &workflowdomain.Workflow{
			ID: "operation-1", Kind: workflowdomain.WorkflowKindDeployKubernetes, Status: status, FinishedAt: &canceledAt,
			Steps: []workflowdomain.Task{{
				ID: "provision-a", Kind: "provision-os", Status: task, Attempt: attempt,
				StartedAt: &taskStarted, FinishedAt: &taskFinished,
				Targets: []workflowdomain.ResourceReference{{Kind: "server", ID: "server-a"}},
			}},
		}
	}
	tests := []struct {
		name        string
		deployment  *serverdomain.DeploymentStatus
		workflow    *workflowdomain.Workflow
		wantState   serverdomain.DeploymentState
		wantAttempt int
	}{
		{
			name:       "succeeded task restores the result",
			deployment: staleCancel(1), workflow: workflowWith(workflowdomain.WorkflowCanceled, workflowdomain.TaskSucceeded, 6),
			wantState: serverdomain.DeploymentSucceeded, wantAttempt: 6,
		},
		{
			name:       "failed task restores the result",
			deployment: staleCancel(1), workflow: workflowWith(workflowdomain.WorkflowCanceled, workflowdomain.TaskFailed, 6),
			wantState: serverdomain.DeploymentFailed, wantAttempt: 6,
		},
		{
			name:       "task that was itself canceled keeps the canceled result",
			deployment: staleCancel(1), workflow: workflowWith(workflowdomain.WorkflowCanceled, workflowdomain.TaskCanceled, 1),
			wantState: serverdomain.DeploymentCanceled, wantAttempt: 1,
		},
		{
			name:       "outcome of an older attempt does not replace a newer cancel",
			deployment: staleCancel(2), workflow: workflowWith(workflowdomain.WorkflowCanceled, workflowdomain.TaskSucceeded, 1),
			wantState: serverdomain.DeploymentCanceled, wantAttempt: 2,
		},
		{
			name:       "workflow that has not ended is never touched",
			deployment: staleCancel(1), workflow: workflowWith(workflowdomain.WorkflowRunning, workflowdomain.TaskSucceeded, 6),
			wantState: serverdomain.DeploymentCanceled, wantAttempt: 1,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			servers := &repairServerRepo{servers: map[string]*serverdomain.Server{"server-a": {ID: "server-a", Deployment: tc.deployment}}}
			workflows := &repairWorkflowRepo{workflows: map[string]*workflowdomain.Workflow{tc.workflow.ID: tc.workflow}}

			if err := repairStuckDeploymentProjections(context.Background(), servers, workflows); err != nil {
				t.Fatalf("repairStuckDeploymentProjections() error = %v", err)
			}

			got := servers.servers["server-a"].Deployment
			if got.State != tc.wantState || got.Attempt != tc.wantAttempt {
				t.Fatalf("deployment = (%q, attempt %d), want (%q, attempt %d)", got.State, got.Attempt, tc.wantState, tc.wantAttempt)
			}
			restored := tc.wantState != serverdomain.DeploymentCanceled
			if restored && (!got.StartedAt.Equal(taskStarted) || got.FinishedAt == nil || !got.FinishedAt.Equal(taskFinished)) {
				t.Errorf("deployment timing = %v -> %v, want the Task's %v -> %v", got.StartedAt, got.FinishedAt, taskStarted, taskFinished)
			}
			if wantWrites := map[bool]int{true: 1, false: 0}[restored]; servers.writes != wantWrites {
				t.Errorf("deployment writes = %d, want %d", servers.writes, wantWrites)
			}
		})
	}
}

// cancelProjectionWorkflowRepo keeps the durable Task projection the workflow writes through the
// real UpdateStep activity.
type cancelProjectionWorkflowRepo struct {
	workflowdomain.WorkflowRepository
	mu    sync.Mutex
	tasks map[string]workflowdomain.Task
}

func (r *cancelProjectionWorkflowRepo) UpdateStep(_ context.Context, _ string, step workflowdomain.Task) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tasks[step.ID] = step
	return nil
}

func (r *cancelProjectionWorkflowRepo) AppendEvent(context.Context, workflowdomain.TimelineEvent) error {
	return nil
}

// TestDeployOSCancelDuringProvisionSettlesServerDeployment is the end-to-end regression for a
// deploy-os canceled by the operator while MAAS is installing: the Workflow ends canceled and,
// through the real UpdateStep activity and Server deployment observer, the Server's Deployment
// result must leave deploying for canceled instead of reporting in-flight work forever.
func TestDeployOSCancelDuringProvisionSettlesServerDeployment(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	server := &serverdomain.Server{ID: "server-a", Deployment: &serverdomain.DeploymentStatus{
		State: serverdomain.DeploymentDeploying, OperationID: "operation-deploy-os", StepID: "provision-a", Attempt: 1,
	}}
	servers := &deploymentProjectionTestRepo{server: server}
	operations := &cancelProjectionWorkflowRepo{tasks: map[string]workflowdomain.Task{}}
	activities := temporalworkflow.NewActivities(operations, nil, nil, serverDeploymentStepObserver{servers: servers})
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	var mu sync.Mutex
	states := []workflowdomain.WorkflowStatus{}

	env.RegisterActivityWithOptions(func(context.Context, temporalworkflow.LeaseRequest) ([]workflowdomain.ResourceLease, error) {
		return []workflowdomain.ResourceLease{{ResourceKey: "server:server-a", Owner: "workflow", FencingToken: 1}}, nil
	}, activity.RegisterOptions{Name: temporalworkflow.ActivityAcquireLeases})
	env.RegisterActivityWithOptions(func(context.Context, temporalworkflow.LeaseRenewal) error { return nil },
		activity.RegisterOptions{Name: temporalworkflow.ActivityRenewLeases})
	env.RegisterActivityWithOptions(func(context.Context, []workflowdomain.ResourceLease) error { return nil },
		activity.RegisterOptions{Name: temporalworkflow.ActivityReleaseLeases})
	env.RegisterActivityWithOptions(func(ctx context.Context, input temporalworkflow.StepUpdate) error {
		// Real Temporal never dispatches a projection scheduled on the canceled workflow context,
		// but the test environment still runs it on a goroutine nobody waits for. Holding canceled
		// projections back keeps such a write from landing before the workflow completes, so only
		// projections the workflow awaited are observed.
		if input.Step.Status == workflowdomain.TaskCanceled {
			time.Sleep(200 * time.Millisecond)
		}
		return activities.UpdateStep(ctx, input)
	}, activity.RegisterOptions{Name: temporalworkflow.ActivityUpdateStep})
	env.RegisterActivityWithOptions(func(_ context.Context, input temporalworkflow.StateUpdate) error {
		mu.Lock()
		states = append(states, input.Status)
		mu.Unlock()
		return nil
	}, activity.RegisterOptions{Name: temporalworkflow.ActivityUpdateState})
	env.RegisterActivityWithOptions(activities.AppendEvent, activity.RegisterOptions{Name: temporalworkflow.ActivityAppendEvent})
	env.RegisterActivityWithOptions(func(ctx context.Context, input temporalworkflow.StepExecutionInput) (temporalworkflow.StepExecutionResult, error) {
		// The cancel then returns the interrupted deployment; that step has its own executor
		// tests, so here it only reports that it left the Server as it is.
		if input.Step.Kind == temporalworkflow.ReturnCanceledDeploymentKind {
			return temporalworkflow.StepExecutionResult{Status: workflowdomain.TaskSkipped, WaitingReason: "The Server is already Ready."}, nil
		}
		env.CancelWorkflow()
		select {
		case <-ctx.Done():
			return temporalworkflow.StepExecutionResult{}, ctx.Err()
		case <-release:
			return temporalworkflow.StepExecutionResult{}, context.Canceled
		}
	}, activity.RegisterOptions{Name: temporalworkflow.ActivityExecuteStep})

	env.ExecuteWorkflow(temporalworkflow.OperationWorkflowV1, temporalworkflow.WorkflowInput{
		OperationID: "operation-deploy-os", Kind: workflowdomain.WorkflowKindDeployOS,
		SiteID: "site-a", Definition: "os-deployment", DefinitionVersion: 1,
		LeaseDuration: time.Minute, MaxParallelism: 1, ResourceKeys: []string{"server:server-a"},
		Steps: []workflowdomain.Task{{
			ID: "provision-a", Kind: "provision-os", Name: "Provision server-a",
			Executor: workflowdomain.RunnerKindProvisioner, Status: workflowdomain.TaskPending, Attempt: 1,
			Targets: []workflowdomain.ResourceReference{{Kind: "server", ID: server.ID}},
		}},
	})
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("canceled workflow returned an error: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(states) == 0 || states[len(states)-1] != workflowdomain.WorkflowCanceled {
		t.Errorf("Operation states = %v, want to end canceled", states)
	}
	operations.mu.Lock()
	task := operations.tasks["provision-a"]
	operations.mu.Unlock()
	if task.Status != workflowdomain.TaskCanceled {
		t.Errorf("provision-a Task status = %q, want %q", task.Status, workflowdomain.TaskCanceled)
	}
	if server.Deployment == nil || server.Deployment.State != serverdomain.DeploymentCanceled {
		t.Errorf("Server deployment = %+v, want state %q", server.Deployment, serverdomain.DeploymentCanceled)
	}
}
