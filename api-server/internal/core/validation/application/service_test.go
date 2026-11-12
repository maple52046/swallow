package application

import (
	"context"
	"errors"
	"testing"

	"github.com/maple52046/swallow/internal/core/store/storetest"
	validationdomain "github.com/maple52046/swallow/internal/core/validation/domain"
	"github.com/maple52046/swallow/internal/core/validation/domain/validationtest"
	workflowdomain "github.com/maple52046/swallow/internal/core/workflow/domain"
)

type serviceFixture struct {
	service   *Service
	runs      *validationtest.Runs
	users     *validationtest.Suites
	schedules *validationtest.Schedules
	launcher  *fakeLauncher
	canceler  *fakeCanceler
}

func newServiceFixture(t *testing.T) serviceFixture {
	t.Helper()
	f := serviceFixture{runs: validationtest.NewRuns(), users: validationtest.NewSuites(), schedules: validationtest.NewSchedules(),
		launcher: &fakeLauncher{}, canceler: &fakeCanceler{}}
	catalog, err := NewCatalog([]validationdomain.Suite{
		moduleSuite("host-baseline", validationdomain.TargetServers, validationdomain.ImpactObserve),
		moduleSuite("gpu-burn", validationdomain.TargetServers, validationdomain.ImpactWorkload),
		moduleSuite("failover", validationdomain.TargetServers, validationdomain.ImpactDisruptive),
		moduleSuite("slurm-acceptance", validationdomain.TargetPlatform, validationdomain.ImpactWorkload),
	}, f.users)
	if err != nil {
		t.Fatalf("NewCatalog() error = %v", err)
	}
	targets := fakeTargets{
		servers: map[string]TargetServer{
			"s-1":     {ID: "s-1", Name: "s-1", SiteID: "site-a", Deployed: true},
			"s-2":     {ID: "s-2", Name: "s-2", SiteID: "site-a", Deployed: true},
			"ready":   {ID: "ready", Name: "ready", SiteID: "site-a"},
			"claimed": {ID: "claimed", Name: "claimed", SiteID: "site-a", Deployed: true, ClaimedBy: "p"},
			"other":   {ID: "other", Name: "other", SiteID: "site-b", Deployed: true},
		},
		platforms: map[string]TargetPlatform{
			"p": {ID: "p", Name: "p", SiteID: "site-a", Type: "slurm", Active: true, AccessServerIDs: []string{"login"},
				Members: []PlatformMember{{ServerID: "login", Roles: []string{"login"}}, {ServerID: "c-1", Roles: []string{"compute"}}}},
			"k8s":     {ID: "k8s", Name: "k8s", SiteID: "site-a", Type: "kubernetes", Active: true, AccessServerIDs: []string{"cp"}},
			"planned": {ID: "planned", Name: "planned", SiteID: "site-a", Type: "slurm", AccessServerIDs: []string{"login"}},
		},
	}
	service, err := NewService(ServiceDeps{Transactor: &storetest.Transactor{}, Catalog: catalog, Users: f.users, Runs: f.runs,
		Schedules: f.schedules, Targets: targets, Launcher: f.launcher, Canceler: f.canceler})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	f.service = service
	return f
}

// A Servers run is accepted as pending with its Workflow, over deployed Servers of one Site.
func TestStartServersRun(t *testing.T) {
	f := newServiceFixture(t)
	run, err := f.service.Start(context.Background(), StartInput{SuiteID: "host-baseline", ServerIDs: []string{"s-1", "claimed"}, RequestedBy: "admin"})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if run.Status != validationdomain.StatusPending || run.Mode != validationdomain.ModeReport || run.Trigger.Kind != validationdomain.TriggerManual ||
		run.SiteID != "site-a" || run.WorkflowID == "" || len(f.launcher.persisted) != 1 || run.ExpiresAt.Sub(run.RequestedAt) != DefaultRunRetention {
		t.Errorf("Start() = %+v", run)
	}
	if _, err := f.runs.Get(context.Background(), run.ID); err != nil {
		t.Errorf("the run was not stored: %v", err)
	}
}

// A Platform run enters through the module's access Servers and judges the members.
func TestStartPlatformRun(t *testing.T) {
	f := newServiceFixture(t)
	run, err := f.service.Start(context.Background(), StartInput{SuiteID: "slurm-acceptance", PlatformID: "p"})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if !contains(run.Target.ServerIDs, "login") || !contains(run.Target.MemberIDs, "c-1") || f.launcher.prepared[0].Platform == nil {
		t.Errorf("Start() target = %+v", run.Target)
	}
}

// Acceptance refuses what validation.md §3 lists, writing nothing.
func TestStartRefuses(t *testing.T) {
	tests := []struct {
		name  string
		input StartInput
		want  error
	}{
		{name: "unknown suite", input: StartInput{SuiteID: "missing", ServerIDs: []string{"s-1"}}, want: validationdomain.ErrSuiteNotFound},
		{name: "unknown Server", input: StartInput{SuiteID: "host-baseline", ServerIDs: []string{"ghost"}}, want: validationdomain.ErrTargetNotFound},
		{name: "not deployed", input: StartInput{SuiteID: "host-baseline", ServerIDs: []string{"ready"}}, want: validationdomain.ErrTargetNotRunnable},
		{name: "workload on a claimed Server", input: StartInput{SuiteID: "gpu-burn", ServerIDs: []string{"claimed"}}, want: validationdomain.ErrTargetNotRunnable},
		{name: "two Sites", input: StartInput{SuiteID: "host-baseline", ServerIDs: []string{"s-1", "other"}}, want: validationdomain.ErrInvalidRequest},
		{name: "duplicate Server", input: StartInput{SuiteID: "host-baseline", ServerIDs: []string{"s-1", "s-1"}}, want: validationdomain.ErrInvalidRequest},
		{name: "Platform for a Servers suite", input: StartInput{SuiteID: "host-baseline", PlatformID: "p"}, want: validationdomain.ErrInvalidRequest},
		{name: "unconfirmed disruptive", input: StartInput{SuiteID: "failover", ServerIDs: []string{"s-1"}}, want: validationdomain.ErrInvalidRequest},
		{name: "parameters for a suite without", input: StartInput{SuiteID: "host-baseline", ServerIDs: []string{"s-1"}, Parameters: map[string]any{"x": 1}}, want: validationdomain.ErrInvalidRequest},
		{name: "Platform of another type", input: StartInput{SuiteID: "slurm-acceptance", PlatformID: "k8s"}, want: validationdomain.ErrTargetNotRunnable},
		{name: "Platform not active", input: StartInput{SuiteID: "slurm-acceptance", PlatformID: "planned"}, want: validationdomain.ErrTargetNotRunnable},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newServiceFixture(t)
			if _, err := f.service.Start(context.Background(), tc.input); !errors.Is(err, tc.want) {
				t.Fatalf("Start() error = %v, want %v", err, tc.want)
			}
			if _, total, _ := f.runs.List(context.Background(), validationdomain.RunFilter{}); total > 0 || len(f.launcher.persisted) > 0 {
				t.Error("a refused run wrote something")
			}
		})
	}
	f := newServiceFixture(t)
	f.launcher.persist = workflowdomain.ErrTargetsBusy
	if _, err := f.service.Start(context.Background(), StartInput{SuiteID: "host-baseline", ServerIDs: []string{"s-1"}}); !errors.Is(err, workflowdomain.ErrTargetsBusy) {
		t.Errorf("Start(busy) error = %v, want ErrTargetsBusy", err)
	}
}

// Only an active report run can be canceled, through its Workflow.
func TestCancelRun(t *testing.T) {
	f := newServiceFixture(t)
	ctx := context.Background()
	run, err := f.service.Start(ctx, StartInput{SuiteID: "host-baseline", ServerIDs: []string{"s-1"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.Cancel(ctx, run.ID, "admin"); err != nil || len(f.canceler.canceled) != 1 {
		t.Errorf("Cancel() = %v, canceled %v", err, f.canceler.canceled)
	}
	gate, _, err := f.service.GateRun(GateRequest{SuiteID: "slurm-acceptance", PlatformID: "p", WorkflowID: "deploy", AccessIDs: []string{"login"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.service.RecordGateRun(ctx, gate); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.Cancel(ctx, gate.ID, "admin"); !errors.Is(err, validationdomain.ErrRunNotCancelable) {
		t.Errorf("Cancel(gate) error = %v, want ErrRunNotCancelable", err)
	}
}

// A user suite is created, revised, refused as a module suite, and retired unless a schedule uses it.
func TestUserSuiteLifecycle(t *testing.T) {
	f := newServiceFixture(t)
	ctx := context.Background()
	suite, err := f.service.CreateSuite(ctx, validationdomain.UserSuiteInput{Name: "mounts", Script: "#!/bin/sh\nexit 0\n"}, "admin")
	if err != nil {
		t.Fatal(err)
	}
	revised, err := f.service.ReplaceSuite(ctx, suite.ID, validationdomain.UserSuiteInput{Name: "mounts", Script: "#!/bin/sh\nexit 1\n"})
	if err != nil || revised.Revision != 2 {
		t.Errorf("ReplaceSuite() = revision %d, %v", revised.Revision, err)
	}
	if _, err := f.service.ReplaceSuite(ctx, "host-baseline", validationdomain.UserSuiteInput{Name: "x", Script: "x"}); !errors.Is(err, validationdomain.ErrSuiteImmutable) {
		t.Errorf("ReplaceSuite(module) error = %v, want ErrSuiteImmutable", err)
	}
	if err := f.schedules.Create(ctx, validationdomain.Schedule{ID: "sch", Spec: validationdomain.ScheduleSpec{SuiteID: suite.ID}}); err != nil {
		t.Fatal(err)
	}
	if err := f.service.RetireSuite(ctx, suite.ID); !errors.Is(err, validationdomain.ErrSuiteInUse) {
		t.Errorf("RetireSuite(used) error = %v, want ErrSuiteInUse", err)
	}
	if err := f.schedules.Delete(ctx, "sch"); err != nil {
		t.Fatal(err)
	}
	if err := f.service.RetireSuite(ctx, suite.ID); err != nil {
		t.Fatalf("RetireSuite() error = %v", err)
	}
	if _, err := f.service.Start(ctx, StartInput{SuiteID: suite.ID, ServerIDs: []string{"s-1"}}); !errors.Is(err, validationdomain.ErrSuiteNotFound) {
		t.Errorf("Start(retired) error = %v, want ErrSuiteNotFound", err)
	}
	listed, _ := f.service.Catalog().List(ctx, false)
	if len(listed) != 4 {
		t.Errorf("List() = %d suites, want the 4 module suites only", len(listed))
	}
}

// A schedule is checked against its suite and targets, and replaced at its generation.
func TestScheduleWrites(t *testing.T) {
	f := newServiceFixture(t)
	ctx := context.Background()
	write := ScheduleWrite{Spec: validationdomain.ScheduleSpec{Name: "nightly", SuiteID: "host-baseline", ServerIDs: []string{"s-1"},
		Trigger: validationdomain.ScheduleTrigger{Kind: validationdomain.ScheduleCron, Cron: "0 2 * * *"}, Enabled: true}}
	schedule, err := f.service.CreateSchedule(ctx, write)
	if err != nil || schedule.SiteID != "site-a" || schedule.Generation != 1 {
		t.Fatalf("CreateSchedule() = %+v, %v", schedule, err)
	}
	write.ExpectedGeneration = 1
	if replaced, err := f.service.ReplaceSchedule(ctx, schedule.ID, write); err != nil || replaced.Generation != 2 {
		t.Errorf("ReplaceSchedule() = %+v, %v", replaced, err)
	}
	if _, err := f.service.ReplaceSchedule(ctx, schedule.ID, write); !errors.Is(err, validationdomain.ErrGenerationConflict) {
		t.Errorf("ReplaceSchedule(stale) error = %v, want ErrGenerationConflict", err)
	}
	for name, spec := range map[string]validationdomain.ScheduleSpec{
		"Platform for a Servers suite": {Name: "a", SuiteID: "host-baseline", PlatformID: "p", Trigger: validationdomain.ScheduleTrigger{Kind: validationdomain.ScheduleAfterDeployment}},
		"disruptive suite":             {Name: "a", SuiteID: "failover", ServerIDs: []string{"s-1"}, Trigger: validationdomain.ScheduleTrigger{Kind: validationdomain.ScheduleAfterDeployment}},
	} {
		if _, err := f.service.CreateSchedule(ctx, ScheduleWrite{Spec: spec}); !errors.Is(err, validationdomain.ErrInvalidRequest) {
			t.Errorf("CreateSchedule(%s) error = %v, want ErrInvalidRequest", name, err)
		}
	}
}
