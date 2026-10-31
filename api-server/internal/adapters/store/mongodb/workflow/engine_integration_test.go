package workflow

import (
	"context"
	"errors"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/bson"

	storemongo "github.com/maple52046/swallow/internal/adapters/store/mongodb"
	"github.com/maple52046/swallow/internal/core/workflow/application"
	workflowdomain "github.com/maple52046/swallow/internal/core/workflow/domain"
)

var integrationCatalog = workflowdomain.PlanCatalog{
	TaskKinds: map[string]workflowdomain.TaskKind{
		"deploy": {Runner: workflowdomain.RunnerKindProvisioner, Resume: workflowdomain.ResumeObserve},
	},
	Completions: map[string]workflowdomain.CompletionClass{"marker": workflowdomain.CompletionProjection},
}

type markerProjection struct {
	write func(ctx context.Context, status workflowdomain.TaskStatus) error
	fail  bool
}

func (p *markerProjection) Project(ctx context.Context, transition application.TaskTransition) error {
	if p.fail && transition.Task.Status == workflowdomain.TaskSucceeded {
		return errors.New("projection failed")
	}
	return p.write(ctx, transition.Task.Status)
}

type engineStore struct {
	engine     *application.Engine
	workflows  *MongoWorkflowRepo
	ownership  *MongoOwnershipRepo
	projection *markerProjection
	markers    func() []string
}

func newEngineStore(t *testing.T) *engineStore {
	t.Helper()
	db := workflowTestDatabase(t)
	workflows, err := NewMongoWorkflowRepo(db)
	if err != nil {
		t.Fatalf("NewMongoWorkflowRepo: %v", err)
	}
	ownership, err := NewMongoOwnershipRepo(db)
	if err != nil {
		t.Fatalf("NewMongoOwnershipRepo: %v", err)
	}
	markers := db.Collection("markers")
	projection := &markerProjection{write: func(ctx context.Context, status workflowdomain.TaskStatus) error {
		_, err := markers.InsertOne(ctx, bson.M{"status": string(status)})
		return err
	}}
	engine, err := application.NewEngine(storemongo.NewTransactor(db.Client()), workflows, ownership, integrationCatalog,
		application.CompletionHandlers{Projections: map[string]application.ProjectionHandler{"marker": projection}}, 4)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	return &engineStore{engine: engine, workflows: workflows, ownership: ownership, projection: projection,
		markers: func() []string {
			cursor, err := markers.Find(context.Background(), bson.M{})
			if err != nil {
				t.Fatalf("read markers: %v", err)
			}
			var docs []struct {
				Status string `bson:"status"`
			}
			if err := cursor.All(context.Background(), &docs); err != nil {
				t.Fatalf("decode markers: %v", err)
			}
			statuses := make([]string, len(docs))
			for i, doc := range docs {
				statuses[i] = doc.Status
			}
			return statuses
		}}
}

func (s *engineStore) create(t *testing.T, id string, resources ...string) {
	t.Helper()
	task := workflowdomain.Task{
		ID: "deploy", Kind: "deploy", Name: "Deploy", Executor: workflowdomain.RunnerKindProvisioner,
		Status: workflowdomain.TaskPending, Attempt: 1, Completion: []workflowdomain.Completion{{Type: "marker"}},
	}
	refs := make([]workflowdomain.ResourceReference, len(resources))
	for i, resource := range resources {
		refs[i] = workflowdomain.ResourceReference{Kind: "server", ID: resource}
	}
	if err := s.workflows.Create(context.Background(), &workflowdomain.Workflow{
		ID: id, Status: workflowdomain.WorkflowPending, StartState: "pending", Steps: []workflowdomain.Task{task},
		TargetResources: refs, RequestedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("Create: %v", err)
	}
}

func (s *engineStore) start(t *testing.T, id string) int64 {
	t.Helper()
	generation, err := s.engine.Dispatch(context.Background(), id)
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if _, err := s.engine.Advance(context.Background(), application.AdvanceRequest{WorkflowID: id, Generation: generation}); err != nil {
		t.Fatalf("Advance: %v", err)
	}
	return generation
}

// A Task's outcome and its declared projection commit in one transaction: a failing projection
// leaves the Task running, with no marker of the outcome.
func TestEngineRecordAndProjectionAreAtomic(t *testing.T) {
	s := newEngineStore(t)
	s.create(t, "w1", "s1")
	gen := s.start(t, "w1")
	ref := application.TaskRef{WorkflowID: "w1", Generation: gen, TaskID: "deploy", Attempt: 1}

	s.projection.fail = true
	if err := s.engine.Record(context.Background(), ref, application.Outcome{}); err == nil {
		t.Fatal("Record with a failing projection = nil, want its error")
	}
	w, _ := s.workflows.FindByID(context.Background(), "w1")
	if w.Steps[0].Status != workflowdomain.TaskRunning {
		t.Fatalf("Task after the failed transition = %s, want it still running", w.Steps[0].Status)
	}
	if got := s.markers(); len(got) != 1 || got[0] != "running" {
		t.Fatalf("markers = %v, want only the running transition", got)
	}

	s.projection.fail = false
	if err := s.engine.Record(context.Background(), ref, application.Outcome{}); err != nil {
		t.Fatalf("Record: %v", err)
	}
	if got := s.markers(); len(got) != 2 || got[1] != "succeeded" {
		t.Fatalf("markers = %v, want running then succeeded", got)
	}
}

// Finishing a Workflow and releasing its resources commit together, and a second Workflow waits
// for the resources until then.
func TestEngineOwnershipIsExclusiveAndReleasedWithTheTerminalTransition(t *testing.T) {
	s := newEngineStore(t)
	s.create(t, "w1", "s1")
	s.create(t, "w2", "s1")
	gen1 := s.start(t, "w1")
	gen2, _ := s.engine.Dispatch(context.Background(), "w2")
	waiting, err := s.engine.Advance(context.Background(), application.AdvanceRequest{WorkflowID: "w2", Generation: gen2})
	if err != nil || waiting.Action != application.AdvanceWaitResources {
		t.Fatalf("second Workflow's Advance = %+v, %v; want to wait for resources", waiting, err)
	}

	if err := s.engine.Record(context.Background(), application.TaskRef{WorkflowID: "w1", Generation: gen1, TaskID: "deploy", Attempt: 1}, application.Outcome{}); err != nil {
		t.Fatalf("Record: %v", err)
	}
	if finished, err := s.engine.Advance(context.Background(), application.AdvanceRequest{WorkflowID: "w1", Generation: gen1}); err != nil || finished.Action != application.AdvanceFinished {
		t.Fatalf("first Workflow's last Advance = %+v, %v; want finished", finished, err)
	}
	if owned, _ := s.ownership.FindByOwner(context.Background(), "w1"); len(owned) != 0 {
		t.Fatalf("the finished Workflow still owns %+v", owned)
	}
	started, err := s.engine.Advance(context.Background(), application.AdvanceRequest{WorkflowID: "w2", Generation: gen2})
	if err != nil || started.Action != application.AdvanceRun || len(started.Start) != 1 {
		t.Fatalf("second Workflow after the release = %+v, %v; want it to start", started, err)
	}
}

// Live progress is fenced by the dispatch generation, and commands are refused once a Workflow
// finished.
func TestWorkflowRepoLiveFencingAndCommands(t *testing.T) {
	s := newEngineStore(t)
	s.create(t, "w1", "s1")
	gen := s.start(t, "w1")
	live := &workflowdomain.TaskLive{CurrentTask: "install"}
	if err := s.workflows.UpdateTaskLive(context.Background(), "w1", gen, "deploy", 1, nil, nil, live); err != nil {
		t.Fatalf("UpdateTaskLive: %v", err)
	}
	if err := s.workflows.UpdateTaskLive(context.Background(), "w1", gen-1, "deploy", 1, nil, nil, live); !errors.Is(err, workflowdomain.ErrDispatchFenced) {
		t.Fatalf("UpdateTaskLive with an old generation = %v, want ErrDispatchFenced", err)
	}
	if err := s.workflows.UpdateTaskLive(context.Background(), "w1", gen, "deploy", 2, nil, nil, live); !errors.Is(err, workflowdomain.ErrTaskNotRunning) {
		t.Fatalf("UpdateTaskLive for another attempt = %v, want ErrTaskNotRunning", err)
	}

	if err := s.workflows.RequestRetry(context.Background(), "w1", workflowdomain.RetryCommand{TaskID: "deploy"}); err != nil {
		t.Fatalf("RequestRetry: %v", err)
	}
	if err := s.workflows.RequestCancel(context.Background(), "w1", workflowdomain.CancelCommand{Reason: "first"}); err != nil {
		t.Fatalf("RequestCancel: %v", err)
	}
	if err := s.workflows.RequestCancel(context.Background(), "w1", workflowdomain.CancelCommand{Reason: "second"}); err != nil {
		t.Fatalf("a repeated RequestCancel: %v", err)
	}
	w, _ := s.workflows.FindByID(context.Background(), "w1")
	if w.Commands.Sequence != 2 || w.Commands.Cancel == nil || w.Commands.Cancel.Reason != "first" {
		t.Fatalf("commands = %+v, want two commands keeping the first cancel", w.Commands)
	}
	if err := s.engine.FinishCanceled(context.Background(), "w1", gen); err != nil {
		t.Fatalf("FinishCanceled: %v", err)
	}
	if err := s.workflows.RequestRetry(context.Background(), "w1", workflowdomain.RetryCommand{TaskID: "deploy"}); !errors.Is(err, workflowdomain.ErrWorkflowControlConflict) {
		t.Fatalf("RequestRetry on a canceled Workflow = %v, want ErrWorkflowControlConflict", err)
	}
	if ids, _ := s.workflows.ListUnfinishedIDs(context.Background()); len(ids) != 0 {
		t.Fatalf("ListUnfinishedIDs = %v, want none", ids)
	}
}
