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
	starters  fakeStarters
}

// The fixture's Users: an admin; bob, an owner of the Pool of s-1, s-2, claimed, and Platform p;
// alice and carol, members of it. Each one's account on s-1 and s-2 is set per test through
// f.starters; by default every User has an account with Host Access and the directory client on
// both, sudo only for bob and the admin, passwordless only for the admin.
var (
	admin = Actor{UserID: "u-admin", Username: "root-admin", Admin: true}
	bob   = operator("u-bob", "bob", "s-1", "s-2", "claimed", "p", "login", "c-1")
	alice = operator("u-alice", "alice")
	carol = operator("u-carol", "carol")
)

func defaultAccounts() map[string]StarterAccount {
	host := StarterHost{Login: true, DirectoryClient: true}
	sudoer := StarterHost{Login: true, Sudo: true, DirectoryClient: true}
	account := func(username string, uid int, h StarterHost) StarterAccount {
		return StarterAccount{DirectoryConfigured: true, Username: username, UIDNumber: uid, Hosts: map[string]StarterHost{"s-1": h, "s-2": h}}
	}
	nopasswd := sudoer
	nopasswd.Passwordless = true
	return map[string]StarterAccount{
		"u-admin": account("root-admin", 10000, nopasswd),
		"u-bob":   account("bob", 10006, sudoer),
		"u-alice": account("alice", 10005, host),
		"u-carol": account("carol", 10007, host),
	}
}

func newServiceFixture(t *testing.T) serviceFixture {
	t.Helper()
	f := serviceFixture{runs: validationtest.NewRuns(), users: validationtest.NewSuites(), schedules: validationtest.NewSchedules(),
		launcher: &fakeLauncher{}, canceler: &fakeCanceler{}, starters: fakeStarters{accounts: defaultAccounts()}}
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
		Schedules: f.schedules, Targets: targets, Starters: f.starters, Launcher: f.launcher, Canceler: f.canceler})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	f.service = service
	return f
}

// createUserSuite stores a user suite written by author.
func (f serviceFixture) createUserSuite(t *testing.T, input validationdomain.UserSuiteInput, author Actor) validationdomain.Suite {
	t.Helper()
	if input.Name == "" {
		input.Name = "mounts"
	}
	if input.Script == "" {
		input.Script = "#!/bin/sh\nexit 0\n"
	}
	suite, err := f.service.CreateSuite(context.Background(), input, validationdomain.Author{UserID: author.UserID, Username: author.Username})
	if err != nil {
		t.Fatalf("CreateSuite() error = %v", err)
	}
	return suite
}

// A Servers run is accepted as pending with its Workflow, over deployed Servers of one Site, and
// records who started it.
func TestStartServersRun(t *testing.T) {
	f := newServiceFixture(t)
	run, err := f.service.Start(context.Background(), StartInput{SuiteID: "host-baseline", ServerIDs: []string{"s-1", "claimed"}, Starter: &bob})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if run.Status != validationdomain.StatusPending || run.Mode != validationdomain.ModeReport || run.Trigger.Kind != validationdomain.TriggerManual ||
		run.SiteID != "site-a" || run.WorkflowID == "" || len(f.launcher.persisted) != 1 || run.ExpiresAt.Sub(run.RequestedAt) != DefaultRunRetention ||
		run.RequestedBy != "bob" || run.RequestedByID != "u-bob" || run.RunAs != nil {
		t.Errorf("Start() = %+v", run)
	}
	if _, err := f.runs.Get(context.Background(), run.ID); err != nil {
		t.Errorf("the run was not stored: %v", err)
	}
}

// A Platform run enters through the module's access Servers and judges the members.
func TestStartPlatformRun(t *testing.T) {
	f := newServiceFixture(t)
	run, err := f.service.Start(context.Background(), StartInput{SuiteID: "slurm-acceptance", PlatformID: "p", Starter: &bob})
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
		{name: "a revision for a module suite", input: StartInput{SuiteID: "host-baseline", ServerIDs: []string{"s-1"}, SuiteRevision: 1}, want: validationdomain.ErrInvalidRequest},
		{name: "a member starts a module suite", input: StartInput{SuiteID: "host-baseline", ServerIDs: []string{"s-1"}, Starter: &alice}, want: validationdomain.ErrRunForbidden},
		{name: "a member starts a Platform suite", input: StartInput{SuiteID: "slurm-acceptance", PlatformID: "p", Starter: &alice}, want: validationdomain.ErrRunForbidden},
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

// A user suite is started at its starter level: owner-level by owners and admins only,
// member-level by members too.
func TestStartUserSuiteAtItsStarterLevel(t *testing.T) {
	f := newServiceFixture(t)
	owners := f.createUserSuite(t, validationdomain.UserSuiteInput{Name: "owners"}, alice)
	members := f.createUserSuite(t, validationdomain.UserSuiteInput{Name: "members", StarterLevel: validationdomain.StarterMember}, bob)
	for _, tc := range []struct {
		name    string
		suite   validationdomain.Suite
		starter Actor
		want    error
	}{
		{name: "a member, owner level (even its author)", suite: owners, starter: alice, want: validationdomain.ErrRunForbidden},
		{name: "an owner, owner level", suite: owners, starter: bob},
		{name: "an admin, owner level", suite: owners, starter: admin},
		{name: "a member, member level", suite: members, starter: alice},
		{name: "another member, member level", suite: members, starter: carol},
	} {
		starter := tc.starter
		_, err := f.service.Start(context.Background(), StartInput{SuiteID: tc.suite.ID, ServerIDs: []string{"s-1"}, Starter: &starter})
		if !errors.Is(err, tc.want) {
			t.Errorf("%s: Start() error = %v, want %v", tc.name, err, tc.want)
		}
	}
}

// A user suite runs as its starter's own account: acceptance refuses what that account cannot do on
// a target (403 for access the starter lacks, 409 for a target that cannot run it), and an accepted
// run records the account and hands it to the launcher.
func TestStartUserSuiteAsTheStarter(t *testing.T) {
	plain := validationdomain.UserSuiteInput{Name: "plain", StarterLevel: validationdomain.StarterMember}
	sudo := validationdomain.UserSuiteInput{Name: "sudo", StarterLevel: validationdomain.StarterMember, Sudo: true}
	for _, tc := range []struct {
		name    string
		input   validationdomain.UserSuiteInput
		starter Actor
		account func(*StarterAccount)
		want    error
	}{
		{name: "no Directory", input: plain, starter: alice, account: func(a *StarterAccount) { a.DirectoryConfigured = false },
			want: validationdomain.ErrTargetNotRunnable},
		{name: "no uidNumber", input: plain, starter: alice, account: func(a *StarterAccount) { a.UIDNumber = 0 },
			want: validationdomain.ErrTargetNotRunnable},
		{name: "no Host Access on one target", input: plain, starter: alice,
			account: func(a *StarterAccount) { a.Hosts["s-2"] = StarterHost{DirectoryClient: true} }, want: validationdomain.ErrRunForbidden},
		{name: "no directory client on one target", input: plain, starter: alice,
			account: func(a *StarterAccount) { a.Hosts["s-2"] = StarterHost{Login: true} }, want: validationdomain.ErrTargetNotRunnable},
		{name: "sudo without sudo", input: sudo, starter: alice, want: validationdomain.ErrRunForbidden},
		{name: "sudo that needs a password", input: sudo, starter: bob, want: validationdomain.ErrTargetNotRunnable},
		{name: "sudo, passwordless", input: sudo, starter: admin},
		{name: "no sudo asked", input: plain, starter: alice},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newServiceFixture(t)
			if tc.account != nil {
				account := f.starters.accounts[tc.starter.UserID]
				hosts := map[string]StarterHost{}
				for id, host := range account.Hosts {
					hosts[id] = host
				}
				account.Hosts = hosts
				tc.account(&account)
				f.starters.accounts[tc.starter.UserID] = account
			}
			suite := f.createUserSuite(t, tc.input, bob)
			starter := tc.starter
			run, err := f.service.Start(context.Background(), StartInput{SuiteID: suite.ID, ServerIDs: []string{"s-1", "s-2"}, Starter: &starter})
			if !errors.Is(err, tc.want) {
				t.Fatalf("Start() error = %v, want %v", err, tc.want)
			}
			if tc.want != nil {
				if _, total, _ := f.runs.List(context.Background(), validationdomain.RunFilter{}); total > 0 || len(f.launcher.prepared) > 0 {
					t.Error("a refused run reached the launcher or the store")
				}
				return
			}
			want := f.starters.accounts[tc.starter.UserID]
			if run.RunAs == nil || run.RunAs.Username != want.Username || run.RunAs.UIDNumber != want.UIDNumber || run.RunAs.Sudo != tc.input.Sudo ||
				run.RequestedByID != tc.starter.UserID || run.Suite.Script != suite.Script {
				t.Errorf("Start() = run as %+v, requested by %q, script %q", run.RunAs, run.RequestedByID, run.Suite.Script)
			}
			if len(f.launcher.prepared) != 1 || f.launcher.prepared[0].Run.RunAs == nil {
				t.Errorf("the launcher was not given the account: %+v", f.launcher.prepared)
			}
		})
	}
	f := newServiceFixture(t)
	suite := f.createUserSuite(t, plain, bob)
	if _, err := f.service.Start(context.Background(), StartInput{SuiteID: suite.ID, ServerIDs: []string{"s-1"}}); !errors.Is(err, validationdomain.ErrInvalidRequest) {
		t.Errorf("Start(user suite without a starter) error = %v, want ErrInvalidRequest", err)
	}
}

// A run pinned to a revision starts only while the suite is at it; after the author changes the
// suite, the pin is a conflict and the new revision starts.
func TestStartPinsTheRevision(t *testing.T) {
	f := newServiceFixture(t)
	ctx := context.Background()
	suite := f.createUserSuite(t, validationdomain.UserSuiteInput{StarterLevel: validationdomain.StarterMember}, bob)
	start := func(revision int) error {
		_, err := f.service.Start(ctx, StartInput{SuiteID: suite.ID, ServerIDs: []string{"s-1"}, SuiteRevision: revision, Starter: &alice})
		return err
	}
	if err := start(1); err != nil {
		t.Fatalf("Start(revision 1) error = %v", err)
	}
	if _, err := f.service.ReplaceSuite(ctx, suite.ID, SuiteReplace{Input: validationdomain.UserSuiteInput{Name: "mounts",
		Script: "#!/bin/sh\nexit 1\n", StarterLevel: validationdomain.StarterMember}}, bob); err != nil {
		t.Fatal(err)
	}
	if err := start(1); !errors.Is(err, validationdomain.ErrRevisionConflict) {
		t.Errorf("Start(stale revision) error = %v, want ErrRevisionConflict", err)
	}
	if err := start(2); err != nil {
		t.Errorf("Start(revision 2) error = %v", err)
	}
	if err := start(0); err != nil {
		t.Errorf("Start(no pin) error = %v", err)
	}
}

// A run's starter, an owner of its targets, and an admin cancel it; another member who reads it
// cannot; only an active report run is canceled.
func TestCancelRun(t *testing.T) {
	f := newServiceFixture(t)
	ctx := context.Background()
	suite := f.createUserSuite(t, validationdomain.UserSuiteInput{StarterLevel: validationdomain.StarterMember}, bob)
	start := func() validationdomain.Run {
		t.Helper()
		run, err := f.service.Start(ctx, StartInput{SuiteID: suite.ID, ServerIDs: []string{"s-1"}, Starter: &alice})
		if err != nil {
			t.Fatal(err)
		}
		return run
	}
	for _, tc := range []struct {
		name  string
		actor Actor
		want  error
	}{
		{name: "another member", actor: carol, want: validationdomain.ErrRunForbidden},
		{name: "its starter", actor: alice},
		{name: "an owner", actor: bob},
		{name: "an admin", actor: admin},
	} {
		run := start()
		if _, err := f.service.Cancel(ctx, run.ID, tc.actor); !errors.Is(err, tc.want) {
			t.Errorf("Cancel(%s) error = %v, want %v", tc.name, err, tc.want)
		}
	}
	if len(f.canceler.canceled) != 3 {
		t.Errorf("canceled %v, want three Workflows", f.canceler.canceled)
	}
	gate, _, err := f.service.GateRun(GateRequest{SuiteID: "slurm-acceptance", PlatformID: "p", WorkflowID: "deploy", AccessIDs: []string{"login"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.service.RecordGateRun(ctx, gate); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.Cancel(ctx, gate.ID, admin); !errors.Is(err, validationdomain.ErrRunNotCancelable) {
		t.Errorf("Cancel(gate) error = %v, want ErrRunNotCancelable", err)
	}
}

// A user suite is created with its author and first revision, revised only by its author (an admin
// included in the refusal), each revision kept, refused as a module suite, and retired by an admin
// once no schedule uses it.
func TestUserSuiteLifecycle(t *testing.T) {
	f := newServiceFixture(t)
	ctx := context.Background()
	suite := f.createUserSuite(t, validationdomain.UserSuiteInput{}, alice)
	if suite.CreatedByID != "u-alice" || suite.CreatedBy != "alice" {
		t.Errorf("CreateSuite() author = %q (%q)", suite.CreatedByID, suite.CreatedBy)
	}
	next := validationdomain.UserSuiteInput{Name: "mounts", Script: "#!/bin/sh\nexit 1\n"}
	for _, actor := range []Actor{bob, admin} {
		if _, err := f.service.ReplaceSuite(ctx, suite.ID, SuiteReplace{Input: next}, actor); !errors.Is(err, validationdomain.ErrNotSuiteAuthor) {
			t.Errorf("ReplaceSuite(by %s) error = %v, want ErrNotSuiteAuthor", actor.Username, err)
		}
	}
	revised, err := f.service.ReplaceSuite(ctx, suite.ID, SuiteReplace{Input: next, ExpectedRevision: 1}, alice)
	if err != nil || revised.Revision != 2 {
		t.Fatalf("ReplaceSuite() = revision %d, %v", revised.Revision, err)
	}
	for revision, script := range map[int]string{1: suite.Script, 2: next.Script} {
		kept, err := f.service.SuiteRevision(ctx, suite.ID, revision)
		if err != nil || kept.Script != script {
			t.Errorf("SuiteRevision(%d) = %q, %v, want %q", revision, kept.Script, err, script)
		}
	}
	if _, err := f.service.SuiteRevision(ctx, suite.ID, 3); !errors.Is(err, validationdomain.ErrRevisionNotFound) {
		t.Errorf("SuiteRevision(3) error = %v, want ErrRevisionNotFound", err)
	}
	if _, err := f.service.SuiteRevision(ctx, "host-baseline", 1); !errors.Is(err, validationdomain.ErrRevisionNotFound) {
		t.Errorf("SuiteRevision(module) error = %v, want ErrRevisionNotFound", err)
	}
	if _, err := f.service.ReplaceSuite(ctx, "host-baseline", SuiteReplace{Input: next}, admin); !errors.Is(err, validationdomain.ErrSuiteImmutable) {
		t.Errorf("ReplaceSuite(module) error = %v, want ErrSuiteImmutable", err)
	}
	if err := f.service.RetireSuite(ctx, suite.ID, bob); !errors.Is(err, validationdomain.ErrNotSuiteAuthor) {
		t.Errorf("RetireSuite(by another user) error = %v, want ErrNotSuiteAuthor", err)
	}
	if err := f.schedules.Create(ctx, validationdomain.Schedule{ID: "sch", Spec: validationdomain.ScheduleSpec{SuiteID: suite.ID}}); err != nil {
		t.Fatal(err)
	}
	if err := f.service.RetireSuite(ctx, suite.ID, admin); !errors.Is(err, validationdomain.ErrSuiteInUse) {
		t.Errorf("RetireSuite(used) error = %v, want ErrSuiteInUse", err)
	}
	if err := f.schedules.Delete(ctx, "sch"); err != nil {
		t.Fatal(err)
	}
	if err := f.service.RetireSuite(ctx, suite.ID, admin); err != nil {
		t.Fatalf("RetireSuite(by an admin) error = %v", err)
	}
	if _, err := f.service.Start(ctx, StartInput{SuiteID: suite.ID, ServerIDs: []string{"s-1"}, Starter: &bob}); !errors.Is(err, validationdomain.ErrSuiteNotFound) {
		t.Errorf("Start(retired) error = %v, want ErrSuiteNotFound", err)
	}
	if _, err := f.service.ReplaceSuite(ctx, suite.ID, SuiteReplace{Input: next}, alice); !errors.Is(err, validationdomain.ErrSuiteImmutable) {
		t.Errorf("ReplaceSuite(retired) error = %v, want ErrSuiteImmutable", err)
	}
	if kept, err := f.service.SuiteRevision(ctx, suite.ID, 2); err != nil || kept.Revision != 2 {
		t.Errorf("SuiteRevision(retired suite) = %+v, %v", kept, err)
	}
	listed, _ := f.service.Catalog().List(ctx, false)
	if len(listed) != 4 {
		t.Errorf("List() = %d suites, want the 4 module suites only", len(listed))
	}
}

// Of two replaces that read the same revision, one wins and the other is a conflict, so a revision
// number never names two scripts; a stale precondition is a conflict too.
func TestReplaceSuiteIsConditional(t *testing.T) {
	f := newServiceFixture(t)
	ctx := context.Background()
	suite := f.createUserSuite(t, validationdomain.UserSuiteInput{}, alice)
	first := validationdomain.UserSuiteInput{Name: "mounts", Script: "#!/bin/sh\necho first\n"}
	second := validationdomain.UserSuiteInput{Name: "mounts", Script: "#!/bin/sh\necho second\n"}
	f.users.BeforeReplace = func() {
		if _, err := f.service.ReplaceSuite(ctx, suite.ID, SuiteReplace{Input: first}, alice); err != nil {
			t.Errorf("the competing ReplaceSuite() error = %v", err)
		}
	}
	if _, err := f.service.ReplaceSuite(ctx, suite.ID, SuiteReplace{Input: second}, alice); !errors.Is(err, validationdomain.ErrRevisionConflict) {
		t.Errorf("ReplaceSuite(raced) error = %v, want ErrRevisionConflict", err)
	}
	current, err := f.service.Catalog().Get(ctx, suite.ID)
	if err != nil || current.Revision != 2 || current.Script != first.Script {
		t.Errorf("the suite is at revision %d with %q (%v), want revision 2 with the winner's script", current.Revision, current.Script, err)
	}
	if kept, err := f.service.SuiteRevision(ctx, suite.ID, 2); err != nil || kept.Script != first.Script {
		t.Errorf("revision 2 = %q, %v, want the winner's script", kept.Script, err)
	}
	if _, err := f.service.ReplaceSuite(ctx, suite.ID, SuiteReplace{Input: second, ExpectedRevision: 1}, alice); !errors.Is(err, validationdomain.ErrRevisionConflict) {
		t.Errorf("ReplaceSuite(stale precondition) error = %v, want ErrRevisionConflict", err)
	}
}

// A schedule is checked against its suite and targets, refuses user suites, and is replaced at its
// generation.
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
	user := f.createUserSuite(t, validationdomain.UserSuiteInput{}, alice)
	for name, spec := range map[string]validationdomain.ScheduleSpec{
		"Platform for a Servers suite": {Name: "a", SuiteID: "host-baseline", PlatformID: "p", Trigger: validationdomain.ScheduleTrigger{Kind: validationdomain.ScheduleAfterDeployment}},
		"disruptive suite":             {Name: "a", SuiteID: "failover", ServerIDs: []string{"s-1"}, Trigger: validationdomain.ScheduleTrigger{Kind: validationdomain.ScheduleAfterDeployment}},
		"user suite":                   {Name: "a", SuiteID: user.ID, ServerIDs: []string{"s-1"}, Trigger: validationdomain.ScheduleTrigger{Kind: validationdomain.ScheduleAfterDeployment}},
	} {
		if _, err := f.service.CreateSchedule(ctx, ScheduleWrite{Spec: spec}); !errors.Is(err, validationdomain.ErrInvalidRequest) {
			t.Errorf("CreateSchedule(%s) error = %v, want ErrInvalidRequest", name, err)
		}
	}
	userWrite := write
	userWrite.ExpectedGeneration = 0
	userWrite.Spec.SuiteID = user.ID
	if _, err := f.service.ReplaceSchedule(ctx, schedule.ID, userWrite); !errors.Is(err, validationdomain.ErrInvalidRequest) {
		t.Errorf("ReplaceSchedule(user suite) error = %v, want ErrInvalidRequest", err)
	}
}
