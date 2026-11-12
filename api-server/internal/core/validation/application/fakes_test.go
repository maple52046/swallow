package application

import (
	"context"
	"slices"

	validationdomain "github.com/maple52046/swallow/internal/core/validation/domain"
)

// fakeTargets serves fixed Servers and Platforms.
type fakeTargets struct {
	servers   map[string]TargetServer
	platforms map[string]TargetPlatform
}

func (f fakeTargets) Server(_ context.Context, id string) (TargetServer, error) {
	server, ok := f.servers[id]
	if !ok {
		return TargetServer{}, validationdomain.ErrTargetNotFound
	}
	return server, nil
}

func (f fakeTargets) Platform(_ context.Context, id string) (TargetPlatform, error) {
	platform, ok := f.platforms[id]
	if !ok {
		return TargetPlatform{}, validationdomain.ErrTargetNotFound
	}
	return platform, nil
}

// fakeLauncher records what it is asked to launch and can refuse persisting.
type fakeLauncher struct {
	prepared  []LaunchRequest
	persisted []PreparedRun
	persist   error
}

func (f *fakeLauncher) PrepareRun(_ context.Context, request LaunchRequest) (PreparedRun, error) {
	f.prepared = append(f.prepared, request)
	return PreparedRun{WorkflowID: "wf-" + request.Run.ID[:8]}, nil
}

func (f *fakeLauncher) PersistRun(_ context.Context, prepared PreparedRun) error {
	if f.persist != nil {
		return f.persist
	}
	f.persisted = append(f.persisted, prepared)
	return nil
}

// fakeCanceler records the Workflows it cancels.
type fakeCanceler struct {
	canceled []string
}

func (f *fakeCanceler) CancelWorkflow(_ context.Context, workflowID, _ string) error {
	f.canceled = append(f.canceled, workflowID)
	return nil
}

func moduleSuite(id string, target validationdomain.TargetKind, impact validationdomain.Impact) validationdomain.Suite {
	suite := validationdomain.Suite{
		ID: id, Source: validationdomain.SourceModule, Name: id, Target: target, Impact: impact, BudgetSeconds: 600,
		Module: &validationdomain.ModuleRef{Kind: "validation", ID: "host", Version: "0.1.0"},
		Checks: []validationdomain.Check{{ID: "time-sync", Gating: true}},
	}
	if target == validationdomain.TargetPlatform {
		suite.PlatformType = "slurm"
	}
	return suite
}

func contains(values []string, value string) bool { return slices.Contains(values, value) }
