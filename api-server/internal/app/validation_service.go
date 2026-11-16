package app

import (
	"time"

	"go.mongodb.org/mongo-driver/mongo"

	authstore "github.com/maple52046/swallow/internal/adapters/store/mongodb/auth"
	validationstore "github.com/maple52046/swallow/internal/adapters/store/mongodb/validation"
	platformdomain "github.com/maple52046/swallow/internal/core/platform/domain"
	serverdomain "github.com/maple52046/swallow/internal/core/server/domain"
	softwaredomain "github.com/maple52046/swallow/internal/core/software/domain"
	"github.com/maple52046/swallow/internal/core/store"
	validationapp "github.com/maple52046/swallow/internal/core/validation/application"
	workflowapp "github.com/maple52046/swallow/internal/core/workflow/application"
)

// validationDeps are what a process's Validation service is built from: the store, the build's
// modules, the Workflow acceptance services the launcher plans through, and the Server and Platform
// stores the targets are read from.
type validationDeps struct {
	db             *mongo.Database
	transactor     store.Transactor
	modules        composedModules
	operations     *workflowapp.ExecutionService
	orchestrations *workflowapp.WorkflowService
	servers        serverdomain.ServerRepository
	platforms      platformdomain.PlatformRepository
	lifecycle      platformdomain.LifecycleReader
	retention      time.Duration
	// ntpServer is the installation's NTP server, which host checks measure the clock against.
	ntpServer string
	// directoryConfigured, access, and assignments answer what a User may do on a Server through the
	// User's own account, which accepting a user suite run checks (validationStarters).
	directoryConfigured bool
	access              accessStores
	assignments         softwaredomain.AssignmentRepository
}

// newValidationService builds the Validation service the API serves and the worker's schedule
// controller and Platform deployments start runs through (decision 081). The API and the worker
// build it the same way here.
func newValidationService(deps validationDeps) (*validationapp.Service, error) {
	runs, err := validationstore.NewMongoRunRepo(deps.db)
	if err != nil {
		return nil, err
	}
	schedules, err := validationstore.NewMongoScheduleRepo(deps.db)
	if err != nil {
		return nil, err
	}
	users := validationstore.NewMongoSuiteRepo(deps.db)
	catalog, err := validationapp.NewCatalog(deps.modules.validations, users)
	if err != nil {
		return nil, err
	}
	return validationapp.NewService(validationapp.ServiceDeps{
		Transactor: deps.transactor, Catalog: catalog, Users: users, Runs: runs, Schedules: schedules,
		Targets: validationTargets{servers: deps.servers, platforms: deps.platforms, lifecycle: deps.lifecycle,
			modules: deps.modules.platforms},
		Starters: validationStarters{
			directoryConfigured: deps.directoryConfigured, users: authstore.NewMongoUserRepo(deps.db),
			memberships: deps.access.memberships, policies: deps.access.policies, settings: deps.access.settings,
			servers: deps.servers, assignments: deps.assignments, directoryClient: directoryClientKind(deps.modules.software),
			limits: directoryInputs{servers: deps.servers, platforms: deps.platforms, modules: deps.modules.registry},
		},
		Launcher: validationLauncher{operations: deps.operations, workflows: deps.orchestrations,
			catalog: workflowPlanCatalog(deps.modules.workflowKinds, deps.modules.software), registry: deps.modules.registry,
			ntp: ntpStepPlanner{server: deps.ntpServer}},
		Canceler:  validationWorkflowCanceler{workflows: deps.orchestrations},
		Retention: deps.retention,
	})
}

// newValidationRecorder builds the Recorder the worker's and the Ansible executor's completion
// handlers fold a run's Task transitions and results into.
func newValidationRecorder(db *mongo.Database, transactor store.Transactor) (*validationapp.Recorder, error) {
	runs, err := validationstore.NewMongoRunRepo(db)
	if err != nil {
		return nil, err
	}
	return validationapp.NewRecorder(transactor, runs)
}
