package app

import (
	"context"
	"strings"

	platformdomain "github.com/maple52046/swallow/internal/core/platform/domain"
	provisioningdomain "github.com/maple52046/swallow/internal/core/provisioning/domain"
	workflowapp "github.com/maple52046/swallow/internal/core/workflow/application"
	workflowdomain "github.com/maple52046/swallow/internal/core/workflow/domain"
)

// Time synchronization (decision 065). Every provision-os Task is followed by a configure-ntp Task
// that points the new Server at the installation's NTP server. A Platform deployment never
// configures time: its own install playbook checks every node's clock first, with the variables
// checkVars returns.
const (
	// ntpStepPrefix names the time-sync Task of one provisioned Server: configure-ntp-<serverId>,
	// depending only on provision-<serverId>, so one failed deployment does not keep its
	// batch-mates from being configured.
	ntpStepPrefix = "configure-ntp-"
	// ntpPlaybook is the release-manifest playbook the Tasks run; launchers name it directly, so a
	// Site needs no playbookMappings entry.
	ntpPlaybook = "configure-ntp"
	// ntpServersVar carries the installation's NTP server, frozen at acceptance, as a trusted
	// variable the operator cannot forge. It is a list so the playbooks stay ready for more than one
	// server without a contract change.
	ntpServersVar = "swallow_ntp_servers"
	// timeSyncCheckModeVar is enforce or warn; timeSyncMaxOffsetVar is the largest offset in seconds
	// from the NTP server the platform playbooks' time_sync_check role accepts.
	timeSyncCheckModeVar = "swallow_time_sync_check"
	timeSyncMaxOffsetVar = "swallow_time_sync_max_offset"
	// timeSyncMaxOffsetSeconds is the check's bound. Servers that follow the same NTP server agree
	// to milliseconds, so a second only trips on a clock that is not synchronized at all, while
	// staying well inside what MUNGE credentials and etcd leases tolerate.
	timeSyncMaxOffsetSeconds = 1.0
)

// ntpStepPlanner builds the configure-ntp Tasks of a deployment Workflow being accepted and the
// time-check variables of a Platform's install step.
//
// server is the installation's NTP server address (config NTPServer, SWALLOW_API_NTP_SERVER). Its
// zero value (no operations service) adds nothing, so launchers wired without it, in tests or on
// the legacy path, keep their previous Workflow shape. Wired, a Workflow that provisions an OS
// always gets configure-ntp: an empty server is provisioningdomain.ErrNTPServerMissing, and
// preparing the Tasks applies the same automation, catalog, lock, and policy checks as any ansible
// Task, so a Site whose automation cannot run refuses the deployment at acceptance.
type ntpStepPlanner struct {
	server     string
	operations *workflowapp.ExecutionService
}

// ntpTaskInput places one time Task inside the Workflow being accepted.
type ntpTaskInput struct {
	OperationID string
	StepID      string
	// Job is the Job the Task belongs to; empty for a Workflow whose Tasks carry no Job (Deploy OS),
	// because a jobbed Task among job-less ones would split the Workflow's execution path.
	Job       string
	ServerIDs []string
	DependsOn []string
	// ProvisionFirst lets a preceding provision-os Task satisfy the deployed-state requirement.
	ProvisionFirst bool
	RequestedBy    string
}

// servers returns the trusted swallow_ntp_servers value: the installation's NTP server, or an
// empty list when none is configured.
func (p ntpStepPlanner) servers() []string {
	if server := strings.TrimSpace(p.server); server != "" {
		return []string{server}
	}
	return []string{}
}

// configureTasks builds one configure-ntp-<serverId> Task per provisioned Server, each depending
// only on that Server's provision-<serverId>, which already proved SSH. It returns
// provisioningdomain.ErrNTPServerMissing when the installation has no NTP server, and nil when
// the planner is unwired or there is nothing to provision.
func (p ntpStepPlanner) configureTasks(ctx context.Context, operationID, requestedBy, job string, provisionedServerIDs []string) ([]workflowdomain.Task, error) {
	if p.operations == nil || len(provisionedServerIDs) == 0 {
		return nil, nil
	}
	servers := p.servers()
	if len(servers) == 0 {
		return nil, provisioningdomain.ErrNTPServerMissing
	}
	tasks := make([]workflowdomain.Task, 0, len(provisionedServerIDs))
	for _, serverID := range provisionedServerIDs {
		task, err := p.task(ctx, ntpTaskInput{
			OperationID: operationID, StepID: ntpStepPrefix + serverID, Job: job,
			ServerIDs: []string{serverID}, DependsOn: []string{"provision-" + serverID},
			ProvisionFirst: true, RequestedBy: requestedBy,
		}, workflowdomain.WorkflowKindConfigureNTP, ntpPlaybook, "Configure time synchronization",
			map[string]any{ntpServersVar: servers})
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, *task)
	}
	return tasks, nil
}

// withCheckVars returns trustedVars plus the variables of the time_sync_check role a Platform's
// install playbook runs before anything else, leaving trustedVars unchanged. It needs no configured
// NTP server: without one the check verifies only that each node reports a synchronized clock.
func (p ntpStepPlanner) withCheckVars(trustedVars map[string]any, mode platformdomain.TimeSyncCheck) map[string]any {
	if mode == "" {
		mode = platformdomain.TimeSyncCheckEnforce
	}
	merged := make(map[string]any, len(trustedVars)+3)
	for key, value := range trustedVars {
		merged[key] = value
	}
	merged[ntpServersVar] = p.servers()
	merged[timeSyncCheckModeVar] = string(mode)
	merged[timeSyncMaxOffsetVar] = timeSyncMaxOffsetSeconds
	return merged
}

// task prepares one ansible Task through the ExecutionService so it gets the same acceptance
// checks as any other ansible Task; its errors are those acceptance errors.
func (p ntpStepPlanner) task(
	ctx context.Context,
	input ntpTaskInput,
	kind workflowdomain.WorkflowKind,
	playbook, name string,
	trustedVars map[string]any,
) (*workflowdomain.Task, error) {
	prepared, err := p.operations.PrepareAnsibleStep(ctx, workflowapp.CreateExecutionInput{
		Kind: string(kind), PlaybookName: playbook, Intent: name,
		TargetServerIDs: input.ServerIDs, TrustedVars: trustedVars, RequestedBy: input.RequestedBy,
	}, input.OperationID, input.ProvisionFirst)
	if err != nil {
		return nil, err
	}
	return &workflowdomain.Task{
		ID: input.StepID, Kind: "ansible-playbook", Name: name,
		Job:        input.Job,
		Executor:   workflowdomain.RunnerKindAnsible,
		DependsOn:  append([]string(nil), input.DependsOn...),
		Targets:    prepared.Targets,
		Parameters: map[string]any{"playbook": prepared.Playbook, "extraVars": prepared.ExtraVars},
	}, nil
}

// isTimeTask reports whether an ansible Task id is a configure-ntp Task, which has nothing for a
// Platform's completion observer to record.
func isTimeTask(stepID string) bool {
	return strings.HasPrefix(stepID, ntpStepPrefix)
}
