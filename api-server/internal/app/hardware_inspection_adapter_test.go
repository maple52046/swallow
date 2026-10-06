package app

import (
	"context"
	"errors"
	"testing"

	operationapp "github.com/maple52046/swallow/internal/operation/application"
	operationdomain "github.com/maple52046/swallow/internal/operation/domain"
	provisioningapp "github.com/maple52046/swallow/internal/provisioning/application"
	provisioningdomain "github.com/maple52046/swallow/internal/provisioning/domain"
	serverdomain "github.com/maple52046/swallow/internal/server/domain"
)

// fakeInspectionWorkflows records what the launcher asks of the Workflow service.
type fakeInspectionWorkflows struct {
	created *operationapp.CreateWorkflowInput
	retried [2]string
	active  []*operationdomain.Workflow
	total   int
}

func (f *fakeInspectionWorkflows) Create(_ context.Context, input operationapp.CreateWorkflowInput) (*operationapp.WorkflowItem, error) {
	f.created = &input
	return &operationapp.WorkflowItem{ID: "workflow-new"}, nil
}

func (f *fakeInspectionWorkflows) RetryStep(_ context.Context, id, stepID string) error {
	f.retried = [2]string{id, stepID}
	return nil
}

func (f *fakeInspectionWorkflows) List(_ context.Context, filter operationdomain.WorkflowFilter) ([]*operationdomain.Workflow, int, error) {
	if filter.Kind != operationdomain.WorkflowKindInspectHardware {
		return nil, 0, errors.New("unexpected kind")
	}
	if filter.ActiveOnly {
		return f.active, len(f.active), nil
	}
	return nil, f.total, nil
}

func inspectionLauncherFor(state string, workflows *fakeInspectionWorkflows) hardwareInspectionLauncher {
	server := &serverdomain.Server{
		ID: "server-id", Observed: serverdomain.Observed{Hostname: "gpu-07"},
		Source:       serverdomain.Source{SiteID: "site-id", IntegrationID: "integration-id", ProviderMachineID: "machine-id"},
		Provisioning: &serverdomain.ProvisioningStatus{State: state},
	}
	return hardwareInspectionLauncher{
		workflows: workflows, history: workflows,
		servers: &deploymentProjectionTestRepo{server: server},
	}
}

func TestHardwareInspectionLauncherStartsWorkflow(t *testing.T) {
	for _, origin := range []provisioningdomain.InspectionOrigin{provisioningdomain.InspectionOriginAutomatic, provisioningdomain.InspectionOriginRequested} {
		t.Run(string(origin), func(t *testing.T) {
			workflows := &fakeInspectionWorkflows{}
			accepted, err := inspectionLauncherFor("new", workflows).LaunchInspection(context.Background(),
				provisioningapp.InspectionRequest{ServerID: "server-id", Origin: origin, RequestedBy: "admin"})
			if err != nil {
				t.Fatalf("LaunchInspection error = %v", err)
			}
			if accepted.WorkflowID != "workflow-new" || accepted.Resumed || accepted.State != "new" {
				t.Errorf("accepted = %+v, want the new Workflow and the stored state", accepted)
			}
			created := workflows.created
			if created == nil || created.Kind != operationdomain.WorkflowKindInspectHardware || created.Definition != inspectionDefinition ||
				created.SiteID != "site-id" || len(created.TargetServerIDs) != 1 || len(created.Steps) != 3 {
				t.Fatalf("created = %+v, want one inspect-hardware Workflow with three Tasks", created)
			}
			kinds := []string{waitEnrollmentTaskKind, ensureBootMediaTaskKind, inspectTaskKind}
			for index, task := range created.Steps {
				if task.Kind != kinds[index] || task.Job != inspectionJob {
					t.Errorf("Task %d = %s in Job %q, want %s in %s", index, task.Kind, task.Job, kinds[index], inspectionJob)
				}
			}
			skip, _ := created.Steps[0].Parameters[skipEnrollmentWaitParameter].(bool)
			if want := origin == provisioningdomain.InspectionOriginRequested; skip != want {
				t.Errorf("skipEnrollmentWait = %v, want %v for %s", skip, want, origin)
			}
			if live, _ := created.Steps[1].Parameters[resolveBootMediaLiveParameter].(bool); !live {
				t.Error("ensure-boot-media must resolve Boot Media live")
			}
		})
	}
}

func TestHardwareInspectionLauncherResumesAttention(t *testing.T) {
	attention := &operationdomain.Workflow{ID: "workflow-old", Status: operationdomain.WorkflowRequiresAttention, Steps: []operationdomain.Task{
		{ID: "wait-enrollment-server-id", Kind: waitEnrollmentTaskKind, Status: operationdomain.TaskSucceeded},
		{ID: "inspect-server-id", Kind: inspectTaskKind, Status: operationdomain.TaskRequiresAttention,
			Error: &operationdomain.NormalizedError{Code: "inspect_pxe_unreached", Retryable: true}},
	}}
	workflows := &fakeInspectionWorkflows{active: []*operationdomain.Workflow{attention}}
	accepted, err := inspectionLauncherFor("new", workflows).LaunchInspection(context.Background(),
		provisioningapp.InspectionRequest{ServerID: "server-id", Origin: provisioningdomain.InspectionOriginRequested})
	if err != nil {
		t.Fatalf("LaunchInspection error = %v", err)
	}
	if !accepted.Resumed || accepted.WorkflowID != "workflow-old" || workflows.retried != [2]string{"workflow-old", "inspect-server-id"} || workflows.created != nil {
		t.Errorf("accepted = %+v, retried = %v, created = %v; want the waiting Workflow retried", accepted, workflows.retried, workflows.created)
	}

	automatic := &fakeInspectionWorkflows{active: []*operationdomain.Workflow{attention}}
	_, err = inspectionLauncherFor("new", automatic).LaunchInspection(context.Background(),
		provisioningapp.InspectionRequest{ServerID: "server-id", Origin: provisioningdomain.InspectionOriginAutomatic})
	if !errors.Is(err, operationdomain.ErrTargetsBusy) || automatic.retried != [2]string{} {
		t.Errorf("automatic error = %v, retried = %v; want busy and no retry", err, automatic.retried)
	}
}

func TestHardwareInspectionLauncherRefuses(t *testing.T) {
	running := &operationdomain.Workflow{ID: "workflow-running", Status: operationdomain.WorkflowRunning}
	cases := []struct {
		name   string
		state  string
		origin provisioningdomain.InspectionOrigin
		active []*operationdomain.Workflow
		want   error
	}{
		{"running inspection", "new", provisioningdomain.InspectionOriginRequested, []*operationdomain.Workflow{running}, operationdomain.ErrTargetsBusy},
		{"deployed Server", "deployed", provisioningdomain.InspectionOriginRequested, nil, provisioningdomain.ErrInspectionNotAllowed},
		{"in-progress Server", "deploying", provisioningdomain.InspectionOriginRequested, nil, provisioningdomain.ErrInspectionNotAllowed},
		{"automatic on a ready Server", "ready", provisioningdomain.InspectionOriginAutomatic, nil, provisioningdomain.ErrInspectionNotAllowed},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			workflows := &fakeInspectionWorkflows{active: tc.active}
			_, err := inspectionLauncherFor(tc.state, workflows).LaunchInspection(context.Background(),
				provisioningapp.InspectionRequest{ServerID: "server-id", Origin: tc.origin})
			if !errors.Is(err, tc.want) || workflows.created != nil {
				t.Errorf("error = %v, created = %v; want %v and nothing created", err, workflows.created, tc.want)
			}
		})
	}
}

func TestHardwareInspectionLauncherHistory(t *testing.T) {
	for _, total := range []int{0, 2} {
		got, err := inspectionLauncherFor("new", &fakeInspectionWorkflows{total: total}).HasInspection(context.Background(), "server-id")
		if err != nil || got != (total > 0) {
			t.Errorf("HasInspection with %d Workflows = %v, %v", total, got, err)
		}
	}
}
