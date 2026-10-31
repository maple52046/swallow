package temporalworkflow

import (
	"context"
	"fmt"
	"strings"

	enumspb "go.temporal.io/api/enums/v1"
	"go.temporal.io/api/workflowservice/v1"
	"go.temporal.io/sdk/client"

	workflowapp "github.com/maple52046/swallow/internal/core/workflow/application"
	workflowdomain "github.com/maple52046/swallow/internal/core/workflow/domain"
)

// executionPrefix starts the Temporal Workflow ID of every swallow Workflow.
const executionPrefix = "swallow-operation/"

// Executor implements the core's WorkflowExecutor port with Temporal (decision 072). Every
// Workflow keeps one Temporal Workflow ID for life; each dispatch is a new run under it, at most
// one at a time.
type Executor struct {
	client    client.Client
	taskQueue string
	routes    map[string]RunnerRoute
}

var _ workflowapp.WorkflowExecutor = (*Executor)(nil)

// NewExecutor returns the executor. taskQueue is where driver runs execute; routes say where each
// Runner's Tasks go.
func NewExecutor(temporalClient client.Client, taskQueue string, routes map[string]RunnerRoute) *Executor {
	if taskQueue == "" {
		taskQueue = TaskQueue
	}
	return &Executor{client: temporalClient, taskQueue: taskQueue, routes: routes}
}

func executionID(workflowID string) string { return executionPrefix + workflowID }

// Start implements workflowapp.WorkflowExecutor.
func (e *Executor) Start(ctx context.Context, workflowID string, generation int64) (string, error) {
	id := executionID(workflowID)
	running, err := e.Running(ctx, workflowID)
	if err != nil {
		return "", err
	}
	if running {
		if err := e.client.TerminateWorkflow(ctx, id, "", "Superseded by a new dispatch."); err != nil && !isWorkflowExecutionGone(err) {
			return "", fmt.Errorf("terminate the previous run of %s: %w", workflowID, err)
		}
	}
	run, err := e.client.ExecuteWorkflow(ctx, client.StartWorkflowOptions{
		ID: id, TaskQueue: e.taskQueue,
		WorkflowIDReusePolicy:    enumspb.WORKFLOW_ID_REUSE_POLICY_ALLOW_DUPLICATE,
		WorkflowIDConflictPolicy: enumspb.WORKFLOW_ID_CONFLICT_POLICY_FAIL,
	}, DriverWorkflowName, DriverInput{WorkflowID: workflowID, Generation: generation, Routes: e.routes})
	if err != nil {
		return "", fmt.Errorf("start the driver of %s: %w", workflowID, err)
	}
	return run.GetRunID(), nil
}

// Running implements workflowapp.WorkflowExecutor.
func (e *Executor) Running(ctx context.Context, workflowID string) (bool, error) {
	description, err := e.client.DescribeWorkflowExecution(ctx, executionID(workflowID), "")
	if isWorkflowExecutionGone(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return description.GetWorkflowExecutionInfo().GetStatus() == enumspb.WORKFLOW_EXECUTION_STATUS_RUNNING, nil
}

// Wake implements workflowapp.WorkflowExecutor.
func (e *Executor) Wake(ctx context.Context, workflowID string) error {
	return gone(e.client.SignalWorkflow(ctx, executionID(workflowID), "", WakeSignal, nil))
}

// Cancel implements workflowapp.WorkflowExecutor.
func (e *Executor) Cancel(ctx context.Context, workflowID string) error {
	return gone(e.client.CancelWorkflow(ctx, executionID(workflowID), ""))
}

// OpenRuns implements workflowapp.WorkflowExecutor with a visibility query for running executions.
func (e *Executor) OpenRuns(ctx context.Context) ([]workflowapp.OpenRun, error) {
	var runs []workflowapp.OpenRun
	var token []byte
	for {
		response, err := e.client.ListWorkflow(ctx, &workflowservice.ListWorkflowExecutionsRequest{
			Query: "ExecutionStatus = 'Running'", NextPageToken: token,
		})
		if err != nil {
			return nil, fmt.Errorf("list running executions: %w", err)
		}
		for _, execution := range response.GetExecutions() {
			id := execution.GetExecution().GetWorkflowId()
			if !strings.HasPrefix(id, executionPrefix) {
				continue
			}
			runs = append(runs, workflowapp.OpenRun{
				WorkflowID: id, RunID: execution.GetExecution().GetRunId(),
				Current: execution.GetType().GetName() == DriverWorkflowName,
			})
		}
		token = response.GetNextPageToken()
		if len(token) == 0 {
			return runs, nil
		}
	}
}

// Terminate implements workflowapp.WorkflowExecutor.
func (e *Executor) Terminate(ctx context.Context, run workflowapp.OpenRun, reason string) error {
	return gone(e.client.TerminateWorkflow(ctx, run.WorkflowID, run.RunID, reason))
}

func gone(err error) error {
	if isWorkflowExecutionGone(err) {
		return workflowdomain.ErrWorkflowExecutionGone
	}
	return err
}
