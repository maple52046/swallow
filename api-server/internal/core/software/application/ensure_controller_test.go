package application

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/maple52046/swallow/internal/core/convergence"
	serverdomain "github.com/maple52046/swallow/internal/core/server/domain"
	softwaredomain "github.com/maple52046/swallow/internal/core/software/domain"
	"github.com/maple52046/swallow/internal/core/store"
	workflowdomain "github.com/maple52046/swallow/internal/core/workflow/domain"
)

const ensureKind softwaredomain.Kind = "node-exporter"

var ensureNow = time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)

type fakeInstaller struct {
	inputs []InstallInput
	err    error
}

func (f *fakeInstaller) Install(_ context.Context, input InstallInput) (string, error) {
	f.inputs = append(f.inputs, input)
	if f.err != nil {
		return "", f.err
	}
	return "wf-new", nil
}

type fakeEnsureServers struct {
	servers map[string]*serverdomain.Server
}

func (f fakeEnsureServers) FindByID(_ context.Context, id string) (*serverdomain.Server, error) {
	if server, ok := f.servers[id]; ok {
		return server, nil
	}
	return nil, serverdomain.ErrServerNotFound
}

func (f fakeEnsureServers) List(_ context.Context, filter serverdomain.ListFilter) (serverdomain.ListResult, error) {
	var result serverdomain.ListResult
	for _, server := range f.servers {
		if !server.Absent && server.Provisioning != nil && server.Provisioning.State == filter.ProvisioningState {
			result.Servers = append(result.Servers, server)
		}
	}
	return result, nil
}

type fakeEnsureAssignments struct {
	byServer map[string]*softwaredomain.Assignment
	byRecord map[string]*softwaredomain.Assignment
}

func (f fakeEnsureAssignments) FindByServerAndKind(_ context.Context, serverID string, kind softwaredomain.Kind) (*softwaredomain.Assignment, error) {
	if assignment, ok := f.byServer[serverID]; ok && assignment.Kind == kind {
		return assignment, nil
	}
	return nil, softwaredomain.ErrAssignmentNotFound
}

func (f fakeEnsureAssignments) FindByRecordKey(_ context.Context, key string) (*softwaredomain.Assignment, error) {
	if assignment, ok := f.byRecord[key]; ok {
		return assignment, nil
	}
	return nil, softwaredomain.ErrAssignmentNotFound
}

type fakeSiteAutomation struct{ enabled map[string]bool }

func (f fakeSiteAutomation) FindBySiteID(_ context.Context, siteID string) (*workflowdomain.AutomationConfiguration, error) {
	enabled, ok := f.enabled[siteID]
	if !ok {
		return nil, workflowdomain.ErrAutomationConfigNotFound
	}
	return &workflowdomain.AutomationConfiguration{SiteID: siteID, Enabled: enabled}, nil
}

// fakeEnsureWorkflows holds the Workflows targeting each Server, newest first, and records cancels.
type fakeEnsureWorkflows struct {
	workflows []*workflowdomain.Workflow
	canceled  map[string]string
}

func (f *fakeEnsureWorkflows) List(_ context.Context, filter workflowdomain.WorkflowFilter) ([]*workflowdomain.Workflow, int, error) {
	var out []*workflowdomain.Workflow
	for _, workflow := range f.workflows {
		if filter.ServerID != "" && !slices.Contains(workflow.TargetServerIDs, filter.ServerID) {
			continue
		}
		if filter.ActiveOnly && workflow.Status.Terminal() {
			continue
		}
		out = append(out, workflow)
	}
	total := len(out)
	if filter.Limit > 0 && len(out) > filter.Limit {
		out = out[:filter.Limit]
	}
	return out, total, nil
}

func (f *fakeEnsureWorkflows) CancelFor(_ context.Context, id, reason string) error {
	if f.canceled == nil {
		f.canceled = map[string]string{}
	}
	f.canceled[id] = reason
	return nil
}

// fakeScrapes answers every read with observation (or err) and records each window asked for.
type fakeScrapes struct {
	observation ScrapeObservation
	byWindow    func(time.Duration) ScrapeObservation
	err         error
	windows     []time.Duration
}

func (f *fakeScrapes) Scrapes(_ context.Context, _, _, _ string, window time.Duration) (ScrapeObservation, error) {
	f.windows = append(f.windows, window)
	if f.err != nil {
		return ScrapeUnknown, f.err
	}
	if f.byWindow != nil {
		return f.byWindow(window), nil
	}
	return f.observation, nil
}

// ensureFixture is one Server under test: covered by the policy unless a test changes it.
type ensureFixture struct {
	server      *serverdomain.Server
	assignment  *softwaredomain.Assignment
	automation  map[string]bool
	workflows   *fakeEnsureWorkflows
	installer   *fakeInstaller
	scrapes     *fakeScrapes
	noExporter  bool
	recordKey   string
	otherRecord *softwaredomain.Assignment
}

func newEnsureFixture() *ensureFixture {
	return &ensureFixture{
		server: &serverdomain.Server{
			ID: "srv-1", Source: serverdomain.Source{SiteID: "site-1"},
			Provisioning: &serverdomain.ProvisioningStatus{State: "deployed", PowerState: "on"},
		},
		automation: map[string]bool{"site-1": true},
		workflows:  &fakeEnsureWorkflows{},
		installer:  &fakeInstaller{},
		scrapes:    &fakeScrapes{observation: ScrapeUp},
	}
}

func (f *ensureFixture) controller(t *testing.T) *EnsureController {
	t.Helper()
	entry := softwaredomain.CatalogEntry{Kind: ensureKind, Label: "Node Exporter", Exporter: &softwaredomain.Exporter{Name: "node-exporter", Port: 9100}}
	if f.noExporter {
		entry.Exporter = nil
	}
	assignments := fakeEnsureAssignments{byServer: map[string]*softwaredomain.Assignment{}, byRecord: map[string]*softwaredomain.Assignment{}}
	if f.assignment != nil {
		assignments.byServer[f.server.ID] = f.assignment
		assignments.byRecord["record-1"] = f.assignment
	}
	if f.otherRecord != nil {
		assignments.byRecord["record-other"] = f.otherRecord
	}
	controller, err := NewEnsureController(EnsureControllerDeps{
		Entry: entry, Installer: f.installer,
		Servers:     fakeEnsureServers{servers: map[string]*serverdomain.Server{f.server.ID: f.server}},
		Assignments: assignments, Automation: fakeSiteAutomation{enabled: f.automation},
		Workflows: f.workflows, Canceler: f.workflows, Scrapes: f.scrapes,
	})
	if err != nil {
		t.Fatalf("NewEnsureController: %v", err)
	}
	controller.now = func() time.Time { return ensureNow }
	return controller
}

func (f *ensureFixture) reconcile(t *testing.T, controller *EnsureController, trigger convergence.Trigger) {
	t.Helper()
	if err := controller.Reconcile(context.Background(), convergence.Request{Key: f.server.ID, Trigger: trigger}); err != nil {
		t.Fatalf("Reconcile(%s) error = %v", trigger, err)
	}
}

func assignmentIn(state softwaredomain.AssignmentState, updated time.Time) *softwaredomain.Assignment {
	applied := updated
	return &softwaredomain.Assignment{
		ServerID: "srv-1", SiteID: "site-1", Kind: ensureKind, State: state,
		LastWorkflowID: "wf-last", LastAppliedAt: &applied, UpdatedAt: updated,
	}
}

func installWorkflow(id, requestedBy string, status workflowdomain.WorkflowStatus, finished time.Time) *workflowdomain.Workflow {
	workflow := &workflowdomain.Workflow{
		ID: id, Kind: workflowdomain.WorkflowKind(ensureKind.InstallWorkflowKind()), Status: status,
		TargetServerIDs: []string{"srv-1"}, RequestedBy: requestedBy, RequestedAt: finished.Add(-5 * time.Minute),
		UpdatedAt: finished,
	}
	if status.Terminal() {
		workflow.FinishedAt = &finished
	}
	return workflow
}

// A covered Server without a working Node Exporter gets one install, as the controller, through
// the same install a user's goes through.
func TestEnsureControllerInstallsWhenNotInstalled(t *testing.T) {
	for _, tc := range []struct {
		name       string
		assignment *softwaredomain.Assignment
		trigger    convergence.Trigger
	}{
		{name: "no assignment, on a change", trigger: convergence.TriggerChange},
		{name: "no assignment, at resync", trigger: convergence.TriggerResync},
		{name: "absent assignment", assignment: assignmentIn(softwaredomain.StateAbsent, ensureNow.Add(-time.Hour)), trigger: convergence.TriggerChange},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newEnsureFixture()
			f.assignment = tc.assignment
			f.reconcile(t, f.controller(t), tc.trigger)
			if len(f.installer.inputs) != 1 {
				t.Fatalf("installs = %d, want 1", len(f.installer.inputs))
			}
			input := f.installer.inputs[0]
			if input.Kind != ensureKind || input.RequestedBy != "swallow:node-exporter-controller" ||
				len(input.Targets) != 1 || input.Targets[0].ServerID != "srv-1" || len(input.Spec) != 0 {
				t.Errorf("install = %+v, want node-exporter on srv-1 by swallow:node-exporter-controller with no spec", input)
			}
		})
	}
}

// The policy covers only present, deployed, unlocked Servers of a Site whose automation is
// enabled; any other Server is left alone, and nothing is ever uninstalled.
func TestEnsureControllerLeavesUncoveredServers(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*ensureFixture)
	}{
		{name: "absent", mutate: func(f *ensureFixture) { f.server.Absent = true }},
		{name: "not deployed", mutate: func(f *ensureFixture) { f.server.Provisioning.State = "ready" }},
		{name: "no provisioning state", mutate: func(f *ensureFixture) { f.server.Provisioning = nil }},
		{name: "locked", mutate: func(f *ensureFixture) { f.server.Provisioning.Locked = true }},
		{name: "automation disabled", mutate: func(f *ensureFixture) { f.automation["site-1"] = false }},
		{name: "no automation configuration", mutate: func(f *ensureFixture) { delete(f.automation, "site-1") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newEnsureFixture()
			tc.mutate(f)
			f.reconcile(t, f.controller(t), convergence.TriggerResync)
			if len(f.installer.inputs) != 0 {
				t.Errorf("installs = %d, want none", len(f.installer.inputs))
			}
		})
	}
}

// Work in progress is not interfered with: a pending or uninstalling assignment, or any unfinished
// Workflow holding the Server, starts nothing.
func TestEnsureControllerWaitsForWorkInProgress(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*ensureFixture)
	}{
		{name: "pending", mutate: func(f *ensureFixture) {
			f.assignment = assignmentIn(softwaredomain.StatePending, ensureNow.Add(-time.Hour))
		}},
		{name: "uninstalling", mutate: func(f *ensureFixture) {
			f.assignment = assignmentIn(softwaredomain.StateUninstalling, ensureNow.Add(-time.Hour))
		}},
		{name: "held by a running Workflow", mutate: func(f *ensureFixture) {
			f.workflows.workflows = []*workflowdomain.Workflow{{
				ID: "wf-deploy", Kind: "deploy-os", Status: workflowdomain.WorkflowRunning, TargetServerIDs: []string{"srv-1"}, RequestedBy: "admin",
			}}
		}},
		{name: "held by someone's parked Workflow", mutate: func(f *ensureFixture) {
			f.assignment = assignmentIn(softwaredomain.StateFailed, ensureNow.Add(-time.Hour))
			f.workflows.workflows = []*workflowdomain.Workflow{{
				ID: "wf-deploy", Kind: "deploy-os", Status: workflowdomain.WorkflowRequiresAttention, TargetServerIDs: []string{"srv-1"}, RequestedBy: "admin",
			}}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newEnsureFixture()
			tc.mutate(f)
			f.reconcile(t, f.controller(t), convergence.TriggerResync)
			if len(f.installer.inputs) != 0 {
				t.Errorf("installs = %d, want none", len(f.installer.inputs))
			}
			if len(f.workflows.canceled) != 0 {
				t.Errorf("canceled = %v, want none", f.workflows.canceled)
			}
		})
	}
}

// The controller's own install that stopped for attention, or whose install Task already failed,
// is canceled so the Server is released; nothing new starts while it still holds the Server.
func TestEnsureControllerCancelsItsOwnParkedInstall(t *testing.T) {
	for _, tc := range []struct {
		name       string
		assignment *softwaredomain.Assignment
		status     workflowdomain.WorkflowStatus
	}{
		{name: "parked with the assignment still pending", assignment: assignmentIn(softwaredomain.StatePending, ensureNow), status: workflowdomain.WorkflowRequiresAttention},
		{name: "parked with the assignment failed", assignment: assignmentIn(softwaredomain.StateFailed, ensureNow), status: workflowdomain.WorkflowRequiresAttention},
		{name: "install failed, not parked yet", assignment: assignmentIn(softwaredomain.StateFailed, ensureNow), status: workflowdomain.WorkflowWaitingExternal},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newEnsureFixture()
			f.assignment = tc.assignment
			f.assignment.LastWorkflowID = "wf-own"
			f.workflows.workflows = []*workflowdomain.Workflow{installWorkflow("wf-own", EnsureRequester(ensureKind), tc.status, ensureNow)}
			f.reconcile(t, f.controller(t), convergence.TriggerChange)
			want := "The Node Exporter install failed; swallow retries it later."
			if got := f.workflows.canceled["wf-own"]; got != want {
				t.Errorf("cancel reason = %q, want %q", got, want)
			}
			if len(f.installer.inputs) != 0 {
				t.Errorf("installs = %d, want none while the canceled install holds the Server", len(f.installer.inputs))
			}
		})
	}
}

// A Workflow the controller did not start is never canceled, even an install of the same kind
// that stopped for attention, and one of its own still running or already being canceled is left
// to finish.
func TestEnsureControllerNeverCancelsOtherWorkflows(t *testing.T) {
	canceling := installWorkflow("wf-own-canceling", EnsureRequester(ensureKind), workflowdomain.WorkflowRequiresAttention, ensureNow)
	canceling.Commands.Cancel = &workflowdomain.CancelCommand{Reason: "earlier"}
	for _, tc := range []struct {
		name     string
		workflow *workflowdomain.Workflow
	}{
		{name: "a user's parked install", workflow: installWorkflow("wf-user", "admin", workflowdomain.WorkflowRequiresAttention, ensureNow)},
		{name: "a parked Workflow of another kind", workflow: &workflowdomain.Workflow{
			ID: "wf-other", Kind: "configure-docker-ce", Status: workflowdomain.WorkflowRequiresAttention,
			TargetServerIDs: []string{"srv-1"}, RequestedBy: EnsureRequester(ensureKind),
		}},
		{name: "its own install still running", workflow: installWorkflow("wf-own", EnsureRequester(ensureKind), workflowdomain.WorkflowWaitingExternal, ensureNow)},
		{name: "its own install already being canceled", workflow: canceling},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newEnsureFixture()
			f.assignment = assignmentIn(softwaredomain.StatePending, ensureNow)
			f.assignment.LastWorkflowID = tc.workflow.ID
			f.workflows.workflows = []*workflowdomain.Workflow{tc.workflow}
			f.reconcile(t, f.controller(t), convergence.TriggerResync)
			if len(f.workflows.canceled) != 0 {
				t.Errorf("canceled = %v, want none", f.workflows.canceled)
			}
			if len(f.installer.inputs) != 0 {
				t.Errorf("installs = %d, want none", len(f.installer.inputs))
			}
		})
	}
}

// A failed install is retried only at resync, after 15 minutes, 1 hour, 6 hours, and then every
// 24 hours of the controller's own consecutive failed installs.
func TestEnsureControllerRetriesFailedInstallWithBackoff(t *testing.T) {
	failedOwn := func(n int) []*workflowdomain.Workflow {
		var workflows []*workflowdomain.Workflow
		for i := range n {
			workflows = append(workflows, installWorkflow("wf-own-"+string(rune('a'+i)), EnsureRequester(ensureKind),
				workflowdomain.WorkflowCanceled, ensureNow.Add(-time.Duration(i)*time.Minute)))
		}
		return workflows
	}
	for _, tc := range []struct {
		name        string
		failedAgo   time.Duration
		history     []*workflowdomain.Workflow
		trigger     convergence.Trigger
		wantInstall bool
	}{
		{name: "change trigger never retries", failedAgo: 2 * time.Hour, trigger: convergence.TriggerChange},
		{name: "someone else's failure, within 15 minutes", failedAgo: 10 * time.Minute, trigger: convergence.TriggerResync},
		{name: "someone else's failure, after 15 minutes", failedAgo: 16 * time.Minute, trigger: convergence.TriggerResync, wantInstall: true},
		{name: "one own failure, after 15 minutes", failedAgo: 16 * time.Minute, history: failedOwn(1), trigger: convergence.TriggerResync, wantInstall: true},
		{name: "two own failures, within an hour", failedAgo: 30 * time.Minute, history: failedOwn(2), trigger: convergence.TriggerResync},
		{name: "two own failures, after an hour", failedAgo: 61 * time.Minute, history: failedOwn(2), trigger: convergence.TriggerResync, wantInstall: true},
		{name: "three own failures, within 6 hours", failedAgo: 5 * time.Hour, history: failedOwn(3), trigger: convergence.TriggerResync},
		{name: "three own failures, after 6 hours", failedAgo: 6*time.Hour + time.Minute, history: failedOwn(3), trigger: convergence.TriggerResync, wantInstall: true},
		{name: "four own failures, within a day", failedAgo: 23 * time.Hour, history: failedOwn(4), trigger: convergence.TriggerResync},
		{name: "four own failures, after a day", failedAgo: 24*time.Hour + time.Minute, history: failedOwn(4), trigger: convergence.TriggerResync, wantInstall: true},
		{name: "a user's install ends the run", failedAgo: 16 * time.Minute, trigger: convergence.TriggerResync, wantInstall: true,
			history: append([]*workflowdomain.Workflow{installWorkflow("wf-user", "admin", workflowdomain.WorkflowFailed, ensureNow)}, failedOwn(3)...)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newEnsureFixture()
			f.assignment = assignmentIn(softwaredomain.StateFailed, ensureNow.Add(-tc.failedAgo))
			f.workflows.workflows = tc.history
			f.reconcile(t, f.controller(t), tc.trigger)
			if got := len(f.installer.inputs) == 1; got != tc.wantInstall {
				t.Errorf("installed = %v, want %v", got, tc.wantInstall)
			}
		})
	}
}

// An installed Node Exporter is reinstalled only at resync, when every scrape of it failed for the
// whole grace period on a host not reported off; anything the metrics store cannot say starts
// nothing.
func TestEnsureControllerReinstallsWhenExporterDown(t *testing.T) {
	for _, tc := range []struct {
		name         string
		mutate       func(*ensureFixture)
		trigger      convergence.Trigger
		wantInstall  bool
		wantNoScrape bool
	}{
		{name: "down for the grace period", trigger: convergence.TriggerResync, wantInstall: true,
			mutate: func(f *ensureFixture) { f.scrapes.observation = ScrapeDown }},
		{name: "up", trigger: convergence.TriggerResync,
			mutate: func(f *ensureFixture) { f.scrapes.observation = ScrapeUp }},
		{name: "unknown", trigger: convergence.TriggerResync,
			mutate: func(f *ensureFixture) { f.scrapes.observation = ScrapeUnknown }},
		{name: "metrics read fails", trigger: convergence.TriggerResync,
			mutate: func(f *ensureFixture) { f.scrapes.err = errors.New("prometheus unreachable") }},
		{name: "change trigger reads no metrics", trigger: convergence.TriggerChange, wantNoScrape: true,
			mutate: func(f *ensureFixture) { f.scrapes.observation = ScrapeDown }},
		{name: "installed for less than the grace period", trigger: convergence.TriggerResync, wantNoScrape: true,
			mutate: func(f *ensureFixture) {
				f.scrapes.observation = ScrapeDown
				f.assignment = assignmentIn(softwaredomain.StateInstalled, ensureNow.Add(-5*time.Minute))
			}},
		{name: "host powered off", trigger: convergence.TriggerResync, wantNoScrape: true,
			mutate: func(f *ensureFixture) {
				f.scrapes.observation = ScrapeDown
				f.server.Provisioning.PowerState = "off"
			}},
		{name: "kind without an exporter", trigger: convergence.TriggerResync, wantNoScrape: true,
			mutate: func(f *ensureFixture) {
				f.scrapes.observation = ScrapeDown
				f.noExporter = true
			}},
		{name: "held by a Workflow", trigger: convergence.TriggerResync,
			mutate: func(f *ensureFixture) {
				f.scrapes.observation = ScrapeDown
				f.workflows.workflows = []*workflowdomain.Workflow{{
					ID: "wf-platform", Kind: "deploy-slurm", Status: workflowdomain.WorkflowRunning, TargetServerIDs: []string{"srv-1"}, RequestedBy: "admin",
				}}
			}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newEnsureFixture()
			f.assignment = assignmentIn(softwaredomain.StateInstalled, ensureNow.Add(-time.Hour))
			tc.mutate(f)
			f.reconcile(t, f.controller(t), tc.trigger)
			if got := len(f.installer.inputs) == 1; got != tc.wantInstall {
				t.Errorf("installed = %v, want %v", got, tc.wantInstall)
			}
			if tc.wantNoScrape && len(f.scrapes.windows) != 0 {
				t.Errorf("scrape reads = %v, want none", f.scrapes.windows)
			}
			if tc.wantInstall && (len(f.scrapes.windows) == 0 || f.scrapes.windows[0] != ensureDownGrace) {
				t.Errorf("scrape windows = %v, want the first over the %s grace period", f.scrapes.windows, ensureDownGrace)
			}
		})
	}
}

// A reinstall that did not bring the exporter back counts toward the backoff; one after which it
// worked again does not, so a new outage is corrected after the grace period alone.
func TestEnsureControllerBacksOffReinstallsThatDidNotHelp(t *testing.T) {
	for _, tc := range []struct {
		name        string
		finishedAgo time.Duration
		upSince     bool
		wantInstall bool
	}{
		{name: "still down, within 15 minutes of the reinstall", finishedAgo: 12 * time.Minute},
		{name: "still down, after 15 minutes", finishedAgo: 16 * time.Minute, wantInstall: true},
		{name: "worked after the reinstall", finishedAgo: 12 * time.Minute, upSince: true, wantInstall: true},
		{name: "reinstall beyond the run horizon", finishedAgo: 3 * 24 * time.Hour, wantInstall: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newEnsureFixture()
			finished := ensureNow.Add(-tc.finishedAgo)
			f.assignment = assignmentIn(softwaredomain.StateInstalled, finished)
			f.assignment.LastWorkflowID = "wf-own"
			f.workflows.workflows = []*workflowdomain.Workflow{installWorkflow("wf-own", EnsureRequester(ensureKind), workflowdomain.WorkflowSucceeded, finished)}
			f.scrapes.byWindow = func(window time.Duration) ScrapeObservation {
				if window > ensureDownGrace && tc.upSince {
					return ScrapeUp
				}
				return ScrapeDown
			}
			f.reconcile(t, f.controller(t), convergence.TriggerResync)
			if got := len(f.installer.inputs) == 1; got != tc.wantInstall {
				t.Errorf("installed = %v, want %v", got, tc.wantInstall)
			}
		})
	}
}

// A refused acceptance is retried at the next resync, not on every rewrite of the Server.
func TestEnsureControllerPausesAfterRefusedInstall(t *testing.T) {
	f := newEnsureFixture()
	f.installer.err = workflowdomain.ErrAutomationCredentialMissing
	controller := f.controller(t)
	f.reconcile(t, controller, convergence.TriggerChange)
	f.reconcile(t, controller, convergence.TriggerChange)
	if len(f.installer.inputs) != 1 {
		t.Errorf("installs after two changes = %d, want 1", len(f.installer.inputs))
	}
	f.reconcile(t, controller, convergence.TriggerResync)
	if len(f.installer.inputs) != 2 {
		t.Errorf("installs after the resync = %d, want 2", len(f.installer.inputs))
	}
}

// Changes wake the controller for the Server they concern: the Server itself, or the Server of an
// assignment of its kind.
func TestEnsureControllerKeys(t *testing.T) {
	f := newEnsureFixture()
	f.assignment = assignmentIn(softwaredomain.StateFailed, ensureNow)
	f.otherRecord = &softwaredomain.Assignment{ServerID: "srv-2", Kind: "docker-ce", State: softwaredomain.StateFailed}
	controller := f.controller(t)
	ctx := context.Background()
	for _, tc := range []struct {
		name   string
		change store.Change
		want   []string
	}{
		{name: "server change", change: store.Change{Collection: "servers", Key: "srv-1", Operation: store.ChangeUpdate}, want: []string{"srv-1"}},
		{name: "server deleted", change: store.Change{Collection: "servers", Key: "srv-1", Operation: store.ChangeDelete}},
		{name: "assignment of the kind", change: store.Change{Collection: "software_assignments", Key: "record-1", Operation: store.ChangeUpdate}, want: []string{"srv-1"}},
		{name: "assignment of another kind", change: store.Change{Collection: "software_assignments", Key: "record-other", Operation: store.ChangeUpdate}},
		{name: "unknown assignment record", change: store.Change{Collection: "software_assignments", Key: "record-gone", Operation: store.ChangeUpdate}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := controller.KeysForChange(ctx, tc.change)
			if err != nil || !slices.Equal(got, tc.want) {
				t.Errorf("KeysForChange(%+v) = %v, %v, want %v", tc.change, got, err, tc.want)
			}
		})
	}
	keys, err := controller.Keys(ctx)
	if err != nil || !slices.Equal(keys, []string{"srv-1"}) {
		t.Errorf("Keys() = %v, %v, want [srv-1]", keys, err)
	}
	if controller.Name() != "node-exporter" {
		t.Errorf("Name() = %q, want node-exporter", controller.Name())
	}
}

// Every dependency is required.
func TestNewEnsureControllerRefusesMissingDependencies(t *testing.T) {
	if _, err := NewEnsureController(EnsureControllerDeps{Entry: softwaredomain.CatalogEntry{Kind: ensureKind}}); err == nil {
		t.Error("NewEnsureController(no dependencies) error = nil, want an error")
	}
}
