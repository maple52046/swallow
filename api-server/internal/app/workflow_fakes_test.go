package app

import (
	"context"
	"time"

	serverdomain "github.com/maple52046/swallow/internal/core/server/domain"
	"github.com/maple52046/swallow/internal/core/store/storetest"
	workflowapp "github.com/maple52046/swallow/internal/core/workflow/application"
	workflowdomain "github.com/maple52046/swallow/internal/core/workflow/domain"
)

// noOwnership is a resource ownership store in which no Workflow owns anything; it only serves the
// WorkflowService's reads, so acquiring through it is refused.
type noOwnership struct{}

func (noOwnership) Acquire(context.Context, string, []string, time.Time) (bool, error) {
	return false, nil
}
func (noOwnership) Release(context.Context, string) error { return nil }
func (noOwnership) FindByOwner(context.Context, string) ([]workflowdomain.ResourceOwnership, error) {
	return nil, nil
}
func (noOwnership) IsHeld(context.Context, string) (bool, error) { return false, nil }
func (noOwnership) Owners(context.Context) ([]string, error)     { return nil, nil }

// newTestWorkflowService is a WorkflowService over workflows that accepts any plan, writes in a
// fake transaction, and reports no owned resources.
func newTestWorkflowService(workflows workflowdomain.WorkflowRepository) *workflowapp.WorkflowService {
	return workflowapp.NewWorkflowService(&storetest.Transactor{}, workflows, workflowdomain.PlanCatalog{}, noOwnership{})
}

// newTestExecutionService is an ExecutionService for planning tests: PrepareAnsibleStep checks
// targets against servers, automation, and catalog and finds every target unlocked. Its
// WorkflowService has no store, so a test that accepts automation through Create needs its own.
// It has no Local Repository resolver, so a playbook the catalog marks as using one is refused.
func newTestExecutionService(
	servers serverdomain.ServerRepository,
	automation workflowdomain.AutomationConfigurationRepository,
	catalog workflowdomain.PlaybookCatalog,
	kinds workflowdomain.WorkflowKinds,
) *workflowapp.ExecutionService {
	return workflowapp.NewExecutionService(servers, automation, catalog, kinds, nil, nil, nil, stubMutationGuard{}, newTestWorkflowService(nil), nil)
}
