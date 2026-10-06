package app

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	operationapp "github.com/maple52046/swallow/internal/operation/application"
	operationdomain "github.com/maple52046/swallow/internal/operation/domain"
	provisioningapp "github.com/maple52046/swallow/internal/provisioning/application"
	provisioningdomain "github.com/maple52046/swallow/internal/provisioning/domain"
	serverdomain "github.com/maple52046/swallow/internal/server/domain"
)

// The inspect-hardware Workflow vocabulary (contract server-enrollment.md, decision 053). Task
// kinds, the Job name, and the Task ID prefixes are published; renaming them breaks Workflows
// already persisted.
const (
	inspectionDefinition   = "hardware-inspection"
	inspectionJob          = "ensure-inspected"
	waitEnrollmentTaskKind = "wait-enrollment-settled"
	inspectTaskKind        = "inspect"
	// skipEnrollmentWaitParameter marks a requested inspection, whose operator asserts the Server
	// may boot now, so wait-enrollment-settled succeeds at once.
	skipEnrollmentWaitParameter = "skipEnrollmentWait"
	// resolveBootMediaLiveParameter makes ensure-boot-media read the Server's Boot Media when it
	// runs instead of using a URL frozen at acceptance, so Boot Media enabled after an inspection
	// stopped for attention is applied by the retry.
	resolveBootMediaLiveParameter = "resolveLive"
	// inspectionOriginParameter carries the InspectionOrigin to the inspect Task: an automatic
	// inspection of a Server the provider already brought to ready has nothing left to do, while a
	// requested one re-inspects it.
	inspectionOriginParameter = "origin"
)

// inspectionWorkflows is the slice of the Workflow service the launcher drives.
type inspectionWorkflows interface {
	Create(ctx context.Context, input operationapp.CreateWorkflowInput) (*operationapp.WorkflowItem, error)
	RetryStep(ctx context.Context, id, stepID string) error
}

// inspectionWorkflowReader lists inspect-hardware Workflows of one Server.
type inspectionWorkflowReader interface {
	List(ctx context.Context, filter operationdomain.WorkflowFilter) ([]*operationdomain.Workflow, int, error)
}

// hardwareInspectionLauncher implements provisioningapp.HardwareInspectionLauncher: it gates one
// Server and persists, or resumes, its inspect-hardware Workflow. It lives in the composition
// root because the provisioning context must not depend on Workflow persistence.
type hardwareInspectionLauncher struct {
	workflows inspectionWorkflows
	history   inspectionWorkflowReader
	servers   serverdomain.ServerRepository
	// protection re-checks the provider-owned Server Lock live before anything is accepted; the
	// inspect Task checks it again before each commission.
	protection serverdomain.MutationGuard
	// refresh live-syncs a requested Server's provisioning state before the gate, so an Inspect
	// pressed right after an out-of-band change is not judged on a stale projection. Optional.
	refresh *provisioningapp.RefreshServerUseCase
}

// LaunchInspection implements provisioningapp.HardwareInspectionLauncher. A requested inspection
// of a Server whose inspect-hardware Workflow waits for attention retries that Workflow; one that
// is still running is a busy conflict, as is any other active Workflow (reported by Create). An
// automatic request never resumes and starts only for a Server still in `new`.
func (l hardwareInspectionLauncher) LaunchInspection(ctx context.Context, request provisioningapp.InspectionRequest) (*provisioningapp.InspectionAccepted, error) {
	serverID := strings.TrimSpace(request.ServerID)
	automatic := request.Origin == provisioningdomain.InspectionOriginAutomatic
	if !automatic && l.refresh != nil {
		if _, err := l.refresh.Execute(ctx, serverID); err != nil {
			slog.Warn("refresh Server before inspection", "serverId", serverID, "error", err)
		}
	}
	server, err := l.servers.FindByID(ctx, serverID)
	if err != nil {
		return nil, err
	}
	name := server.DisplayName()
	if server.Absent || server.Provisioning == nil {
		return nil, fmt.Errorf("%w: Server %s has no observed provisioning state to inspect", provisioningdomain.ErrInspectionNotAllowed, name)
	}
	state := provisioningdomain.MachineStatus(server.Provisioning.State)
	if automatic && state != provisioningdomain.MachineStatusNew {
		return nil, fmt.Errorf("%w: Server %s is no longer new", provisioningdomain.ErrInspectionNotAllowed, name)
	}
	if decision := provisioningdomain.EvaluateInspection(state); !decision.Allowed {
		return nil, fmt.Errorf("%w: Server %s %s", provisioningdomain.ErrInspectionNotAllowed, name, decision.Reason)
	}
	if l.protection != nil {
		if err := l.protection.RequireUnlocked(ctx, []string{serverID}); err != nil {
			return nil, err
		}
	}
	active, err := l.activeInspection(ctx, serverID)
	if err != nil {
		return nil, err
	}
	if active != nil {
		if !automatic && active.Status == operationdomain.WorkflowRequiresAttention {
			if taskID := retryableInspectionTask(active); taskID != "" {
				if err := l.workflows.RetryStep(ctx, active.ID, taskID); err != nil {
					return nil, err
				}
				return inspectionAccepted(server, active.ID, true), nil
			}
		}
		return nil, fmt.Errorf("%w: Server %s is already being inspected by Workflow %s", operationdomain.ErrTargetsBusy, name, active.ID)
	}
	origin := request.Origin
	if origin == "" {
		origin = provisioningdomain.InspectionOriginRequested
	}
	created, err := l.workflows.Create(ctx, operationapp.CreateWorkflowInput{
		Kind:          operationdomain.WorkflowKindInspectHardware,
		IntentSummary: "Inspect hardware of " + name,
		IntentSnapshot: map[string]any{
			"serverId": serverID,
			"origin":   string(origin),
		},
		Definition: inspectionDefinition, DefinitionVersion: 1,
		SiteID: server.Source.SiteID, TargetServerIDs: []string{serverID},
		Steps:       inspectionSteps(server, origin),
		RequestedBy: request.RequestedBy, RequestCorrelation: request.RequestID,
	})
	if err != nil {
		return nil, err
	}
	return inspectionAccepted(server, created.ID, false), nil
}

// HasInspection implements provisioningapp.HardwareInspectionLauncher.
func (l hardwareInspectionLauncher) HasInspection(ctx context.Context, serverID string) (bool, error) {
	_, total, err := l.history.List(ctx, operationdomain.WorkflowFilter{
		ServerID: serverID, Kind: operationdomain.WorkflowKindInspectHardware, Limit: 1,
	})
	return total > 0, err
}

// activeInspection returns the Server's unfinished inspect-hardware Workflow, including one
// waiting in requires_attention, or nil.
func (l hardwareInspectionLauncher) activeInspection(ctx context.Context, serverID string) (*operationdomain.Workflow, error) {
	workflows, _, err := l.history.List(ctx, operationdomain.WorkflowFilter{
		ServerID: serverID, Kind: operationdomain.WorkflowKindInspectHardware, ActiveOnly: true, Limit: 1,
	})
	if err != nil || len(workflows) == 0 {
		return nil, err
	}
	return workflows[0], nil
}

// retryableInspectionTask names the Task to retry in an inspect-hardware Workflow waiting for
// attention. Any retryable failed Task resumes the whole ensure-inspected Job; the inspect Task is
// preferred so the retry reads as a retried inspection in the timeline.
func retryableInspectionTask(workflow *operationdomain.Workflow) string {
	found := ""
	for _, task := range workflow.Steps {
		retryable := (task.Status == operationdomain.TaskFailed || task.Status == operationdomain.TaskRequiresAttention) &&
			task.Error != nil && task.Error.Retryable
		if !retryable {
			continue
		}
		if task.Kind == inspectTaskKind {
			return task.ID
		}
		if found == "" {
			found = task.ID
		}
	}
	return found
}

// inspectionSteps builds the ensure-inspected Job: wait for the provider's enrollment, apply Boot
// Media read live, then inspect. Running it as a Job makes any retry re-run all three, so each is
// an idempotent ensure (decision 017).
func inspectionSteps(server *serverdomain.Server, origin provisioningdomain.InspectionOrigin) []operationdomain.Task {
	target := []operationdomain.ResourceReference{{Kind: "server", ID: server.ID}}
	name := server.DisplayName()
	waitID := "wait-enrollment-" + server.ID
	ensureID := ensureBootMediaTaskKind + "-" + server.ID
	return []operationdomain.Task{
		{
			ID: waitID, Kind: waitEnrollmentTaskKind, Name: "Wait for the enrollment of " + name + " to finish",
			Job: inspectionJob, Executor: operationdomain.RunnerKindProvisioner, Targets: target,
			Parameters: map[string]any{skipEnrollmentWaitParameter: origin != provisioningdomain.InspectionOriginAutomatic},
		},
		{
			ID: ensureID, Kind: ensureBootMediaTaskKind, Name: "Ensure Boot Media on " + name,
			Job: inspectionJob, Executor: operationdomain.RunnerKindInternal, Targets: target,
			DependsOn:  []string{waitID},
			Parameters: map[string]any{resolveBootMediaLiveParameter: true},
		},
		{
			ID: inspectTaskKind + "-" + server.ID, Kind: inspectTaskKind, Name: "Inspect hardware of " + name,
			Job: inspectionJob, Executor: operationdomain.RunnerKindProvisioner, Targets: target,
			DependsOn:  []string{ensureID},
			Parameters: map[string]any{inspectionOriginParameter: string(origin)},
		},
	}
}

func inspectionAccepted(server *serverdomain.Server, workflowID string, resumed bool) *provisioningapp.InspectionAccepted {
	return &provisioningapp.InspectionAccepted{
		ProvisioningStateItem: provisioningapp.ProvisioningStateItemOf(server),
		WorkflowID:            workflowID,
		Resumed:               resumed,
	}
}
