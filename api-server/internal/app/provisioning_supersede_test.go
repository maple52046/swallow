package app

import (
	"context"
	"errors"
	"strings"
	"testing"

	provisioningapp "github.com/maple52046/swallow/internal/core/provisioning/application"
	provisioningdomain "github.com/maple52046/swallow/internal/core/provisioning/domain"
	serverdomain "github.com/maple52046/swallow/internal/core/server/domain"
	"github.com/maple52046/swallow/internal/core/store/storetest"
	workflowapp "github.com/maple52046/swallow/internal/core/workflow/application"
	workflowdomain "github.com/maple52046/swallow/internal/core/workflow/domain"
)

// supersedeWorkflowRepo serves one parked Workflow holding the target Server, the way the store
// still reports it right after a cancel request (the Workflow writes canceling itself), records the
// cancel commands it receives and the Operation a launch creates. refuse makes a cancel request
// fail, and finish models a Workflow that ended on its own between the list and the cancel.
type supersedeWorkflowRepo struct {
	workflowdomain.WorkflowRepository
	parked   *workflowdomain.Workflow
	created  *workflowdomain.Workflow
	canceled []string
	refuse   bool
	finish   bool
}

func (r *supersedeWorkflowRepo) RequestCancel(_ context.Context, id string, _ workflowdomain.CancelCommand) error {
	r.canceled = append(r.canceled, id)
	if r.finish {
		r.parked.Status = workflowdomain.WorkflowSucceeded
	}
	if r.refuse {
		return workflowdomain.ErrWorkflowControlConflict
	}
	return nil
}

func (r *supersedeWorkflowRepo) FindByID(_ context.Context, id string) (*workflowdomain.Workflow, error) {
	if id == r.parked.ID {
		return r.parked, nil
	}
	return nil, workflowdomain.ErrWorkflowNotFound
}

func (r *supersedeWorkflowRepo) List(_ context.Context, filter workflowdomain.WorkflowFilter) ([]*workflowdomain.Workflow, int, error) {
	if filter.ActiveOnly && filter.ServerID == "server-id" && !r.parked.Status.Terminal() {
		return []*workflowdomain.Workflow{r.parked}, 1, nil
	}
	return nil, 0, nil
}

func (r *supersedeWorkflowRepo) Create(_ context.Context, operation *workflowdomain.Workflow) error {
	r.created = operation
	return nil
}

func (r *supersedeWorkflowRepo) AppendEvent(context.Context, workflowdomain.TimelineEvent) error {
	return nil
}

func supersedeLauncher() (durableProvisioningLauncher, *supersedeWorkflowRepo, *supersedeWorkflowRepo) {
	repo := &supersedeWorkflowRepo{parked: &workflowdomain.Workflow{
		ID: "op-parked", SchemaVersion: 4, Kind: workflowdomain.WorkflowKindDeployKubernetes,
		Intent: map[string]any{"summary": "Deploy k0s Platform test-amd-operator"},
		Status: workflowdomain.WorkflowRequiresAttention, TargetServerIDs: []string{"server-id"},
	}}
	server := &serverdomain.Server{
		ID: "server-id", Observed: serverdomain.Observed{Hostname: "lab-afde-mi308-3"},
		Source:       serverdomain.Source{SiteID: "site-id", IntegrationID: "integration-id", ProviderMachineID: "machine-id"},
		Provisioning: &serverdomain.ProvisioningStatus{State: "failed"},
	}
	return durableProvisioningLauncher{
		operations: workflowapp.NewWorkflowService(&storetest.Transactor{}, repo, workflowdomain.PlanCatalog{}),
		servers:    &deploymentProjectionTestRepo{server: server},
	}, repo, repo
}

// TestRecoveryLaunchSupersedesActiveWorkflows covers decision 061: the Release dialog used to
// cancel the Workflow holding the Server and poll up to 30 seconds with every control disabled
// before it could release. Asked to, the backend now cancels that Workflow and creates the
// Release at once; the Release waits for the canceled Workflow's leases instead of the browser.
func TestRecoveryLaunchSupersedesActiveWorkflows(t *testing.T) {
	release := []provisioningapp.ReleaseServerInput{{ServerID: "server-id"}}
	recoverInputs := []provisioningapp.RecoverServerInput{{ServerID: "server-id"}}
	cancelActive := provisioningapp.RecoveryLaunchOptions{CancelActiveWorkflows: true}
	launches := map[string]func(durableProvisioningLauncher, provisioningapp.RecoveryLaunchOptions) error{
		"release": func(l durableProvisioningLauncher, options provisioningapp.RecoveryLaunchOptions) error {
			_, err := l.LaunchRelease(context.Background(), release, options, "admin", "request-id")
			return err
		},
		"recover": func(l durableProvisioningLauncher, options provisioningapp.RecoveryLaunchOptions) error {
			_, err := l.LaunchRecover(context.Background(), recoverInputs, options, "admin", "request-id")
			return err
		},
	}
	for name, launch := range launches {
		t.Run(name+" without the option stays refused as busy", func(t *testing.T) {
			launcher, repo, controller := supersedeLauncher()
			err := launch(launcher, provisioningapp.RecoveryLaunchOptions{})
			if !errors.Is(err, workflowdomain.ErrTargetsBusy) || repo.created != nil || len(controller.canceled) != 0 {
				t.Fatalf("err = %v, created %v, canceled %v; want busy, nothing created or canceled", err, repo.created != nil, controller.canceled)
			}
		})
		t.Run(name+" cancels the holder and is created at once", func(t *testing.T) {
			launcher, repo, controller := supersedeLauncher()
			if err := launch(launcher, cancelActive); err != nil {
				t.Fatalf("launch error = %v", err)
			}
			if len(controller.canceled) != 1 || controller.canceled[0] != "op-parked" {
				t.Errorf("canceled = %v, want op-parked", controller.canceled)
			}
			if repo.created == nil || len(repo.created.TargetServerIDs) != 1 {
				t.Fatalf("created = %+v, want one Operation for the Server", repo.created)
			}
		})
		t.Run(name+" refused cancel of running work creates nothing", func(t *testing.T) {
			launcher, repo, controller := supersedeLauncher()
			controller.refuse = true
			err := launch(launcher, cancelActive)
			if !errors.Is(err, workflowdomain.ErrTargetsBusy) || !strings.Contains(err.Error(), "Deploy k0s Platform test-amd-operator") {
				t.Fatalf("err = %v, want a busy conflict naming the Workflow", err)
			}
			if repo.created != nil {
				t.Error("an Operation was created behind a Workflow that would not stop")
			}
		})
		t.Run(name+" of a Platform's deployed Server is refused even with the cancel option", func(t *testing.T) {
			launcher, repo, controller := supersedeLauncher()
			server := launcher.servers.(*deploymentProjectionTestRepo).server
			server.Provisioning.State = "deployed"
			server.PlatformClaim = &serverdomain.PlatformClaim{PlatformID: "platform-a"}
			launcher.platformNames = staticPlatformNames{"platform-a": "production-k0s"}
			err := launch(launcher, cancelActive)
			var claimed *provisioningdomain.ServerClaimedByPlatformError
			if !errors.As(err, &claimed) || !strings.Contains(err.Error(), "Platform production-k0s") {
				t.Fatalf("err = %v, want a ServerClaimedByPlatformError naming production-k0s", err)
			}
			if repo.created != nil || len(controller.canceled) != 0 {
				t.Errorf("created %v, canceled %v; a refused claim must create and cancel nothing", repo.created != nil, controller.canceled)
			}
		})
		t.Run(name+" refused cancel of finished work proceeds", func(t *testing.T) {
			launcher, repo, controller := supersedeLauncher()
			controller.refuse, controller.finish = true, true
			if err := launch(launcher, cancelActive); err != nil {
				t.Fatalf("launch error = %v, want the race with a natural finish to be accepted", err)
			}
			if repo.created == nil {
				t.Error("no Operation was created after the holder finished")
			}
		})
	}
}

// Recover is refused for a Platform's Server only when it is deployed (recovering it would release
// it); recovering a Platform's failed node back to Ready is how a broken deployment is repaired.
func TestRecoverOfAPlatformsFailedServerIsAllowed(t *testing.T) {
	launcher, repo, _ := supersedeLauncher()
	server := launcher.servers.(*deploymentProjectionTestRepo).server
	server.PlatformClaim = &serverdomain.PlatformClaim{PlatformID: "platform-a"}
	_, err := launcher.LaunchRecover(context.Background(), []provisioningapp.RecoverServerInput{{ServerID: "server-id"}},
		provisioningapp.RecoveryLaunchOptions{CancelActiveWorkflows: true}, "admin", "request-id")
	if err != nil {
		t.Fatalf("LaunchRecover() error = %v, want a failed Platform node to be recoverable", err)
	}
	if repo.created == nil {
		t.Error("no recover Operation was created")
	}
}

// staticPlatformNames resolves Platform ids from a map, for refusal messages in tests.
type staticPlatformNames map[string]string

func (n staticPlatformNames) PlatformName(_ context.Context, platformID string) (string, string, error) {
	name, ok := n[platformID]
	if !ok {
		return "", "", errors.New("unknown platform")
	}
	return name, "kubernetes", nil
}
