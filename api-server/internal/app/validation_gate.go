package app

import (
	"context"
	"errors"
	"fmt"

	platformdomain "github.com/maple52046/swallow/internal/core/platform/domain"
	validationapp "github.com/maple52046/swallow/internal/core/validation/application"
	validationdomain "github.com/maple52046/swallow/internal/core/validation/domain"
	workflowdomain "github.com/maple52046/swallow/internal/core/workflow/domain"
)

// slurmAcceptanceSuite is the Validation Suite every Slurm deployment composes as its gate
// (validation.md §5), so a Slurm Platform whose GPUs, containers, or MPI do not work is not deployed
// (decisions 063 and 066).
const slurmAcceptanceSuite = "slurm-acceptance"

// The gate's Task ids: its suite's Tasks are prefixed so they cannot collide with the deployment's
// own, and its complete-validation Task is the one the deployment's later Jobs depend on.
const (
	gateTaskPrefix     = "gate-"
	gateCompleteTaskID = "gate-complete-validation"
)

// validationGate composes a module suite into a Platform deployment as its gate (validation.md §5):
// the suite's Jobs and a complete-validation Task in the suite's last Job join the deployment's own
// Workflow, so retrying a failed gate reruns the checks; the gate run is written with the Workflow,
// in the deployment's acceptance transaction.
type validationGate struct {
	service  *validationapp.Service
	launcher validationLauncher
}

// composedGate is a gate planned into a deployment: its run (written by PersistDeployment), its
// Tasks and labelled Jobs, and the Task the deployment's later Jobs depend on.
type composedGate struct {
	run   validationdomain.Run
	steps []workflowdomain.Task
	jobs  []workflowdomain.JobDeclaration
	last  string
}

// gateRequest is one gate: its suite, the Platform's members by role, the Servers its suite enters
// through, and the run's parameters.
type gateRequest struct {
	suiteID    string
	siteID     string
	access     []string
	members    []validationapp.PlatformMember
	parameters map[string]any
}

// compose plans the gate after the deployment's Tasks after (validation.md §5). provisionFirst lets
// its checks target Servers the deployment provisions first.
func (g *validationGate) compose(ctx context.Context, launch platformdomain.DeploymentLaunch, operationID string,
	request gateRequest, after []string, provisionFirst bool,
) (*composedGate, error) {
	if g == nil || g.service == nil {
		return nil, errors.New("the deployment's validation gate is not available in this process")
	}
	memberIDs := make([]string, 0, len(request.members))
	for _, member := range request.members {
		memberIDs = append(memberIDs, member.ServerID)
	}
	run, suite, err := g.service.GateRun(validationapp.GateRequest{
		SuiteID: request.suiteID, PlatformID: launch.Platform.ID, SiteID: request.siteID, WorkflowID: operationID,
		AccessIDs: request.access, MemberIDs: memberIDs, Parameters: request.parameters, RequestedBy: launch.RequestedBy,
	})
	if err != nil {
		return nil, fmt.Errorf("plan the %s gate: %w", request.suiteID, err)
	}
	if len(suite.Template.Jobs) == 0 {
		return nil, fmt.Errorf("plan the %s gate: its template declares no Job to complete in", request.suiteID)
	}
	compiled, err := suite.Template.Compile(g.launcher.catalog, workflowdomain.TemplateInput{
		IDPrefix: gateTaskPrefix, Access: request.access, Members: memberIDs, After: after,
	})
	if err != nil {
		return nil, fmt.Errorf("plan the %s gate: %w", request.suiteID, err)
	}
	platform := &validationapp.TargetPlatform{
		ID: launch.Platform.ID, Name: launch.Platform.Name, SiteID: request.siteID, Type: string(launch.Platform.Type),
		Active: true, Members: request.members, AccessServerIDs: request.access,
	}
	steps, err := g.launcher.prepareChecks(ctx, operationID, run, suite, platform, compiled.Tasks, provisionFirst)
	if err != nil {
		return nil, err
	}
	last := suite.Template.Jobs[len(suite.Template.Jobs)-1].Name
	steps = append(steps, completeValidationTask(gateCompleteTaskID, last, run, steps))
	return &composedGate{run: run, steps: steps, jobs: compiled.Jobs, last: gateCompleteTaskID}, nil
}

// slurmGateRequest is the slurm-acceptance gate of a Slurm deployment: entered through the primary
// controller, which answers for the cluster as soon as the install Task ends, over every member by
// its Slurm roles, expecting GPU GRES on the GPU compute nodes the deployment mapped.
func slurmGateRequest(launch platformdomain.DeploymentLaunch, siteID string) gateRequest {
	vars := launch.TrustedVars
	roles := map[string][]string{}
	var order []string
	for _, group := range []struct{ ids, role string }{
		{"swallow_slurm_controller_ids", "controller"}, {"swallow_slurm_compute_ids", "compute"}, {"swallow_slurm_login_ids", "login"},
	} {
		for _, id := range decodeStringSlice(vars[group.ids]) {
			if roles[id] == nil {
				order = append(order, id)
			}
			roles[id] = append(roles[id], group.role)
		}
	}
	members := make([]validationapp.PlatformMember, 0, len(order))
	for _, id := range order {
		members = append(members, validationapp.PlatformMember{ServerID: id, Roles: roles[id]})
	}
	primary, _ := vars["swallow_slurm_primary_controller_id"].(string)
	parameters := map[string]any{}
	if gpus := decodeStringSlice(vars["swallow_slurm_gpu_compute_ids"]); len(gpus) > 0 {
		parameters["gpuServers"] = gpus
	}
	return gateRequest{suiteID: slurmAcceptanceSuite, siteID: siteID, access: []string{primary}, members: members, parameters: parameters}
}

// labelGateJobs gives the gate's Jobs in jobs the labels and retry granularity its template declares.
func labelGateJobs(jobs []workflowdomain.JobDeclaration, gate *composedGate) []workflowdomain.JobDeclaration {
	declared := map[string]workflowdomain.JobDeclaration{}
	for _, job := range gate.jobs {
		declared[job.Name] = job
	}
	for i, job := range jobs {
		if gateJob, ok := declared[job.Name]; ok {
			jobs[i] = gateJob
		}
	}
	return jobs
}
