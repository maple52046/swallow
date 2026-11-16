package application

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/maple52046/swallow/internal/core/store"
	validationdomain "github.com/maple52046/swallow/internal/core/validation/domain"
)

// TargetServer is what accepting a run needs to know about one target Server.
type TargetServer struct {
	ID       string
	Name     string
	SiteID   string
	Deployed bool
	// ClaimedBy is the Platform that claims the Server, empty when none does.
	ClaimedBy string
}

// PlatformMember is one member Server of a target Platform and its roles in it.
type PlatformMember struct {
	ServerID string
	Roles    []string
}

// TargetPlatform is what accepting a run needs to know about a target Platform.
type TargetPlatform struct {
	ID     string
	Name   string
	SiteID string
	Type   string
	// Active is whether its lifecycle is active: deployed and not being uninstalled.
	Active  bool
	Members []PlatformMember
	// AccessServerIDs are the Servers the Platform is entered through, as its module decides from
	// its spec (login nodes, control-plane members); empty when it names none.
	AccessServerIDs []string
}

// Targets reads targets for acceptance. Both methods return ErrTargetNotFound for an id that does
// not exist.
type Targets interface {
	Server(ctx context.Context, id string) (TargetServer, error)
	Platform(ctx context.Context, id string) (TargetPlatform, error)
}

// LaunchRequest is one accepted run and what its Workflow is compiled from.
type LaunchRequest struct {
	Run   validationdomain.Run
	Suite validationdomain.Suite
	// Platform is the target Platform of a platform suite, nil for a servers suite.
	Platform *TargetPlatform
}

// PreparedRun is a run's Workflow planned and checked but not yet accepted. Plan is the launcher's
// own; the service only hands it back to PersistRun.
type PreparedRun struct {
	WorkflowID string
	Plan       any
}

// RunLauncher plans and accepts the Workflow of a report run (implemented in the composition root,
// which compiles the suite's template).
type RunLauncher interface {
	// PrepareRun plans the Workflow without writing anything, applying the automation and Server
	// Lock checks every Ansible Task gets; a check that fails is returned as the workflow context's
	// error.
	PrepareRun(ctx context.Context, request LaunchRequest) (PreparedRun, error)
	// PersistRun accepts the planned Workflow; it must be called inside the caller's transaction,
	// where the busy check refuses a target with unfinished work.
	PersistRun(ctx context.Context, prepared PreparedRun) error
}

// WorkflowCanceler writes the cancel of a run's Workflow with a reason.
type WorkflowCanceler interface {
	CancelWorkflow(ctx context.Context, workflowID, reason string) error
}

// StartInput is a request to run a suite.
type StartInput struct {
	SuiteID    string
	ServerIDs  []string
	PlatformID string
	Parameters map[string]any
	// Confirm acknowledges a disruptive suite's impact.
	Confirm bool
	// SuiteRevision, when positive, is the user suite revision the starter reviewed; the run is
	// refused (ErrRevisionConflict) when the suite has moved on. A module suite takes none.
	SuiteRevision int
	Trigger       validationdomain.Trigger
	// Starter is the User who starts the run from the API: the starter level and, for a user suite,
	// the account checks apply to it, and the run records it. Nil for a run swallow starts itself (a
	// schedule), which only a module suite may be.
	Starter *Actor
	// RequestedBy names a run without a Starter (the schedule controller).
	RequestedBy string
}

// DefaultRunRetention is how long a run is kept when the installation sets none.
const DefaultRunRetention = 90 * 24 * time.Hour

// Service holds the run and suite use cases: start, read, and cancel runs, and manage user suites.
type Service struct {
	transactor store.Transactor
	catalog    *Catalog
	users      validationdomain.SuiteRepository
	runs       validationdomain.RunRepository
	schedules  validationdomain.ScheduleRepository
	targets    Targets
	starters   StarterAccess
	launcher   RunLauncher
	canceler   WorkflowCanceler
	retention  time.Duration
	now        func() time.Time
}

// ServiceDeps are the Service's dependencies; every one is required. Starters is a security check
// (a user suite runs as its starter only where that User may log in): a process that never starts
// a user suite still passes an implementation, never nil.
type ServiceDeps struct {
	Transactor store.Transactor
	Catalog    *Catalog
	Users      validationdomain.SuiteRepository
	Runs       validationdomain.RunRepository
	Schedules  validationdomain.ScheduleRepository
	Targets    Targets
	Starters   StarterAccess
	Launcher   RunLauncher
	Canceler   WorkflowCanceler
	// Retention is how long a run is kept; zero is DefaultRunRetention.
	Retention time.Duration
}

// NewService builds the Service; a missing dependency is refused so no process runs it half wired.
func NewService(deps ServiceDeps) (*Service, error) {
	if deps.Transactor == nil || deps.Catalog == nil || deps.Users == nil || deps.Runs == nil || deps.Schedules == nil ||
		deps.Targets == nil || deps.Starters == nil || deps.Launcher == nil || deps.Canceler == nil {
		return nil, errors.New("validation service: every dependency is required")
	}
	retention := deps.Retention
	if retention <= 0 {
		retention = DefaultRunRetention
	}
	return &Service{
		transactor: deps.Transactor, catalog: deps.Catalog, users: deps.Users, runs: deps.Runs, schedules: deps.Schedules,
		targets: deps.Targets, starters: deps.Starters, launcher: deps.Launcher, canceler: deps.Canceler,
		retention: retention, now: time.Now,
	}, nil
}

// Catalog returns the suite catalog.
func (s *Service) Catalog() *Catalog { return s.catalog }

// Start accepts a report run (validation.md §3): it checks the suite, its pinned revision, and the
// parameters; the starter level; the targets; and, for a user suite, that the starter's own account
// can run it on every target. It then plans the run's Workflow and writes the run and the Workflow in
// one transaction.
//
// The route guard has already required Pool read on every target; Start adds what depends on the
// suite. A run without a Starter (a schedule's) skips the starter checks and may only be a module
// suite. Errors: ErrSuiteNotFound, ErrInvalidRequest, ErrRevisionConflict, ErrRunForbidden,
// ErrTargetNotFound, ErrTargetNotRunnable, and the workflow context's acceptance errors (busy, locked,
// automation unavailable).
func (s *Service) Start(ctx context.Context, input StartInput) (validationdomain.Run, error) {
	suite, err := s.catalog.Get(ctx, strings.TrimSpace(input.SuiteID))
	if err != nil {
		return validationdomain.Run{}, err
	}
	if suite.Retired {
		return validationdomain.Run{}, fmt.Errorf("%w: suite %s is retired", validationdomain.ErrSuiteNotFound, suite.ID)
	}
	if err := checkPinnedRevision(suite, input.SuiteRevision); err != nil {
		return validationdomain.Run{}, err
	}
	if err := suite.ValidateParameters(input.Parameters); err != nil {
		return validationdomain.Run{}, err
	}
	if suite.Impact == validationdomain.ImpactDisruptive && !input.Confirm {
		return validationdomain.Run{}, fmt.Errorf("%w: suite %s is disruptive; confirm it", validationdomain.ErrInvalidRequest, suite.ID)
	}
	requestedBy, requestedByID := input.RequestedBy, ""
	switch {
	case input.Starter != nil:
		requestedBy, requestedByID = input.Starter.Username, input.Starter.UserID
		if err := checkStarterLevel(ctx, suite, *input.Starter, input.ServerIDs, input.PlatformID); err != nil {
			return validationdomain.Run{}, err
		}
	case suite.Source == validationdomain.SourceUser:
		return validationdomain.Run{}, fmt.Errorf("%w: user suite %s runs as the User who starts it, and this run has none", validationdomain.ErrInvalidRequest, suite.ID)
	}
	resolved, err := s.resolveTargets(ctx, suite, input.ServerIDs, input.PlatformID)
	if err != nil {
		return validationdomain.Run{}, err
	}
	var runAs *validationdomain.RunAccount
	if suite.Source == validationdomain.SourceUser {
		account, err := s.starters.StarterAccount(ctx, input.Starter.UserID, resolved.target.ServerIDs)
		if err != nil {
			return validationdomain.Run{}, err
		}
		recorded, err := starterRunAccount(account, suite, resolved.target.ServerIDs, resolved.names)
		if err != nil {
			return validationdomain.Run{}, err
		}
		runAs = &recorded
	}
	trigger := input.Trigger
	if trigger.Kind == "" {
		trigger.Kind = validationdomain.TriggerManual
	}
	now := s.now().UTC()
	run := validationdomain.Run{
		ID: uuid.NewString(), SiteID: resolved.siteID, Suite: runSuite(suite), Checks: slices.Clone(suite.Checks),
		Target: resolved.target, Parameters: input.Parameters, Mode: validationdomain.ModeReport, Trigger: trigger,
		Status: validationdomain.StatusPending, RequestedBy: requestedBy, RequestedByID: requestedByID, RunAs: runAs,
		RequestedAt: now, ExpiresAt: now.Add(s.retention),
	}
	platform := resolved.platform
	if run.Parameters == nil {
		run.Parameters = map[string]any{}
	}
	prepared, err := s.launcher.PrepareRun(ctx, LaunchRequest{Run: run, Suite: suite, Platform: platform})
	if err != nil {
		return validationdomain.Run{}, err
	}
	run.WorkflowID = prepared.WorkflowID
	err = s.transactor.InTransaction(ctx, func(ctx context.Context) error {
		if err := s.launcher.PersistRun(ctx, prepared); err != nil {
			return err
		}
		return s.runs.Create(ctx, run)
	})
	if err != nil {
		return validationdomain.Run{}, err
	}
	return run, nil
}

// GateRequest asks for the run record of a suite a deployment composes as a gate.
type GateRequest struct {
	SuiteID    string
	PlatformID string
	SiteID     string
	WorkflowID string
	AccessIDs  []string
	MemberIDs  []string
	// Parameters are the run's parameters, checked against the suite's schema.
	Parameters  map[string]any
	RequestedBy string
}

// GateRun builds the run record of a gate (mode gate, trigger deployment) without writing it: the
// deployment's launcher compiles the suite's template into its own Workflow and writes the run with
// RecordGateRun in its acceptance transaction. It returns the suite with the run.
func (s *Service) GateRun(request GateRequest) (validationdomain.Run, validationdomain.Suite, error) {
	suite, ok := s.catalog.Module(request.SuiteID)
	if !ok {
		return validationdomain.Run{}, validationdomain.Suite{}, fmt.Errorf("%w: module suite %s", validationdomain.ErrSuiteNotFound, request.SuiteID)
	}
	if err := suite.ValidateParameters(request.Parameters); err != nil {
		return validationdomain.Run{}, validationdomain.Suite{}, err
	}
	parameters := request.Parameters
	if parameters == nil {
		parameters = map[string]any{}
	}
	now := s.now().UTC()
	return validationdomain.Run{
		ID: uuid.NewString(), SiteID: request.SiteID, Suite: runSuite(suite), Checks: slices.Clone(suite.Checks),
		Target: validationdomain.Target{Kind: validationdomain.TargetPlatform, PlatformID: request.PlatformID,
			ServerIDs: slices.Clone(request.AccessIDs), MemberIDs: slices.Clone(request.MemberIDs)},
		Parameters: parameters, Mode: validationdomain.ModeGate,
		Trigger:    validationdomain.Trigger{Kind: validationdomain.TriggerDeployment, WorkflowID: request.WorkflowID},
		WorkflowID: request.WorkflowID, Status: validationdomain.StatusPending,
		RequestedBy: request.RequestedBy, RequestedAt: now, ExpiresAt: now.Add(s.retention),
	}, suite, nil
}

// RecordGateRun writes a gate run; it must be called inside the deployment's acceptance
// transaction, so a refused deployment leaves no run.
func (s *Service) RecordGateRun(ctx context.Context, run validationdomain.Run) error {
	return s.runs.Create(ctx, run)
}

// Get returns one run.
func (s *Service) Get(ctx context.Context, id string) (validationdomain.Run, error) {
	return s.runs.Get(ctx, id)
}

// List returns one page of runs, newest first, and the total.
func (s *Service) List(ctx context.Context, filter validationdomain.RunFilter) ([]validationdomain.Run, int, error) {
	return s.runs.List(ctx, filter)
}

// Cancel writes the cancel of a report run's Workflow for actor and returns the run. The route guard
// has required Pool read on the run's targets; Cancel allows the run's starter (by user id), a User
// who operates every target, and an admin (ErrRunForbidden otherwise). A run that ended or a gate
// run is ErrRunNotCancelable.
func (s *Service) Cancel(ctx context.Context, id string, actor Actor) (validationdomain.Run, error) {
	run, err := s.runs.Get(ctx, id)
	if err != nil {
		return validationdomain.Run{}, err
	}
	if err := checkCancel(ctx, run, actor); err != nil {
		return validationdomain.Run{}, err
	}
	requestedBy := actor.Username
	if !run.Status.Active() {
		return validationdomain.Run{}, fmt.Errorf("%w: the run already ended", validationdomain.ErrRunNotCancelable)
	}
	if run.Mode == validationdomain.ModeGate {
		return validationdomain.Run{}, fmt.Errorf("%w: a gate run ends with its deployment; cancel Workflow %s", validationdomain.ErrRunNotCancelable, run.WorkflowID)
	}
	reason := "Canceled by the user."
	if requestedBy != "" {
		reason = "Canceled by " + requestedBy + "."
	}
	if err := s.canceler.CancelWorkflow(ctx, run.WorkflowID, reason); err != nil {
		return validationdomain.Run{}, err
	}
	return run, nil
}

// checkCancel allows a cancel by the run's starter (matched by user id, never by username, which a
// later User may take), by an admin, or by a User who operates every target of the run: its Servers,
// a Platform run's members, and its Platform.
func checkCancel(ctx context.Context, run validationdomain.Run, actor Actor) error {
	if actor.Admin || run.RequestedByID != "" && run.RequestedByID == actor.UserID {
		return nil
	}
	if actor.Targets == nil {
		return fmt.Errorf("%w: the caller's access to the run's targets is unknown", validationdomain.ErrRunForbidden)
	}
	servers := append(slices.Clone(run.Target.ServerIDs), run.Target.MemberIDs...)
	operates, err := actor.Targets.OperatesTargets(ctx, servers, run.Target.PlatformID)
	if err != nil {
		return err
	}
	if !operates {
		return fmt.Errorf("%w: only the User who started run %s, an owner of its targets' Pool, or an admin may cancel it", validationdomain.ErrRunForbidden, run.ID)
	}
	return nil
}

// checkPinnedRevision refuses a run pinned to a revision that is not the suite's current one: a
// module suite takes no revision (ErrInvalidRequest), and a user suite that changed since the
// starter reviewed it is ErrRevisionConflict, so a script the starter never saw never runs.
func checkPinnedRevision(suite validationdomain.Suite, revision int) error {
	switch {
	case revision < 0:
		return fmt.Errorf("%w: suiteRevision must be positive", validationdomain.ErrInvalidRequest)
	case revision == 0:
		return nil
	case suite.Source != validationdomain.SourceUser:
		return fmt.Errorf("%w: module suite %s has no revisions; leave suiteRevision out", validationdomain.ErrInvalidRequest, suite.ID)
	case revision != suite.Revision:
		return fmt.Errorf("%w: suite %s is at revision %d, not %d; review the new revision and start it again",
			validationdomain.ErrRevisionConflict, suite.ID, suite.Revision, revision)
	}
	return nil
}

// CreateSuite stores a new user suite at revision 1 with author as its author, and records that
// revision, in one transaction.
func (s *Service) CreateSuite(ctx context.Context, input validationdomain.UserSuiteInput, author validationdomain.Author) (validationdomain.Suite, error) {
	now := s.now().UTC()
	suite, err := validationdomain.NewUserSuite(uuid.NewString(), input, author, now)
	if err != nil {
		return validationdomain.Suite{}, err
	}
	err = s.transactor.InTransaction(ctx, func(ctx context.Context) error {
		if err := s.users.Create(ctx, suite); err != nil {
			return err
		}
		return s.users.AddRevision(ctx, suite.CurrentRevision(now))
	})
	if err != nil {
		return validationdomain.Suite{}, err
	}
	return suite, nil
}

// SuiteReplace is a replace of a user suite: the new content and, when ExpectedRevision is positive,
// the revision the writer loaded (ErrRevisionConflict when the suite is no longer at it).
type SuiteReplace struct {
	Input            validationdomain.UserSuiteInput
	ExpectedRevision int
}

// ReplaceSuite replaces a user suite for actor and records a new revision when the content that
// runs changed, in one transaction. The write is conditional on the revision read, so of two
// concurrent replaces one wins and the other is ErrRevisionConflict. Errors: ErrSuiteNotFound,
// ErrSuiteImmutable (a module suite, or a retired one), ErrNotSuiteAuthor (anyone but the author,
// admins included), ErrInvalidRequest, ErrRevisionConflict.
func (s *Service) ReplaceSuite(ctx context.Context, id string, write SuiteReplace, actor Actor) (validationdomain.Suite, error) {
	current, err := s.catalog.Get(ctx, id)
	if err != nil {
		return validationdomain.Suite{}, err
	}
	now := s.now().UTC()
	next, err := current.Replace(write.Input, actor.UserID, now)
	if err != nil {
		return validationdomain.Suite{}, err
	}
	if write.ExpectedRevision > 0 && write.ExpectedRevision != current.Revision {
		return validationdomain.Suite{}, fmt.Errorf("%w: suite %s is at revision %d, not %d",
			validationdomain.ErrRevisionConflict, id, current.Revision, write.ExpectedRevision)
	}
	err = s.transactor.InTransaction(ctx, func(ctx context.Context) error {
		if err := s.users.Replace(ctx, next, current.Revision); err != nil {
			return err
		}
		if next.Revision == current.Revision {
			return nil
		}
		return s.users.AddRevision(ctx, next.CurrentRevision(now))
	})
	if err != nil {
		return validationdomain.Suite{}, err
	}
	return next, nil
}

// RetireSuite retires a user suite for actor: ErrSuiteImmutable for a module suite,
// ErrNotSuiteAuthor for anyone but its author or an admin, ErrSuiteInUse while a schedule names it.
// Retiring a retired suite succeeds. Its revisions stay readable.
func (s *Service) RetireSuite(ctx context.Context, id string, actor Actor) error {
	current, err := s.catalog.Get(ctx, id)
	if err != nil {
		return err
	}
	if err := current.CheckRetire(actor.UserID, actor.Admin); err != nil {
		return err
	}
	if current.Retired {
		return nil
	}
	used, err := s.schedules.UsesSuite(ctx, id)
	if err != nil {
		return err
	}
	if used {
		return fmt.Errorf("%w: delete or change the schedules that run suite %s first", validationdomain.ErrSuiteInUse, id)
	}
	return s.users.Retire(ctx, id, s.now().UTC())
}

// SuiteRevision returns one kept revision of a user suite, retired or not: ErrSuiteNotFound for an
// unknown suite, ErrRevisionNotFound for a module suite or a revision that was never kept.
func (s *Service) SuiteRevision(ctx context.Context, suiteID string, revision int) (validationdomain.SuiteRevision, error) {
	suite, err := s.catalog.Get(ctx, suiteID)
	if err != nil {
		return validationdomain.SuiteRevision{}, err
	}
	if suite.Source != validationdomain.SourceUser {
		return validationdomain.SuiteRevision{}, fmt.Errorf("%w: module suite %s has no revisions", validationdomain.ErrRevisionNotFound, suiteID)
	}
	return s.users.Revision(ctx, suiteID, revision)
}

// resolvedTargets is a run's checked targets: the run's target, its Site, the target Platform of a
// platform suite, and the target Servers' names by id, for messages.
type resolvedTargets struct {
	target   validationdomain.Target
	siteID   string
	platform *TargetPlatform
	names    map[string]string
}

// resolveTargets checks the request's targets against the suite (validation.md §3).
func (s *Service) resolveTargets(ctx context.Context, suite validationdomain.Suite, serverIDs []string, platformID string) (resolvedTargets, error) {
	platformID = strings.TrimSpace(platformID)
	switch suite.Target {
	case validationdomain.TargetServers:
		if platformID != "" || len(serverIDs) == 0 {
			return resolvedTargets{}, fmt.Errorf("%w: suite %s takes serverIds and no platformId", validationdomain.ErrInvalidRequest, suite.ID)
		}
		siteID := ""
		names := map[string]string{}
		for _, id := range serverIDs {
			id = strings.TrimSpace(id)
			if _, seen := names[id]; id == "" || seen {
				return resolvedTargets{}, fmt.Errorf("%w: serverIds must be distinct and non-empty", validationdomain.ErrInvalidRequest)
			}
			server, err := s.targets.Server(ctx, id)
			if err != nil {
				return resolvedTargets{}, err
			}
			names[id] = server.Name
			if siteID == "" {
				siteID = server.SiteID
			}
			if server.SiteID != siteID {
				return resolvedTargets{}, fmt.Errorf("%w: the Servers belong to more than one Site", validationdomain.ErrInvalidRequest)
			}
			if !server.Deployed {
				return resolvedTargets{}, fmt.Errorf("%w: Server %s is not deployed", validationdomain.ErrTargetNotRunnable, server.Name)
			}
			if server.ClaimedBy != "" && suite.Impact.RefusesClaimedServers() {
				return resolvedTargets{}, fmt.Errorf("%w: Server %s belongs to Platform %s; a %s suite would compete with its workloads, so run the Platform's suites instead",
					validationdomain.ErrTargetNotRunnable, server.Name, server.ClaimedBy, suite.Impact)
			}
		}
		ids := make([]string, 0, len(names))
		for _, id := range serverIDs {
			ids = append(ids, strings.TrimSpace(id))
		}
		return resolvedTargets{target: validationdomain.Target{Kind: validationdomain.TargetServers, ServerIDs: ids}, siteID: siteID, names: names}, nil
	case validationdomain.TargetPlatform:
		if platformID == "" || len(serverIDs) > 0 {
			return resolvedTargets{}, fmt.Errorf("%w: suite %s takes a platformId and no serverIds", validationdomain.ErrInvalidRequest, suite.ID)
		}
		platform, err := s.targets.Platform(ctx, platformID)
		if err != nil {
			return resolvedTargets{}, err
		}
		switch {
		case platform.Type != suite.PlatformType:
			return resolvedTargets{}, fmt.Errorf("%w: Platform %s is %s; suite %s validates %s",
				validationdomain.ErrTargetNotRunnable, platform.Name, platform.Type, suite.ID, suite.PlatformType)
		case !platform.Active:
			return resolvedTargets{}, fmt.Errorf("%w: Platform %s is not active", validationdomain.ErrTargetNotRunnable, platform.Name)
		case len(platform.AccessServerIDs) == 0:
			return resolvedTargets{}, fmt.Errorf("%w: Platform %s has no Server to enter it through", validationdomain.ErrTargetNotRunnable, platform.Name)
		}
		members := make([]string, 0, len(platform.Members))
		for _, member := range platform.Members {
			members = append(members, member.ServerID)
		}
		return resolvedTargets{
			target: validationdomain.Target{Kind: validationdomain.TargetPlatform, PlatformID: platform.ID,
				ServerIDs: slices.Clone(platform.AccessServerIDs), MemberIDs: members},
			siteID: platform.SiteID, platform: &platform,
		}, nil
	}
	return resolvedTargets{}, fmt.Errorf("%w: suite %s has an unknown target kind", validationdomain.ErrInvalidRequest, suite.ID)
}

// runSuite records which suite a run ran.
func runSuite(suite validationdomain.Suite) validationdomain.RunSuite {
	recorded := validationdomain.RunSuite{ID: suite.ID, Source: suite.Source, Name: suite.Name}
	if suite.Module != nil {
		recorded.Version = suite.Module.Version
	}
	if suite.Source == validationdomain.SourceUser {
		recorded.Revision, recorded.ScriptSHA256, recorded.Script = suite.Revision, suite.ScriptSHA256, suite.Script
	}
	return recorded
}
