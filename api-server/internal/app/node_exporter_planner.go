package app

import (
	"context"
	"fmt"
	"log/slog"

	softwareapp "github.com/maple52046/swallow/internal/core/software/application"
	workflowapp "github.com/maple52046/swallow/internal/core/workflow/application"
	workflowdomain "github.com/maple52046/swallow/internal/core/workflow/domain"
	nodeexporter "github.com/maple52046/swallow/modules/software/node-exporter"
)

// Node Exporter composition. A Workflow that provisions an OS installs the Node Exporter on each
// provisioned Server as Managed Software, the step after its time synchronization: an
// install-node-exporter-<serverId> Task runs the kind's software playbook and
// record-node-exporter-<serverId> marks the Server's Software Assignment installed. Until the
// record succeeds the install is not done: either Task ending without success marks the
// assignment failed (the software-assignment-failure completion).
//
// Both Tasks are leaves: no Task depends on them, so a failed exporter install never holds back the
// OS or the Platform, though it leaves the Workflow requires_attention until the user retries it.
const (
	nodeExporterInstallPrefix = "install-node-exporter-"
	nodeExporterRecordPrefix  = "record-node-exporter-"
)

// nodeExporterPlanner builds the Node Exporter Tasks of a Workflow being accepted and writes their
// pending Software Assignments once the Workflow exists.
//
// Its zero value (no software service) adds nothing, so launchers wired without it, in tests, keep
// their previous Workflow shape. Wired, the install Tasks are prepared through the same ansible
// acceptance checks as any other (automation, catalog, lock, policy), and the install through the
// software service's composed-install rules.
type nodeExporterPlanner struct {
	software   *softwareapp.SoftwareService
	operations *workflowapp.ExecutionService
}

// nodeExporterInstall is the Node Exporter part of an accepted plan: its Tasks and the install
// whose pending assignments are recorded after the Workflow is written.
type nodeExporterInstall struct {
	tasks   []workflowdomain.Task
	install *softwareapp.ComposedInstall
}

// installTasks builds one install and one record Task per Server in after, each install depending
// on the Task after names for its Server (its configure-ntp, or its provision-os when the
// launcher adds no time Task). job is the Job both Tasks belong to; "" for a job-less Workflow
// (Deploy OS). It returns the zero value when the planner is unwired or after is empty, and the
// software service's or the ansible acceptance's error otherwise.
func (p nodeExporterPlanner) installTasks(
	ctx context.Context,
	operationID, requestedBy, job string,
	serverIDs []string,
	after map[string]string,
) (nodeExporterInstall, error) {
	if p.software == nil || p.operations == nil || len(serverIDs) == 0 {
		return nodeExporterInstall{}, nil
	}
	targets := make([]softwareapp.InstallTarget, len(serverIDs))
	for i, serverID := range serverIDs {
		targets[i] = softwareapp.InstallTarget{ServerID: serverID}
	}
	install, err := p.software.PlanComposedInstall(ctx, softwareapp.InstallInput{
		Kind: nodeexporter.Kind, Targets: targets, Spec: map[string]any{}, RequestedBy: requestedBy,
	})
	if err != nil {
		return nodeExporterInstall{}, fmt.Errorf("plan the %s install: %w", p.software.Label(nodeexporter.Kind), err)
	}
	label := p.software.Label(nodeexporter.Kind)
	playbook := installPlaybook(nodeexporter.Kind)
	tasks := make([]workflowdomain.Task, 0, 2*len(serverIDs))
	for _, serverID := range serverIDs {
		prepared, err := p.operations.PrepareAnsibleStep(ctx, workflowapp.CreateExecutionInput{
			Kind: string(playbook.workflowKind), PlaybookName: playbook.playbook, Intent: "Install " + label,
			TargetServerIDs: []string{serverID}, TrustedVars: install.TrustedVars, RequestedBy: requestedBy,
		}, operationID, true)
		if err != nil {
			return nodeExporterInstall{}, err
		}
		installID := nodeExporterInstallPrefix + serverID
		tasks = append(tasks,
			workflowdomain.Task{
				ID: installID, Kind: "ansible-playbook", Name: "Install " + label,
				Job:        job,
				Executor:   workflowdomain.RunnerKindHostAutomation,
				DependsOn:  []string{after[serverID]},
				Targets:    prepared.Targets,
				Parameters: map[string]any{"playbook": prepared.Playbook, "extraVars": prepared.ExtraVars},
				Completion: []workflowdomain.Completion{softwareFailure(nodeexporter.Kind, []string{serverID})},
			},
			workflowdomain.Task{
				ID: nodeExporterRecordPrefix + serverID, Kind: softwareStepKindRecord, Name: "Record " + label + " assignment",
				Job:        job,
				Executor:   workflowdomain.RunnerKindInternal,
				DependsOn:  []string{installID},
				Targets:    serverResourceRefs([]string{serverID}),
				Parameters: map[string]any{"softwareKind": string(nodeexporter.Kind), "serverIds": []string{serverID}},
				Completion: []workflowdomain.Completion{softwareFailure(nodeexporter.Kind, []string{serverID})},
			},
		)
	}
	return nodeExporterInstall{tasks: tasks, install: &install}, nil
}

// record writes the pending Software Assignments of an accepted plan's Node Exporter install. A
// write failure is logged rather than undoing an accepted Workflow: the record Task tolerates a
// missing assignment, so the cost is a missing row, not a failed deployment.
func (p nodeExporterPlanner) record(ctx context.Context, planned nodeExporterInstall, workflowID string) {
	if planned.install == nil {
		return
	}
	if err := p.software.RecordComposedInstall(ctx, *planned.install, workflowID); err != nil {
		slog.Error("record the Node Exporter's pending Software Assignments", "operationId", workflowID, "error", err)
	}
}
