package application

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	workflowdomain "github.com/maple52046/swallow/internal/core/workflow/domain"
)

// fakeAnsibleRunner runs a fake playbook: it records the input, reports one streamed event, and
// either returns result data, fails, or waits until canceled.
type fakeAnsibleRunner struct {
	mu      sync.Mutex
	input   workflowdomain.RunnerInput
	fail    error
	block   bool
	data    map[string]any
	started chan struct{}
}

func (r *fakeAnsibleRunner) Run(ctx context.Context, input workflowdomain.RunnerInput) (workflowdomain.RunnerResult, error) {
	r.mu.Lock()
	r.input = input
	r.mu.Unlock()
	if r.started != nil {
		close(r.started)
	}
	if r.block {
		<-ctx.Done()
		return workflowdomain.RunnerResult{}, ctx.Err()
	}
	return workflowdomain.RunnerResult{Data: r.data}, r.fail
}

func (r *fakeAnsibleRunner) EventsSince(string, int) (workflowdomain.RunProgressDelta, error) {
	return workflowdomain.RunProgressDelta{
		LastSeq: 1, CurrentTask: "install",
		Events: []workflowdomain.StreamedTaskEvent{{TaskEvent: workflowdomain.TaskEvent{Status: "ok", Changed: true}}},
	}, nil
}

func (r *fakeAnsibleRunner) Logs(context.Context, string) (string, error)   { return "", nil }
func (r *fakeAnsibleRunner) Stderr(context.Context, string) (string, error) { return "", nil }
func (r *fakeAnsibleRunner) Events(context.Context, string) ([]workflowdomain.TaskEvent, error) {
	return nil, nil
}

type stubAnsibleConfigurations struct {
	workflowdomain.AutomationConfigurationRepository
}

func (stubAnsibleConfigurations) FindBySiteID(context.Context, string) (*workflowdomain.AutomationConfiguration, error) {
	return &workflowdomain.AutomationConfiguration{SiteID: "site", Enabled: true, SSHUser: "ubuntu", SSHPort: 22,
		KnownHosts: "static-host ssh-ed25519 AAAA", CredentialSource: workflowdomain.CredentialSourceDeploymentKey}, nil
}

func (stubAnsibleConfigurations) Credential(context.Context, string) (workflowdomain.AutomationCredential, error) {
	return workflowdomain.AutomationCredential{}, nil
}

type stubCatalog struct{}

func (stubCatalog) Resolve(name string) (string, error) { return "/playbooks/" + name, nil }

type stubInventory struct{}

func (stubInventory) Inventory(context.Context, string) (map[string]any, error) {
	return map[string]any{"_meta": map[string]any{"hostvars": map[string]any{"s1": map[string]any{"ansible_host": "10.0.0.1"}}}}, nil
}

type stubGuard struct{ err error }

func (g stubGuard) RequireUnlocked(context.Context, []string) error { return g.err }

type stubSecrets struct {
	workflowdomain.OperationSecretRepository
}

func (stubSecrets) Resolve(context.Context, string) (any, error) { return "s3cret", nil }

type stubScanner struct{}

func (stubScanner) Scan(context.Context, []string, int) (string, error) {
	return "10.0.0.1 ssh-ed25519 BBBB", nil
}

type recordingLive struct {
	mu       sync.Mutex
	external []*workflowdomain.ExternalExecutionReference
	live     []*workflowdomain.TaskLive
	err      error
}

func (l *recordingLive) UpdateLive(_ context.Context, _ TaskRef, external *workflowdomain.ExternalExecutionReference, _ *time.Time, live *workflowdomain.TaskLive) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if external != nil {
		l.external = append(l.external, external)
	}
	if live != nil {
		l.live = append(l.live, live)
	}
	return l.err
}

type recordingEvents struct{ appended int }

func (e *recordingEvents) AppendEvents(_ context.Context, events []workflowdomain.StreamedTaskEvent) error {
	e.appended += len(events)
	return nil
}

func (e *recordingEvents) ListEvents(context.Context, string) ([]workflowdomain.TaskEvent, error) {
	return nil, nil
}

func newAnsibleTaskFixture(t *testing.T, runner *fakeAnsibleRunner, guard stubGuard) (*AnsibleTaskRunner, *recordingLive, *recordingEvents) {
	t.Helper()
	live, events := &recordingLive{}, &recordingEvents{}
	tasks, err := NewAnsibleTaskRunner(stubAnsibleConfigurations{}, stubCatalog{}, runner, events, stubInventory{}, guard,
		stubSecrets{}, stubScanner{}, live, t.TempDir())
	if err != nil {
		t.Fatalf("NewAnsibleTaskRunner: %v", err)
	}
	tasks.streamInterval = time.Millisecond
	return tasks, live, events
}

func ansibleInput() AnsibleTask {
	return AnsibleTask{WorkflowID: "w", SiteID: "site", Generation: 2, Task: workflowdomain.Task{
		ID: "install", Attempt: 3, Targets: []workflowdomain.ResourceReference{{Kind: "server", ID: "s1"}},
		Parameters: map[string]any{"playbook": "deploy-k0s", "extraVars": map[string]any{"x": 1}},
		SecretRefs: map[string]string{"password": "ref"},
	}}
}

func TestAnsibleTaskRunsThePlaybookAndReturnsItsData(t *testing.T) {
	runner := &fakeAnsibleRunner{data: map[string]any{"token": "t"}}
	tasks, live, events := newAnsibleTaskFixture(t, runner, stubGuard{})

	outcome := tasks.Run(context.Background(), ansibleInput())
	if outcome.Status != workflowdomain.TaskSucceeded || outcome.Data["token"] != "t" {
		t.Fatalf("outcome = %+v, want succeeded with the playbook's data", outcome)
	}
	runID := AnsibleRunID("w", "install", 3)
	if outcome.ExternalExecution == nil || outcome.ExternalExecution.ID != runID || len(live.external) != 1 {
		t.Fatalf("reference = %+v (published %d), want run %s published once before the run", outcome.ExternalExecution, len(live.external), runID)
	}
	if runner.input.SecretVars["password"] != "s3cret" || runner.input.PlaybookPath != "/playbooks/deploy-k0s" {
		t.Fatalf("runner input = %+v, want the resolved secret and playbook", runner.input)
	}
	if got := runner.input.Configuration.KnownHosts; got != "10.0.0.1 ssh-ed25519 BBBB\nstatic-host ssh-ed25519 AAAA\n" {
		t.Fatalf("known_hosts = %q, want the scanned key then the Site's entries", got)
	}
	if events.appended == 0 || len(live.live) == 0 || live.live[len(live.live)-1].CurrentTask != "install" {
		t.Fatalf("streamed %d events and %d live updates, want both", events.appended, len(live.live))
	}
}

func TestAnsibleTaskFailures(t *testing.T) {
	failed, _, _ := newAnsibleTaskFixture(t, &fakeAnsibleRunner{fail: errors.New("task failed")}, stubGuard{})
	if outcome := failed.Run(context.Background(), ansibleInput()); outcome.Status != workflowdomain.TaskFailed ||
		outcome.Error.Code != "ansible_failed" || !outcome.Error.Retryable {
		t.Errorf("a failing playbook = %+v, want a retryable ansible_failed", outcome)
	}
	locked, _, _ := newAnsibleTaskFixture(t, &fakeAnsibleRunner{}, stubGuard{err: errors.New("locked")})
	if outcome := locked.Run(context.Background(), ansibleInput()); outcome.Status != workflowdomain.TaskFailed || outcome.Error.Code != "server_locked" {
		t.Errorf("a locked target = %+v, want server_locked before the run", outcome)
	}
	resumed := ansibleInput()
	resumed.Resumed = true
	if outcome := failed.Run(context.Background(), resumed); outcome.Status != workflowdomain.TaskRequiresAttention ||
		outcome.Error.Code != "executor_outcome_unknown" {
		t.Errorf("a resumed attempt = %+v, want executor_outcome_unknown", outcome)
	}
}

func TestAnsibleTaskCancelStopsTheRun(t *testing.T) {
	runner := &fakeAnsibleRunner{block: true, started: make(chan struct{})}
	tasks, _, _ := newAnsibleTaskFixture(t, runner, stubGuard{})
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-runner.started
		cancel()
	}()
	if outcome := tasks.Run(ctx, ansibleInput()); outcome.Status != workflowdomain.TaskCanceled {
		t.Fatalf("a canceled run = %+v, want canceled", outcome)
	}
}

func TestNewAnsibleTaskRunnerRequiresEveryDependency(t *testing.T) {
	if _, err := NewAnsibleTaskRunner(stubAnsibleConfigurations{}, stubCatalog{}, &fakeAnsibleRunner{}, &recordingEvents{},
		stubInventory{}, nil, stubSecrets{}, stubScanner{}, &recordingLive{}, "/tmp"); err == nil {
		t.Fatal("a runner without the Server Lock guard was built")
	}
}
