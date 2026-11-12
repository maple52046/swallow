package application

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/maple52046/swallow/internal/core/convergence"
	validationdomain "github.com/maple52046/swallow/internal/core/validation/domain"
)

// fakeDeployments answers with the keys it holds.
type fakeDeployments struct {
	platforms map[string]string
	servers   string
}

func (f *fakeDeployments) PlatformDeployment(_ context.Context, id string) (string, error) {
	return f.platforms[id], nil
}

func (f *fakeDeployments) ServersDeployment(context.Context, []string) (string, error) {
	return f.servers, nil
}

type scheduleFixture struct {
	serviceFixture
	controller  *ScheduleController
	deployments *fakeDeployments
	clock       time.Time
}

func newScheduleFixture(t *testing.T) *scheduleFixture {
	t.Helper()
	f := &scheduleFixture{serviceFixture: newServiceFixture(t), deployments: &fakeDeployments{platforms: map[string]string{}},
		clock: time.Date(2026, 11, 12, 9, 30, 0, 0, time.UTC)}
	controller, err := NewScheduleController(f.service, f.deployments)
	if err != nil {
		t.Fatal(err)
	}
	controller.now = func() time.Time { return f.clock }
	f.controller = controller
	return f
}

func (f *scheduleFixture) create(t *testing.T, spec validationdomain.ScheduleSpec) string {
	t.Helper()
	schedule, err := f.service.CreateSchedule(context.Background(), ScheduleWrite{Spec: spec})
	if err != nil {
		t.Fatalf("CreateSchedule() error = %v", err)
	}
	return schedule.ID
}

func (f *scheduleFixture) reconcile(t *testing.T, id string) validationdomain.ScheduleStatus {
	t.Helper()
	if err := f.controller.Reconcile(context.Background(), convergence.Request{Key: id}); err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	schedule, err := f.schedules.Get(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return schedule.Status
}

// endRun completes the run so the schedule may start the next one.
func (f *scheduleFixture) endRun(t *testing.T, id string) {
	t.Helper()
	run, err := f.runs.Get(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	run.Status = validationdomain.StatusCompleted
	if err := f.runs.Save(context.Background(), run); err != nil {
		t.Fatal(err)
	}
}

func realized(status validationdomain.ScheduleStatus) (bool, string) {
	condition, _ := convergence.FindCondition(status.Conditions, convergence.ConditionRealized)
	return condition.Status == convergence.ConditionTrue, condition.Reason
}

func hourly() validationdomain.ScheduleSpec {
	return validationdomain.ScheduleSpec{Name: "hourly", SuiteID: "host-baseline", ServerIDs: []string{"s-1"}, Enabled: true,
		Trigger: validationdomain.ScheduleTrigger{Kind: validationdomain.ScheduleCron, Cron: "0 * * * *"}}
}

// A cron schedule first only computes its next time; when it comes due it starts one run as the
// schedule and moves on, a time missed while the worker was down runs once, and an occurrence while
// the previous run is unfinished is skipped.
func TestScheduleControllerRunsCronOccurrences(t *testing.T) {
	f := newScheduleFixture(t)
	id := f.create(t, hourly())

	status := f.reconcile(t, id)
	if ok, _ := realized(status); !ok || status.NextRunAt == nil || !status.NextRunAt.Equal(time.Date(2026, 11, 12, 10, 0, 0, 0, time.UTC)) ||
		status.LastRunID != "" || status.ObservedGeneration != 1 {
		t.Fatalf("first pass status = %+v, want realized with 10:00 next and no run", status)
	}

	f.clock = time.Date(2026, 11, 12, 10, 0, 30, 0, time.UTC)
	status = f.reconcile(t, id)
	run, err := f.runs.Get(context.Background(), status.LastRunID)
	if err != nil || run.Trigger.Kind != validationdomain.TriggerSchedule || run.Trigger.ScheduleID != id || run.RequestedBy != scheduleRequester {
		t.Fatalf("due pass started run %+v (%v), want one triggered by the schedule", run, err)
	}
	if !status.NextRunAt.Equal(time.Date(2026, 11, 12, 11, 0, 0, 0, time.UTC)) || status.LastSkipReason != "" {
		t.Errorf("after the run: next %v skip %q", status.NextRunAt, status.LastSkipReason)
	}
	if again := f.reconcile(t, id); again.LastRunID != run.ID || len(f.launcher.persisted) != 1 {
		t.Errorf("a second pass at the same time started another run (%d)", len(f.launcher.persisted))
	}

	// The worker was down from 10:00 until 13:20: one run, and the next time after now.
	f.endRun(t, run.ID)
	f.clock = time.Date(2026, 11, 12, 13, 20, 0, 0, time.UTC)
	status = f.reconcile(t, id)
	if len(f.launcher.persisted) != 2 || !status.NextRunAt.Equal(time.Date(2026, 11, 12, 14, 0, 0, 0, time.UTC)) {
		t.Errorf("missed times: %d runs, next %v; want one more run and 14:00", len(f.launcher.persisted), status.NextRunAt)
	}

	// That run has not ended at 14:00, so the 14:00 occurrence is skipped.
	f.clock = time.Date(2026, 11, 12, 14, 0, 5, 0, time.UTC)
	status = f.reconcile(t, id)
	if len(f.launcher.persisted) != 2 || status.LastSkipReason == "" || !status.NextRunAt.Equal(time.Date(2026, 11, 12, 15, 0, 0, 0, time.UTC)) {
		t.Errorf("unfinished previous run: %d runs, skip %q, next %v", len(f.launcher.persisted), status.LastSkipReason, status.NextRunAt)
	}
}

// A refused run (a busy target) skips its occurrence with the reason; any other failure is returned
// so the controller retries it, leaving the status unwritten.
func TestScheduleControllerSkipsRefusedRuns(t *testing.T) {
	f := newScheduleFixture(t)
	id := f.create(t, hourly())
	f.reconcile(t, id)
	f.clock = time.Date(2026, 11, 12, 10, 0, 0, 0, time.UTC)

	f.launcher.persist = errors.New("store unavailable")
	if err := f.controller.Reconcile(context.Background(), convergence.Request{Key: id}); err == nil {
		t.Fatal("a transient failure was not returned")
	}
	if schedule, _ := f.schedules.Get(context.Background(), id); !schedule.Status.NextRunAt.Equal(f.clock) {
		t.Errorf("a failed occurrence moved the next time to %v", schedule.Status.NextRunAt)
	}

	f.launcher.persist = fmt.Errorf("%w: a target is busy", validationdomain.ErrRunRefused)
	status := f.reconcile(t, id)
	if status.LastRunID != "" || status.LastSkipReason == "" || !status.NextRunAt.After(f.clock) {
		t.Errorf("refused occurrence status = %+v, want skipped with its reason and the next time", status)
	}

	// A target that is not deployed is refused the same way.
	notDeployed := hourly()
	notDeployed.Name, notDeployed.ServerIDs = "ready", []string{"ready"}
	other := f.create(t, notDeployed)
	f.clock = time.Date(2026, 11, 12, 9, 0, 0, 0, time.UTC)
	f.reconcile(t, other)
	f.clock = time.Date(2026, 11, 12, 10, 0, 0, 0, time.UTC)
	f.launcher.persist = nil
	if status := f.reconcile(t, other); status.LastSkipReason == "" || status.LastRunID != "" {
		t.Errorf("not deployed target: status %+v, want the occurrence skipped", status)
	}
}

// A disabled schedule, one whose suite was retired, and one whose target is gone are not realized
// and start nothing.
func TestScheduleControllerReportsWhyItCannotRun(t *testing.T) {
	f := newScheduleFixture(t)
	ctx := context.Background()
	disabled := hourly()
	disabled.Enabled = false
	if status := f.reconcile(t, f.create(t, disabled)); status.NextRunAt != nil {
		t.Errorf("disabled schedule has next time %v", status.NextRunAt)
	} else if ok, reason := realized(status); ok || reason != ReasonScheduleDisabled {
		t.Errorf("disabled schedule realized %v reason %q", ok, reason)
	}

	suite, err := f.service.CreateSuite(ctx, validationdomain.UserSuiteInput{Name: "mounts", Script: "exit 0"}, "admin")
	if err != nil {
		t.Fatal(err)
	}
	spec := hourly()
	spec.Name, spec.SuiteID = "user", suite.ID
	id := f.create(t, spec)
	retired, _ := f.users.Get(ctx, suite.ID)
	retired.Retired = true
	if err := f.users.Replace(ctx, retired); err != nil {
		t.Fatal(err)
	}
	if ok, reason := realized(f.reconcile(t, id)); ok || reason != ReasonSuiteMissing {
		t.Errorf("retired suite: realized %v reason %q", ok, reason)
	}

	gone := hourly()
	gone.Name = "gone"
	goneID := f.create(t, gone)
	schedule, _ := f.schedules.Get(ctx, goneID)
	schedule.Spec.ServerIDs = []string{"deleted"}
	if _, err := f.schedules.WriteSpec(ctx, goneID, 0, schedule.Spec, schedule.SiteID, f.clock); err != nil {
		t.Fatal(err)
	}
	if ok, reason := realized(f.reconcile(t, goneID)); ok || reason != ReasonTargetMissing || len(f.launcher.persisted) != 0 {
		t.Errorf("deleted target: realized %v reason %q, %d runs", ok, reason, len(f.launcher.persisted))
	}
}

// An after-deployment schedule remembers the deployment it first sees without running, starts one
// run for each new deployment, and none for one it already handled.
func TestScheduleControllerRunsAfterDeployments(t *testing.T) {
	f := newScheduleFixture(t)
	f.deployments.platforms["p"] = "wf-deploy-1"
	id := f.create(t, validationdomain.ScheduleSpec{Name: "after", SuiteID: "slurm-acceptance", PlatformID: "p", Enabled: true,
		Trigger: validationdomain.ScheduleTrigger{Kind: validationdomain.ScheduleAfterDeployment}})

	status := f.reconcile(t, id)
	if status.LastDeploymentKey != "wf-deploy-1" || status.LastRunID != "" || status.NextRunAt != nil {
		t.Fatalf("first pass status = %+v, want the deployment remembered and no run", status)
	}
	if status = f.reconcile(t, id); status.LastRunID != "" {
		t.Errorf("the same deployment started a run")
	}
	f.deployments.platforms["p"] = "wf-deploy-2"
	status = f.reconcile(t, id)
	if status.LastRunID == "" || status.LastDeploymentKey != "wf-deploy-2" {
		t.Fatalf("new deployment status = %+v, want a run", status)
	}
	f.endRun(t, status.LastRunID)
	if status = f.reconcile(t, id); len(f.launcher.persisted) != 1 {
		t.Errorf("a handled deployment started another run (%d)", len(f.launcher.persisted))
	}
}
