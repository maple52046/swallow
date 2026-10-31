package temporalworkflow

import (
	"context"
	"os"
	"regexp"
	"testing"
	"time"

	enumspb "go.temporal.io/api/enums/v1"
	historypb "go.temporal.io/api/history/v1"
	"go.temporal.io/api/temporalproto"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"
	sdkworkflow "go.temporal.io/sdk/workflow"

	operationdomain "github.com/maple52046/swallow/internal/operation/domain"
)

const replayFixturePath = "testdata/operation-v1-noop.json"

// TestOperationWorkflowV1ReplaysGoldenHistory is the CI gate for deterministic changes
// to the persisted v1 definition. Incompatible edits need Temporal patching or a new
// workflow type rather than silently invalidating in-flight histories.
func TestOperationWorkflowV1ReplaysGoldenHistory(t *testing.T) {
	replayer := worker.NewWorkflowReplayer()
	replayer.RegisterWorkflowWithOptions(OperationWorkflowV1, sdkworkflow.RegisterOptions{Name: WorkflowNameV1})
	if err := replayer.ReplayWorkflowHistoryFromJSONFile(nil, replayFixturePath); err != nil {
		t.Fatalf("replay %s: %v", replayFixturePath, err)
	}
}

// TestUpdateOperationWorkflowV1ReplayFixture deliberately requires an opt-in. It runs
// only mock activities on an isolated task queue, so refreshing history cannot touch
// Mongo, a provider, Ansible, or any managed Server.
func TestUpdateOperationWorkflowV1ReplayFixture(t *testing.T) {
	if os.Getenv("UPDATE_TEMPORAL_REPLAY_FIXTURES") != "1" {
		t.Skip("set UPDATE_TEMPORAL_REPLAY_FIXTURES=1 to refresh the golden history")
	}
	address := os.Getenv("TEMPORAL_ADDRESS")
	if address == "" {
		address = "127.0.0.1:7233"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	temporalClient, err := client.Dial(client.Options{HostPort: address, Identity: "swallow-replay-fixture"})
	if err != nil {
		t.Fatalf("connect Temporal: %v", err)
	}
	defer temporalClient.Close()

	taskQueue := "swallow-replay-fixture"
	temporalWorker := worker.New(temporalClient, taskQueue, worker.Options{Identity: "swallow-replay-fixture"})
	temporalWorker.RegisterWorkflowWithOptions(OperationWorkflowV1,
		sdkworkflow.RegisterOptions{Name: WorkflowNameV1})
	registerFixtureActivities(temporalWorker)
	if err := temporalWorker.Start(); err != nil {
		t.Fatalf("start fixture worker: %v", err)
	}
	defer temporalWorker.Stop()

	workflowID := "swallow-replay-fixture/operation-v1"
	run, err := temporalClient.ExecuteWorkflow(ctx, client.StartWorkflowOptions{
		ID: workflowID, TaskQueue: taskQueue,
		WorkflowIDReusePolicy: enumspb.WORKFLOW_ID_REUSE_POLICY_ALLOW_DUPLICATE,
	}, WorkflowNameV1, WorkflowInput{
		OperationID: "fixture-operation-v1", Kind: operationdomain.WorkflowKindCustom,
		SiteID: "fixture-site", Definition: "fixture.noop", DefinitionVersion: 1,
		LeaseDuration: time.Minute, MaxParallelism: 1, ResourceKeys: []string{"server:fixture"},
		Steps: []operationdomain.Task{{
			ID: "noop", Kind: "noop", Name: "No external side effect",
			Executor: operationdomain.RunnerKindInternal,
			Status:   operationdomain.TaskPending, Attempt: 1,
		}},
	})
	if err != nil {
		t.Fatalf("start fixture workflow: %v", err)
	}
	if err := run.Get(ctx, nil); err != nil {
		t.Fatalf("complete fixture workflow: %v", err)
	}

	history := &historypb.History{}
	iterator := temporalClient.GetWorkflowHistory(ctx, run.GetID(), run.GetRunID(), false,
		enumspb.HISTORY_EVENT_FILTER_TYPE_ALL_EVENT)
	for iterator.HasNext() {
		event, err := iterator.Next()
		if err != nil {
			t.Fatalf("read fixture history: %v", err)
		}
		history.Events = append(history.Events, event)
	}
	encoded, err := (temporalproto.CustomJSONMarshalOptions{Indent: "  "}).Marshal(history)
	if err != nil {
		t.Fatalf("encode fixture history: %v", err)
	}
	hostname, _ := os.Hostname()
	stickyQueue := regexp.MustCompile(`"name": "` + regexp.QuoteMeta(hostname) + `:[0-9a-f-]+"`)
	encoded = stickyQueue.ReplaceAll(encoded, []byte(`"name": "swallow-replay-fixture:sticky"`))
	if err := os.MkdirAll("testdata", 0o755); err != nil {
		t.Fatalf("create fixture directory: %v", err)
	}
	if err := os.WriteFile(replayFixturePath, append(encoded, '\n'), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
}

func registerFixtureActivities(temporalWorker worker.Worker) {
	temporalWorker.RegisterActivityWithOptions(func(context.Context, LeaseRequest) ([]operationdomain.ResourceLease, error) {
		return []operationdomain.ResourceLease{{ResourceKey: "server:fixture", Owner: "fixture", FencingToken: 1}}, nil
	}, activity.RegisterOptions{Name: ActivityAcquireLeases})
	temporalWorker.RegisterActivityWithOptions(func(context.Context, LeaseRenewal) error { return nil },
		activity.RegisterOptions{Name: ActivityRenewLeases})
	temporalWorker.RegisterActivityWithOptions(func(context.Context, []operationdomain.ResourceLease) error { return nil },
		activity.RegisterOptions{Name: ActivityReleaseLeases})
	temporalWorker.RegisterActivityWithOptions(func(context.Context, StateUpdate) error { return nil },
		activity.RegisterOptions{Name: ActivityUpdateState})
	temporalWorker.RegisterActivityWithOptions(func(context.Context, StepUpdate) error { return nil },
		activity.RegisterOptions{Name: ActivityUpdateStep})
	temporalWorker.RegisterActivityWithOptions(func(context.Context, StepExecutionInput) (StepExecutionResult, error) {
		return StepExecutionResult{Status: operationdomain.TaskSucceeded, Progress: 100}, nil
	}, activity.RegisterOptions{Name: ActivityExecuteStep})
}
