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
	Confirm     bool
	Trigger     validationdomain.Trigger
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
	launcher   RunLauncher
	canceler   WorkflowCanceler
	retention  time.Duration
	now        func() time.Time
}

// ServiceDeps are the Service's dependencies; every one is required.
type ServiceDeps struct {
	Transactor store.Transactor
	Catalog    *Catalog
	Users      validationdomain.SuiteRepository
	Runs       validationdomain.RunRepository
	Schedules  validationdomain.ScheduleRepository
	Targets    Targets
	Launcher   RunLauncher
	Canceler   WorkflowCanceler
	// Retention is how long a run is kept; zero is DefaultRunRetention.
	Retention time.Duration
}

// NewService builds the Service; a missing dependency is refused so no process runs it half wired.
func NewService(deps ServiceDeps) (*Service, error) {
	if deps.Transactor == nil || deps.Catalog == nil || deps.Users == nil || deps.Runs == nil || deps.Schedules == nil ||
		deps.Targets == nil || deps.Launcher == nil || deps.Canceler == nil {
		return nil, errors.New("validation service: every dependency is required")
	}
	retention := deps.Retention
	if retention <= 0 {
		retention = DefaultRunRetention
	}
	return &Service{
		transactor: deps.Transactor, catalog: deps.Catalog, users: deps.Users, runs: deps.Runs, schedules: deps.Schedules,
		targets: deps.Targets, launcher: deps.Launcher, canceler: deps.Canceler, retention: retention, now: time.Now,
	}, nil
}

// Catalog returns the suite catalog.
func (s *Service) Catalog() *Catalog { return s.catalog }

// Start accepts a report run: it checks the suite, the parameters, and the targets
// (validation.md §3), plans the run's Workflow, and writes the run and the Workflow in one
// transaction. Errors: ErrSuiteNotFound, ErrInvalidRequest, ErrTargetNotFound, ErrTargetNotRunnable,
// and the workflow context's acceptance errors (busy, locked, automation unavailable).
func (s *Service) Start(ctx context.Context, input StartInput) (validationdomain.Run, error) {
	suite, err := s.catalog.Get(ctx, strings.TrimSpace(input.SuiteID))
	if err != nil {
		return validationdomain.Run{}, err
	}
	if suite.Retired {
		return validationdomain.Run{}, fmt.Errorf("%w: suite %s is retired", validationdomain.ErrSuiteNotFound, suite.ID)
	}
	if err := suite.ValidateParameters(input.Parameters); err != nil {
		return validationdomain.Run{}, err
	}
	if suite.Impact == validationdomain.ImpactDisruptive && !input.Confirm {
		return validationdomain.Run{}, fmt.Errorf("%w: suite %s is disruptive; confirm it", validationdomain.ErrInvalidRequest, suite.ID)
	}
	target, siteID, platform, err := s.resolveTargets(ctx, suite, input.ServerIDs, input.PlatformID)
	if err != nil {
		return validationdomain.Run{}, err
	}
	trigger := input.Trigger
	if trigger.Kind == "" {
		trigger.Kind = validationdomain.TriggerManual
	}
	now := s.now().UTC()
	run := validationdomain.Run{
		ID: uuid.NewString(), SiteID: siteID, Suite: runSuite(suite), Checks: slices.Clone(suite.Checks),
		Target: target, Parameters: input.Parameters, Mode: validationdomain.ModeReport, Trigger: trigger,
		Status: validationdomain.StatusPending, RequestedBy: input.RequestedBy, RequestedAt: now,
		ExpiresAt: now.Add(s.retention),
	}
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

// Cancel writes the cancel of a report run's Workflow and returns the run. A run that ended or a
// gate run is ErrRunNotCancelable.
func (s *Service) Cancel(ctx context.Context, id, requestedBy string) (validationdomain.Run, error) {
	run, err := s.runs.Get(ctx, id)
	if err != nil {
		return validationdomain.Run{}, err
	}
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

// CreateSuite stores a new user suite at revision 1.
func (s *Service) CreateSuite(ctx context.Context, input validationdomain.UserSuiteInput, createdBy string) (validationdomain.Suite, error) {
	suite, err := validationdomain.NewUserSuite(uuid.NewString(), input, createdBy, s.now().UTC())
	if err != nil {
		return validationdomain.Suite{}, err
	}
	if err := s.users.Create(ctx, suite); err != nil {
		return validationdomain.Suite{}, err
	}
	return suite, nil
}

// ReplaceSuite replaces a user suite; a module suite or a retired one is ErrSuiteImmutable.
func (s *Service) ReplaceSuite(ctx context.Context, id string, input validationdomain.UserSuiteInput) (validationdomain.Suite, error) {
	current, err := s.catalog.Get(ctx, id)
	if err != nil {
		return validationdomain.Suite{}, err
	}
	next, err := current.Replace(input, s.now().UTC())
	if err != nil {
		return validationdomain.Suite{}, err
	}
	if err := s.users.Replace(ctx, next); err != nil {
		return validationdomain.Suite{}, err
	}
	return next, nil
}

// RetireSuite retires a user suite: ErrSuiteImmutable for a module suite, ErrSuiteInUse while a
// schedule names it. Retiring a retired suite succeeds.
func (s *Service) RetireSuite(ctx context.Context, id string) error {
	current, err := s.catalog.Get(ctx, id)
	if err != nil {
		return err
	}
	if current.Source != validationdomain.SourceUser {
		return fmt.Errorf("%w: module suite %s", validationdomain.ErrSuiteImmutable, id)
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
	current.Retired, current.UpdatedAt = true, s.now().UTC()
	return s.users.Replace(ctx, current)
}

// resolveTargets checks the request's targets against the suite (validation.md §3) and returns the
// run's target, its Site, and the target Platform of a platform suite.
func (s *Service) resolveTargets(ctx context.Context, suite validationdomain.Suite, serverIDs []string, platformID string) (validationdomain.Target, string, *TargetPlatform, error) {
	platformID = strings.TrimSpace(platformID)
	switch suite.Target {
	case validationdomain.TargetServers:
		if platformID != "" || len(serverIDs) == 0 {
			return validationdomain.Target{}, "", nil, fmt.Errorf("%w: suite %s takes serverIds and no platformId", validationdomain.ErrInvalidRequest, suite.ID)
		}
		siteID := ""
		seen := map[string]bool{}
		for _, id := range serverIDs {
			id = strings.TrimSpace(id)
			if id == "" || seen[id] {
				return validationdomain.Target{}, "", nil, fmt.Errorf("%w: serverIds must be distinct and non-empty", validationdomain.ErrInvalidRequest)
			}
			seen[id] = true
			server, err := s.targets.Server(ctx, id)
			if err != nil {
				return validationdomain.Target{}, "", nil, err
			}
			if siteID == "" {
				siteID = server.SiteID
			}
			if server.SiteID != siteID {
				return validationdomain.Target{}, "", nil, fmt.Errorf("%w: the Servers belong to more than one Site", validationdomain.ErrInvalidRequest)
			}
			if !server.Deployed {
				return validationdomain.Target{}, "", nil, fmt.Errorf("%w: Server %s is not deployed", validationdomain.ErrTargetNotRunnable, server.Name)
			}
			if server.ClaimedBy != "" && suite.Impact.RefusesClaimedServers() {
				return validationdomain.Target{}, "", nil, fmt.Errorf("%w: Server %s belongs to Platform %s; a %s suite would compete with its workloads, so run the Platform's suites instead",
					validationdomain.ErrTargetNotRunnable, server.Name, server.ClaimedBy, suite.Impact)
			}
		}
		ids := make([]string, 0, len(seen))
		for _, id := range serverIDs {
			ids = append(ids, strings.TrimSpace(id))
		}
		return validationdomain.Target{Kind: validationdomain.TargetServers, ServerIDs: ids}, siteID, nil, nil
	case validationdomain.TargetPlatform:
		if platformID == "" || len(serverIDs) > 0 {
			return validationdomain.Target{}, "", nil, fmt.Errorf("%w: suite %s takes a platformId and no serverIds", validationdomain.ErrInvalidRequest, suite.ID)
		}
		platform, err := s.targets.Platform(ctx, platformID)
		if err != nil {
			return validationdomain.Target{}, "", nil, err
		}
		switch {
		case platform.Type != suite.PlatformType:
			return validationdomain.Target{}, "", nil, fmt.Errorf("%w: Platform %s is %s; suite %s validates %s",
				validationdomain.ErrTargetNotRunnable, platform.Name, platform.Type, suite.ID, suite.PlatformType)
		case !platform.Active:
			return validationdomain.Target{}, "", nil, fmt.Errorf("%w: Platform %s is not active", validationdomain.ErrTargetNotRunnable, platform.Name)
		case len(platform.AccessServerIDs) == 0:
			return validationdomain.Target{}, "", nil, fmt.Errorf("%w: Platform %s has no Server to enter it through", validationdomain.ErrTargetNotRunnable, platform.Name)
		}
		members := make([]string, 0, len(platform.Members))
		for _, member := range platform.Members {
			members = append(members, member.ServerID)
		}
		return validationdomain.Target{Kind: validationdomain.TargetPlatform, PlatformID: platform.ID,
			ServerIDs: slices.Clone(platform.AccessServerIDs), MemberIDs: members}, platform.SiteID, &platform, nil
	}
	return validationdomain.Target{}, "", nil, fmt.Errorf("%w: suite %s has an unknown target kind", validationdomain.ErrInvalidRequest, suite.ID)
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
