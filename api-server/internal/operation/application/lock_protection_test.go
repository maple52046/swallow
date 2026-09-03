package application

import (
	"context"
	"errors"
	"testing"
	"time"

	operationdomain "github.com/maple52046/swallow/internal/operation/domain"
	serverdomain "github.com/maple52046/swallow/internal/server/domain"
)

type lockTestExecutionRepo struct {
	created bool
	updated *operationdomain.Execution
}

func (r *lockTestExecutionRepo) Create(context.Context, *operationdomain.ExecutionOperation) error {
	r.created = true
	return nil
}
func (*lockTestExecutionRepo) FindByID(context.Context, string) (*operationdomain.ExecutionOperation, error) {
	return nil, errors.New("not implemented")
}
func (*lockTestExecutionRepo) List(context.Context, operationdomain.ExecutionListFilter) (operationdomain.ExecutionListResult, error) {
	return operationdomain.ExecutionListResult{}, nil
}
func (*lockTestExecutionRepo) FindActiveByServerIDs(context.Context, []string) ([]*operationdomain.ExecutionOperation, error) {
	return nil, nil
}
func (*lockTestExecutionRepo) Claim(context.Context, string, string, time.Time) (bool, error) {
	return true, nil
}
func (r *lockTestExecutionRepo) UpdateExecution(_ context.Context, _ string, _ string, execution operationdomain.Execution) error {
	copy := execution
	r.updated = &copy
	return nil
}
func (*lockTestExecutionRepo) MarkExpiredIndeterminate(context.Context, time.Time) (int64, error) {
	return 0, nil
}
func (*lockTestExecutionRepo) SecretVars(context.Context, string) (map[string]any, error) {
	return nil, nil
}

type lockTestServerRepo struct{ server *serverdomain.Server }

func (r lockTestServerRepo) FindByID(_ context.Context, id string) (*serverdomain.Server, error) {
	if r.server.ID == id {
		return r.server, nil
	}
	return nil, serverdomain.ErrServerNotFound
}
func (lockTestServerRepo) FindBySource(context.Context, serverdomain.Source) (*serverdomain.Server, error) {
	return nil, serverdomain.ErrServerNotFound
}
func (lockTestServerRepo) FindByHardware(context.Context, serverdomain.Hardware) ([]*serverdomain.Server, error) {
	return nil, nil
}
func (lockTestServerRepo) List(context.Context, serverdomain.ListFilter) (serverdomain.ListResult, error) {
	return serverdomain.ListResult{}, nil
}
func (lockTestServerRepo) Upsert(context.Context, *serverdomain.Server) error { return nil }
func (lockTestServerRepo) MarkAbsent(context.Context, string, time.Time) (int, error) {
	return 0, nil
}
func (lockTestServerRepo) SetMembership(context.Context, string, *serverdomain.MembershipStatus) error {
	return nil
}
func (lockTestServerRepo) SetGPUs(context.Context, string, []serverdomain.GPU) error { return nil }
func (lockTestServerRepo) CountByIntegration(context.Context, string) (int, error)   { return 0, nil }
func (lockTestServerRepo) Delete(context.Context, string) error                      { return nil }

type lockTestConfigurationRepo struct{ reads int }

func (r *lockTestConfigurationRepo) FindBySiteID(context.Context, string) (*operationdomain.AutomationConfiguration, error) {
	r.reads++
	return &operationdomain.AutomationConfiguration{Enabled: true, HasCredential: true}, nil
}
func (*lockTestConfigurationRepo) Upsert(context.Context, *operationdomain.AutomationConfiguration) error {
	return nil
}
func (*lockTestConfigurationRepo) ReplaceCredential(context.Context, string, operationdomain.AutomationCredential) error {
	return nil
}
func (*lockTestConfigurationRepo) Credential(context.Context, string) (operationdomain.AutomationCredential, error) {
	return operationdomain.AutomationCredential{}, nil
}

type lockTestCatalog struct{}

func (lockTestCatalog) Resolve(name string) (string, error) { return name, nil }

type lockTestRunner struct{ calls int }

func (r *lockTestRunner) Run(context.Context, operationdomain.RunnerInput) (operationdomain.RunnerResult, error) {
	r.calls++
	return operationdomain.RunnerResult{}, nil
}
func (*lockTestRunner) Logs(context.Context, string) (string, error) { return "", nil }
func (*lockTestRunner) Events(context.Context, string) ([]operationdomain.TaskEvent, error) {
	return nil, nil
}

type lockTestGuard struct {
	err       error
	serverIDs []string
}

func (g *lockTestGuard) RequireUnlocked(_ context.Context, serverIDs []string) error {
	g.serverIDs = append([]string(nil), serverIDs...)
	return g.err
}

type lockTestLeaseRepo struct{ releases int }

func (*lockTestLeaseRepo) Acquire(context.Context, string, string, time.Time) (bool, error) {
	return true, nil
}
func (*lockTestLeaseRepo) Renew(context.Context, string, string, time.Time) error { return nil }
func (r *lockTestLeaseRepo) Release(context.Context, string, string) error {
	r.releases++
	return nil
}
func (*lockTestLeaseRepo) ReleaseExpired(context.Context, time.Time) error { return nil }

type lockTestInventory struct{}

func (lockTestInventory) Inventory(context.Context, string) (map[string]any, error) {
	return map[string]any{}, nil
}

func lockTestServer() *serverdomain.Server {
	return &serverdomain.Server{
		ID:           "server-1",
		Source:       serverdomain.Source{SiteID: "site-1"},
		Observed:     serverdomain.Observed{Hostname: "node-1"},
		Provisioning: &serverdomain.ProvisioningStatus{State: "deployed"},
	}
}

func TestExecutionCreateChecksLiveLockBeforePersisting(t *testing.T) {
	repository := &lockTestExecutionRepo{}
	configurations := &lockTestConfigurationRepo{}
	guard := &lockTestGuard{err: &serverdomain.ServerLockedError{Name: "node-1"}}
	service := NewExecutionService(
		repository,
		lockTestServerRepo{server: lockTestServer()},
		configurations,
		lockTestCatalog{},
		&lockTestRunner{},
		nil,
		guard,
	)

	_, err := service.Create(context.Background(), CreateExecutionInput{
		Kind: "custom", TargetServerIDs: []string{"server-1"}, PlaybookName: "diagnostic.yml",
	})
	if !errors.Is(err, serverdomain.ErrServerLocked) {
		t.Fatalf("Create() error = %v, want ErrServerLocked", err)
	}
	if repository.created {
		t.Fatal("locked target persisted an Operation")
	}
	if configurations.reads != 0 {
		t.Fatal("automation configuration was read after lock refusal")
	}
}

func TestDispatcherChecksLiveLockBeforeRunnerSetup(t *testing.T) {
	repository := &lockTestExecutionRepo{}
	leases := &lockTestLeaseRepo{}
	configurations := &lockTestConfigurationRepo{}
	runner := &lockTestRunner{}
	guard := &lockTestGuard{err: &serverdomain.ServerLockedError{Name: "node-1"}}
	dispatcher := NewDispatcher(
		repository, leases, configurations, lockTestCatalog{}, runner, lockTestInventory{}, nil,
		time.Second, time.Minute, guard,
	)
	operation := &operationdomain.ExecutionOperation{
		ID: "operation-1", SiteID: "site-1", TargetServerIDs: []string{"server-1"},
		Execution: operationdomain.Execution{Status: operationdomain.StatusRunning},
	}

	dispatcher.execute(context.Background(), operation, "dispatcher-1")

	if runner.calls != 0 || configurations.reads != 0 {
		t.Fatalf("locked pre-run target reached setup: runner=%d configReads=%d", runner.calls, configurations.reads)
	}
	if repository.updated == nil || repository.updated.Status != operationdomain.StatusFailed {
		t.Fatalf("execution update = %+v, want failed", repository.updated)
	}
	if repository.updated.StatusReason != guard.err.Error() {
		t.Fatalf("status reason = %q, want %q", repository.updated.StatusReason, guard.err.Error())
	}
	if leases.releases != 1 {
		t.Fatalf("lease releases = %d, want 1", leases.releases)
	}
}
