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

// fakeOperator is a TargetAuthorizer for a caller who operates exactly the listed Servers and
// Platforms.
type fakeOperator struct {
	operates map[string]bool
}

func (f fakeOperator) OperatesTargets(_ context.Context, serverIDs []string, platformID string) (bool, error) {
	for _, id := range serverIDs {
		if !f.operates[id] {
			return false, nil
		}
	}
	if platformID != "" && !f.operates[platformID] {
		return false, nil
	}
	return len(serverIDs) > 0 || platformID != "", nil
}

// operator returns an Actor that operates ids.
func operator(userID, username string, ids ...string) Actor {
	operates := map[string]bool{}
	for _, id := range ids {
		operates[id] = true
	}
	return Actor{UserID: userID, Username: username, Targets: fakeOperator{operates: operates}}
}

// fakeStarters serves fixed accounts by user id; an unknown User is ErrRunForbidden.
type fakeStarters struct {
	accounts map[string]StarterAccount
}

func (f fakeStarters) StarterAccount(_ context.Context, userID string, serverIDs []string) (StarterAccount, error) {
	account, ok := f.accounts[userID]
	if !ok {
		return StarterAccount{}, validationdomain.ErrRunForbidden
	}
	hosts := map[string]StarterHost{}
	for _, id := range serverIDs {
		hosts[id] = account.Hosts[id]
	}
	account.Hosts = hosts
	return account, nil
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
