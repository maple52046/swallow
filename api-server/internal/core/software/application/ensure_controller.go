package application

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/maple52046/swallow/internal/core/convergence"
	serverdomain "github.com/maple52046/swallow/internal/core/server/domain"
	softwaredomain "github.com/maple52046/swallow/internal/core/software/domain"
	"github.com/maple52046/swallow/internal/core/store"
	workflowdomain "github.com/maple52046/swallow/internal/core/workflow/domain"
)

// ensureDownGrace is how long every scrape of an installed kind's exporter must have failed before
// the controller installs the kind again. It is longer than a reboot or a package upgrade that
// restarts the service, so a Server that recovers by itself is never reinstalled.
const ensureDownGrace = 10 * time.Minute

// ensureRetryBackoff is how long the controller waits before installing again after the n-th
// consecutive install it started that left no working kind (it failed, was canceled, or the
// exporter was never scraped successfully after it); the last entry repeats. A Server whose install
// cannot succeed (an unsupported OS, a repository without the package, a port the Site's
// Prometheus cannot reach) therefore gets one install a day, never a loop.
var ensureRetryBackoff = []time.Duration{15 * time.Minute, time.Hour, 6 * time.Hour, 24 * time.Hour}

// ensureRunHorizon bounds how long ago an install of the controller's may have finished to still
// count toward the backoff. The backoff never waits more than a day, so consecutive installs of one
// run are less than a day and a resync apart; an older one means the controller was off or the
// Server uncovered meanwhile, and the backoff starts over. It also bounds the metrics window read.
const ensureRunHorizon = 48 * time.Hour

// ensureRefusalPause is how long a Server whose install was refused at acceptance (for example the
// Site's automation cannot log in yet) is not tried again on a change: reconcile rewrites every
// Server every pass, so without the pause each pass would repeat the refused acceptance. It only
// throttles; the periodic resync always tries again, and a restarted worker simply tries once more.
const ensureRefusalPause = 10 * time.Minute

// powerStateOff is the provisioning axis's power state of a host its provisioner reports powered
// off. Its exporter is down because the host is, which no install can change.
const powerStateOff = "off"

// EnsureRequester is the requestedBy of the Workflows the EnsureController of kind starts,
// "swallow:<kind>-controller". It is how the controller recognizes its own installs: it cancels
// only Workflows of the kind's install Workflow kind with this requester, never one a user or any
// other part of swallow started.
func EnsureRequester(kind softwaredomain.Kind) string {
	return "swallow:" + string(kind) + "-controller"
}

// SoftwareInstaller starts an install of a Managed Software kind and returns its Workflow id.
// *SoftwareService implements it with Install, the path a user's install takes, so the assignment,
// the Workflow, and its timeline are those of any install.
type SoftwareInstaller interface {
	Install(ctx context.Context, input InstallInput) (string, error)
}

// EnsureServers reads the Servers the controller converges. FindByID returns
// serverdomain.ErrServerNotFound for an unknown id.
type EnsureServers interface {
	FindByID(ctx context.Context, id string) (*serverdomain.Server, error)
	List(ctx context.Context, filter serverdomain.ListFilter) (serverdomain.ListResult, error)
}

// EnsureAssignments reads the Software Assignments the controller observes. FindByRecordKey maps
// the key a software_assignments change carries to its record. Both return
// softwaredomain.ErrAssignmentNotFound when there is none.
type EnsureAssignments interface {
	FindByServerAndKind(ctx context.Context, serverID string, kind softwaredomain.Kind) (*softwaredomain.Assignment, error)
	FindByRecordKey(ctx context.Context, key string) (*softwaredomain.Assignment, error)
}

// SiteAutomation reads a Site's Automation Configuration, or workflowdomain.ErrAutomationConfigNotFound
// for a Site that has none.
type SiteAutomation interface {
	FindBySiteID(ctx context.Context, siteID string) (*workflowdomain.AutomationConfiguration, error)
}

// WorkflowLister lists Workflows newest first (by requestedAt).
type WorkflowLister interface {
	List(ctx context.Context, filter workflowdomain.WorkflowFilter) ([]*workflowdomain.Workflow, int, error)
}

// WorkflowCanceler requests the cancel of an unfinished Workflow with the reason it records; a
// finished one is workflowdomain.ErrWorkflowControlConflict.
type WorkflowCanceler interface {
	CancelFor(ctx context.Context, id, reason string) error
}

// ScrapeObservation is what a Site's metrics store saw of one exporter on one Server over a window.
type ScrapeObservation int

const (
	// ScrapeUnknown: the Site has no metrics store, or the store did not scrape the exporter on the
	// Server in the window (no target yet, or the store itself was down). Nothing follows from it.
	ScrapeUnknown ScrapeObservation = iota
	// ScrapeDown: every scrape in the window failed.
	ScrapeDown
	// ScrapeUp: at least one scrape in the window succeeded.
	ScrapeUp
)

// ExporterScrapes reads the scrape results of one exporter on one Server from its Site's metrics
// store (monitoring.md §5.2), the observed state an installed kind is compared against. A Site
// without a metrics store is ScrapeUnknown with no error; a read that fails is an error, which the
// controller treats as unknown.
type ExporterScrapes interface {
	Scrapes(ctx context.Context, siteID, exporter, serverID string, window time.Duration) (ScrapeObservation, error)
}

// EnsureControllerDeps are the EnsureController's dependencies. Every field is required.
type EnsureControllerDeps struct {
	// Entry is the catalog entry of the kind the controller keeps installed. Its Exporter, when it
	// declares one, is what the controller observes; without one it acts on the assignment alone.
	Entry       softwaredomain.CatalogEntry
	Installer   SoftwareInstaller
	Servers     EnsureServers
	Assignments EnsureAssignments
	Automation  SiteAutomation
	Workflows   WorkflowLister
	Canceler    WorkflowCanceler
	Scrapes     ExporterScrapes
}

// EnsureController keeps one Managed Software kind installed on every Server its policy covers
// (software-deployment.md, Node Exporter). The desired state is derived, not written by anyone: a
// Server is covered by EnsureCoverage when it is present, in provisioning state `deployed` (disk or
// RAM), not locked, and its Site's automation is enabled. The observed state is the Server's
// Software Assignment of the kind and, at resync, its Site Prometheus's scrapes of the kind's
// exporter. Installing is non-destructive and the kind's playbook is an idempotent ensure, so the
// controller corrects drift itself (desired-state-model.md §6.2) and never removes the kind from a
// Server it stops covering.
//
// For a covered Server that no unfinished Workflow holds, it installs when there is no assignment
// or it is absent; again with ensureRetryBackoff when it failed; and, at resync, when it is
// installed but every exporter scrape failed for ensureDownGrace on a host not reported powered
// off. A pending or uninstalling assignment, an unknown observation, or a failed metrics read
// starts nothing. Its installs go through SoftwareInstaller under EnsureRequester; one of its own
// installs that stopped for attention, or whose install already failed, it cancels so the Server
// is released and the assignment becomes failed, which the retry backoff then covers.
//
// It is keyed by Server. A change to the Server or to an assignment of the kind wakes it; since
// reconcile rewrites every Server each pass, a change only acts on the assignment's state, and the
// metrics store is read and failed installs are retried at the periodic resync. Reconcile returns
// an error only when a store read fails, which the runtime retries.
type EnsureController struct {
	kind        softwaredomain.Kind
	label       string
	exporter    string
	requester   string
	installer   SoftwareInstaller
	servers     EnsureServers
	assignments EnsureAssignments
	coverage    EnsureCoverage
	workflows   WorkflowLister
	canceler    WorkflowCanceler
	scrapes     ExporterScrapes
	now         func() time.Time

	// refusedMu guards refused, the time each Server's last install was refused at acceptance
	// (ensureRefusalPause).
	refusedMu sync.Mutex
	refused   map[string]time.Time
}

// NewEnsureController wires the controller of deps.Entry's kind, refusing a missing dependency.
func NewEnsureController(deps EnsureControllerDeps) (*EnsureController, error) {
	if deps.Entry.Kind == "" || deps.Installer == nil || deps.Servers == nil || deps.Assignments == nil ||
		deps.Automation == nil || deps.Workflows == nil || deps.Canceler == nil || deps.Scrapes == nil {
		return nil, errors.New("software ensure controller: every dependency is required")
	}
	controller := &EnsureController{
		kind: deps.Entry.Kind, label: deps.Entry.Label, requester: EnsureRequester(deps.Entry.Kind),
		installer: deps.Installer, servers: deps.Servers, assignments: deps.Assignments,
		coverage: NewEnsureCoverage(deps.Automation), workflows: deps.Workflows, canceler: deps.Canceler, scrapes: deps.Scrapes,
		now: time.Now, refused: map[string]time.Time{},
	}
	if deps.Entry.Exporter != nil {
		controller.exporter = deps.Entry.Exporter.Name
	}
	return controller, nil
}

// Name implements convergence.Controller: the kind, so its lease is "controller:<kind>".
func (c *EnsureController) Name() string { return string(c.kind) }

// Collections implements convergence.Controller: a Server changes provisioning state or lock in
// `servers`, and an install, a failure, or an uninstall changes its assignment in
// `software_assignments`.
func (c *EnsureController) Collections() []string {
	return []string{"servers", "software_assignments"}
}

// KeysForChange implements convergence.Controller: a changed Server is its own key, and a changed
// assignment of the kind is its Server; a deletion needs nothing.
func (c *EnsureController) KeysForChange(ctx context.Context, change store.Change) ([]string, error) {
	if change.Operation == store.ChangeDelete {
		return nil, nil
	}
	if change.Collection == "servers" {
		return []string{change.Key}, nil
	}
	assignment, err := c.assignments.FindByRecordKey(ctx, change.Key)
	if errors.Is(err, softwaredomain.ErrAssignmentNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if assignment.Kind != c.kind {
		return nil, nil
	}
	return []string{assignment.ServerID}, nil
}

// Keys implements convergence.Controller with every present Server in provisioning state
// `deployed`, of every Site.
func (c *EnsureController) Keys(ctx context.Context) ([]string, error) {
	result, err := c.servers.List(ctx, serverdomain.ListFilter{ProvisioningState: softwaredomain.RequiredProvisioningState})
	if err != nil {
		return nil, err
	}
	keys := make([]string, 0, len(result.Servers))
	for _, server := range result.Servers {
		keys = append(keys, server.ID)
	}
	return keys, nil
}

// Reconcile implements convergence.Controller for one Server: it releases the controller's own
// parked installs, then, for a covered Server that needs one and that no Workflow holds, starts an
// install. A refused install and a failed metrics read are logged, not returned.
func (c *EnsureController) Reconcile(ctx context.Context, request convergence.Request) error {
	server, err := c.servers.FindByID(ctx, request.Key)
	if errors.Is(err, serverdomain.ErrServerNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	assignment, err := c.assignments.FindByServerAndKind(ctx, server.ID, c.kind)
	if errors.Is(err, softwaredomain.ErrAssignmentNotFound) {
		assignment = nil
	} else if err != nil {
		return err
	}
	resync := request.Trigger == convergence.TriggerResync
	state := assignmentState(assignment)
	if state == softwaredomain.StateInstalled && !resync {
		return nil
	}
	// Only a pending or failed assignment can name one of the controller's unfinished installs.
	if state == softwaredomain.StatePending || state == softwaredomain.StateFailed {
		held, err := c.releaseOwnParked(ctx, server, assignment)
		if err != nil || held {
			return err
		}
	}
	covered, err := c.coverage.Covers(ctx, server)
	if err != nil || !covered {
		return err
	}
	reason, err := c.installReason(ctx, server, assignment, resync)
	if err != nil || reason == "" {
		return err
	}
	if !resync && c.recentlyRefused(server.ID) {
		return nil
	}
	active, _, err := c.workflows.List(ctx, workflowdomain.WorkflowFilter{ServerID: server.ID, ActiveOnly: true, Limit: 1})
	if err != nil {
		return err
	}
	if len(active) > 0 {
		return nil
	}
	return c.install(ctx, server, reason)
}

// assignmentState is the assignment's state, "" when the Server has none.
func assignmentState(assignment *softwaredomain.Assignment) softwaredomain.AssignmentState {
	if assignment == nil {
		return ""
	}
	return assignment.State
}

// releaseOwnParked cancels the controller's own unfinished installs on the Server that stopped for
// attention, or whose install already failed and only waits to park, so a failed install never
// keeps holding the Server; their canceled Tasks mark the assignment failed. Workflows anyone else
// started, and those already being canceled, are left alone. It reports whether any unfinished
// Workflow still holds the Server.
func (c *EnsureController) releaseOwnParked(ctx context.Context, server *serverdomain.Server, assignment *softwaredomain.Assignment) (bool, error) {
	active, _, err := c.workflows.List(ctx, workflowdomain.WorkflowFilter{ServerID: server.ID, ActiveOnly: true})
	if err != nil {
		return false, err
	}
	for _, workflow := range active {
		if !c.ownInstall(workflow) || workflow.Commands.Cancel != nil || workflow.Status == workflowdomain.WorkflowCanceling {
			continue
		}
		failed := assignment.State == softwaredomain.StateFailed && assignment.LastWorkflowID == workflow.ID
		if workflow.Status != workflowdomain.WorkflowRequiresAttention && !failed {
			continue
		}
		reason := fmt.Sprintf("The %s install failed; swallow retries it later.", c.label)
		if err := c.canceler.CancelFor(ctx, workflow.ID, reason); err != nil {
			if errors.Is(err, workflowdomain.ErrWorkflowControlConflict) {
				continue
			}
			return true, err
		}
		slog.Info("software ensure: canceled a failed install", "kind", c.kind, "serverId", server.ID, "workflowId", workflow.ID)
	}
	return len(active) > 0, nil
}

// ownInstall reports whether the controller started workflow: the kind's install Workflow kind
// under EnsureRequester.
func (c *EnsureController) ownInstall(workflow *workflowdomain.Workflow) bool {
	return workflow.RequestedBy == c.requester && string(workflow.Kind) == c.kind.InstallWorkflowKind()
}

// installReason says why the covered Server needs an install now, or "" when it does not: no
// assignment or an absent one; a failed one whose retry backoff elapsed (at resync); an installed
// one whose exporter is down (at resync).
func (c *EnsureController) installReason(ctx context.Context, server *serverdomain.Server, assignment *softwaredomain.Assignment, resync bool) (string, error) {
	switch assignmentState(assignment) {
	case "", softwaredomain.StateAbsent:
		return "not installed", nil
	case softwaredomain.StateFailed:
		if !resync {
			return "", nil
		}
		return c.retryReason(ctx, server, assignment)
	case softwaredomain.StateInstalled:
		return c.downReason(ctx, server, assignment)
	default:
		return "", nil
	}
}

// retryReason retries a failed install once the backoff since the assignment failed has elapsed.
func (c *EnsureController) retryReason(ctx context.Context, server *serverdomain.Server, assignment *softwaredomain.Assignment) (string, error) {
	if c.now().Before(assignment.UpdatedAt.Add(ensureRetryBackoff[0])) {
		return "", nil
	}
	count, _, err := c.unsuccessfulInstalls(ctx, server)
	if err != nil {
		return "", err
	}
	if c.now().Before(assignment.UpdatedAt.Add(backoffAfter(count))) {
		return "", nil
	}
	return "the last install failed", nil
}

// downReason reinstalls an installed kind whose exporter has been down for ensureDownGrace, on a
// host not reported powered off, once the backoff after the controller's own unsuccessful
// reinstalls has elapsed. The assignment must have been installed for the whole grace window, so
// time before its exporter became a scrape target is never counted as down.
func (c *EnsureController) downReason(ctx context.Context, server *serverdomain.Server, assignment *softwaredomain.Assignment) (string, error) {
	if c.exporter == "" || server.Provisioning.PowerState == powerStateOff {
		return "", nil
	}
	installedAt := assignment.UpdatedAt
	if assignment.LastAppliedAt != nil {
		installedAt = *assignment.LastAppliedAt
	}
	now := c.now()
	if now.Sub(installedAt) < ensureDownGrace {
		return "", nil
	}
	observed, err := c.scrapes.Scrapes(ctx, server.Source.SiteID, c.exporter, server.ID, ensureDownGrace)
	if err != nil {
		slog.Warn("software ensure: cannot read the exporter's scrapes; acting on the assignment only",
			"kind", c.kind, "serverId", server.ID, "error", err)
		return "", nil
	}
	if observed != ScrapeDown {
		return "", nil
	}
	count, last, err := c.unsuccessfulInstalls(ctx, server)
	if err != nil {
		return "", err
	}
	if count > 0 && now.Before(last.Add(backoffAfter(count))) {
		return "", nil
	}
	return "its exporter is down", nil
}

// unsuccessfulInstalls counts the controller's own consecutive installs, newest first, that left
// no working kind: one that did not succeed, or one that succeeded but after which the exporter was
// never scraped successfully. Any other Workflow on the Server, or an install older than
// ensureRunHorizon, ends the run, so work a user or a deployment did there restarts the backoff.
// last is when the newest counted install finished. Only the store read is an error; a failed
// metrics read counts the install as working.
func (c *EnsureController) unsuccessfulInstalls(ctx context.Context, server *serverdomain.Server) (count int, last time.Time, err error) {
	history, _, err := c.workflows.List(ctx, workflowdomain.WorkflowFilter{ServerID: server.ID, Limit: len(ensureRetryBackoff)})
	if err != nil {
		return 0, time.Time{}, err
	}
	for _, workflow := range history {
		if !c.ownInstall(workflow) || !workflow.Status.Terminal() {
			break
		}
		finished := workflow.UpdatedAt
		if workflow.FinishedAt != nil {
			finished = *workflow.FinishedAt
		}
		if c.now().Sub(finished) > ensureRunHorizon {
			break
		}
		if workflow.Status == workflowdomain.WorkflowSucceeded && c.workedSince(ctx, server, finished) {
			break
		}
		if count == 0 {
			last = finished
		}
		count++
	}
	return count, last, nil
}

// workedSince reports whether the kind may have worked since an install of it succeeded at since:
// true unless every scrape of its exporter since then failed. Without an exporter, a metrics
// store, or a successful read there is no evidence it did not, so it is true.
func (c *EnsureController) workedSince(ctx context.Context, server *serverdomain.Server, since time.Time) bool {
	window := c.now().Sub(since)
	if c.exporter == "" || window < time.Second {
		return true
	}
	observed, err := c.scrapes.Scrapes(ctx, server.Source.SiteID, c.exporter, server.ID, window)
	if err != nil {
		slog.Warn("software ensure: cannot read the exporter's scrapes; counting the install as working",
			"kind", c.kind, "serverId", server.ID, "error", err)
		return true
	}
	return observed != ScrapeDown
}

// backoffAfter is the wait after count consecutive unsuccessful installs; none waits the first
// step, for a failure of an install the controller did not start.
func backoffAfter(count int) time.Duration {
	return ensureRetryBackoff[min(max(count-1, 0), len(ensureRetryBackoff)-1)]
}

// install starts the kind's install on the Server through the installer. A refused acceptance is
// logged and remembered (ensureRefusalPause) rather than returned, so the runtime does not retry it
// in a loop; the next resync tries again.
func (c *EnsureController) install(ctx context.Context, server *serverdomain.Server, reason string) error {
	workflowID, err := c.installer.Install(ctx, InstallInput{
		Kind: c.kind, Targets: []InstallTarget{{ServerID: server.ID}}, Spec: map[string]any{}, RequestedBy: c.requester,
	})
	if err != nil {
		c.rememberRefusal(server.ID)
		level := slog.LevelWarn
		if errors.Is(err, workflowdomain.ErrTargetsBusy) {
			level = slog.LevelDebug
		}
		slog.Log(ctx, level, "software ensure: install not started", "kind", c.kind, "serverId", server.ID, "reason", reason, "error", err)
		return nil
	}
	c.forgetRefusal(server.ID)
	slog.Info("software ensure: started install", "kind", c.kind, "serverId", server.ID, "workflowId", workflowID, "reason", reason)
	return nil
}

// recentlyRefused reports whether the Server's last install was refused within
// ensureRefusalPause; rememberRefusal and forgetRefusal keep that record.
func (c *EnsureController) recentlyRefused(serverID string) bool {
	c.refusedMu.Lock()
	defer c.refusedMu.Unlock()
	at, ok := c.refused[serverID]
	return ok && c.now().Sub(at) < ensureRefusalPause
}

func (c *EnsureController) rememberRefusal(serverID string) {
	c.refusedMu.Lock()
	defer c.refusedMu.Unlock()
	c.refused[serverID] = c.now()
}

func (c *EnsureController) forgetRefusal(serverID string) {
	c.refusedMu.Lock()
	defer c.refusedMu.Unlock()
	delete(c.refused, serverID)
}
