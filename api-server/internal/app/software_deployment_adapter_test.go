package app

import (
	"context"
	"fmt"
	"path/filepath"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/maple52046/swallow/internal/adapters/hostautomation/ansible"
	softwareapp "github.com/maple52046/swallow/internal/core/software/application"
	softwaredomain "github.com/maple52046/swallow/internal/core/software/domain"
	workflowapp "github.com/maple52046/swallow/internal/core/workflow/application"
	workflowdomain "github.com/maple52046/swallow/internal/core/workflow/domain"
)

// Every catalog kind must install and uninstall through a playbook the shipped manifest registers,
// under a Workflow kind the build accepts that requires a deployed host: a kind added to the catalog
// without both would be accepted by the API and then refused at launch.
func TestEverySoftwareKindHasRegisteredPlaybooks(t *testing.T) {
	modulesDir := filepath.Join("..", "..", "modules")
	catalog, err := ansible.LoadManifestCatalog(filepath.Join(modulesDir, "manifest.json"), modulesDir)
	if err != nil {
		t.Fatalf("load shipped manifest: %v", err)
	}
	modules := testModules(t)
	for _, entry := range modules.software.Entries() {
		for mode, mapping := range map[string]softwarePlaybook{
			"install": installPlaybook(entry.Kind), "uninstall": uninstallPlaybook(entry.Kind),
		} {
			if _, err := catalog.Resolve(mapping.playbook); err != nil {
				t.Errorf("%s %s playbook %q: %v", entry.Kind, mode, mapping.playbook, err)
			}
			if !modules.workflowKinds.Valid(mapping.workflowKind) || modules.workflowKinds.RequiredProvisioningState(mapping.workflowKind) != "deployed" {
				t.Errorf("%s %s Workflow kind %q must be valid and require deployed", entry.Kind, mode, mapping.workflowKind)
			}
		}
	}
}

// recordingAssignments is an Assignment repository over a map keyed by serverId, all of one kind;
// SetState records each write.
type recordingAssignments struct {
	softwaredomain.AssignmentRepository
	byServer map[string]*softwaredomain.Assignment
	writes   []string
}

func (r *recordingAssignments) FindByServerAndKind(_ context.Context, serverID string, _ softwaredomain.Kind) (*softwaredomain.Assignment, error) {
	if assignment, ok := r.byServer[serverID]; ok {
		return assignment, nil
	}
	return nil, softwaredomain.ErrAssignmentNotFound
}

func (r *recordingAssignments) SetState(_ context.Context, serverID string, kind softwaredomain.Kind, state softwaredomain.AssignmentState, workflowID string, appliedAt *time.Time) error {
	r.writes = append(r.writes, fmt.Sprintf("%s/%s=%s@%s applied=%v", serverID, kind, state, workflowID, appliedAt != nil))
	return nil
}

// A Slurm uninstall retires only live exporter records, keeping each one's last Workflow; a Server
// without a record or with an absent one is left alone.
func TestDeviceMetricsExporterAssignmentsMarkLiveRecordsAbsent(t *testing.T) {
	repo := &recordingAssignments{byServer: map[string]*softwaredomain.Assignment{
		"s1": {ServerID: "s1", Kind: softwaredomain.KindDeviceMetricsExporter, State: softwaredomain.StateInstalled, LastWorkflowID: "deploy-1"},
		"s2": {ServerID: "s2", Kind: softwaredomain.KindDeviceMetricsExporter, State: softwaredomain.StateAbsent, LastWorkflowID: "deploy-0"},
	}}
	if err := (deviceMetricsExporterAssignments{assignments: repo}).MarkDeviceMetricsExporterRemoved(context.Background(), []string{"s1", "s2", "s3"}); err != nil {
		t.Fatalf("MarkDeviceMetricsExporterRemoved() error = %v", err)
	}
	if want := []string{"s1/device-metrics-exporter=absent@deploy-1 applied=false"}; !slices.Equal(repo.writes, want) {
		t.Errorf("writes = %v, want %v", repo.writes, want)
	}
}

// A software Workflow's configure Job is labelled with what it does, since install and uninstall
// share its name.
func TestSoftwareWorkflowLabelsItsConfigureJobByDirection(t *testing.T) {
	launcher := softwareDeploymentLauncher{
		operations:     newTestExecutionService(slurmTestServers{}, ntpTestAutomation{}, slurmTestCatalog{}, testModules(t).workflowKinds),
		orchestrations: newTestWorkflowService(nil),
		modules:        testModules(t).registry,
	}
	launch := softwareapp.SoftwareLaunch{Kind: softwaredomain.KindNFS, Label: "NFS", ServerIDs: []string{"s1"}, RequestedBy: "admin"}
	for want, prepare := range map[string]func(context.Context, softwareapp.SoftwareLaunch) (softwareapp.PreparedSoftwareWorkflow, error){
		"Install NFS": launcher.PrepareInstall, "Uninstall NFS": launcher.PrepareUninstall,
	} {
		prepared, err := prepare(context.Background(), launch)
		if err != nil {
			t.Fatalf("prepare %s: %v", want, err)
		}
		var got string
		for _, job := range prepared.(*preparedSoftwareWorkflow).workflow.Jobs {
			if job.Name == "configure-nfs" {
				got = job.Label
			}
		}
		if got != want {
			t.Errorf("configure-nfs label = %q, want %q", got, want)
		}
	}
}

// An install is failed until its record Task succeeds, so both the install Task and the record Task
// declare the failure marking; an uninstall declares none, leaving its Assignments uninstalling
// for a retry.
func TestSoftwareInstallRecordTaskMarksTheAssignmentFailed(t *testing.T) {
	launcher := softwareDeploymentLauncher{
		operations:     newTestExecutionService(slurmTestServers{}, ntpTestAutomation{}, slurmTestCatalog{}, testModules(t).workflowKinds),
		orchestrations: newTestWorkflowService(nil),
		modules:        testModules(t).registry,
	}
	launch := softwareapp.SoftwareLaunch{Kind: softwaredomain.KindPodman, Label: "Podman", ServerIDs: []string{"s1"}, RequestedBy: "admin"}
	want := []workflowdomain.Completion{softwareFailure(softwaredomain.KindPodman, []string{"s1"})}
	for mode, prepare := range map[string]func(context.Context, softwareapp.SoftwareLaunch) (softwareapp.PreparedSoftwareWorkflow, error){
		"install": launcher.PrepareInstall, "uninstall": launcher.PrepareUninstall,
	} {
		prepared, err := prepare(context.Background(), launch)
		if err != nil {
			t.Fatalf("prepare %s: %v", mode, err)
		}
		for _, task := range prepared.(*preparedSoftwareWorkflow).workflow.Steps {
			if task.ID != softwareInstallStepID && task.ID != softwareRecordStepID {
				continue
			}
			if mode == "install" && !reflect.DeepEqual(task.Completion, want) {
				t.Errorf("install %s completion = %v, want %v", task.ID, task.Completion, want)
			}
			if mode == "uninstall" && len(task.Completion) != 0 {
				t.Errorf("uninstall %s completion = %v, want none", task.ID, task.Completion)
			}
		}
	}
}

// The failure marking applies to every way an install or record Task ends without success,
// including a Task skipped after an earlier one failed, and never to a running or succeeded one.
func TestSoftwareFailureCompletionMarksEveryUnfinishedOutcome(t *testing.T) {
	for status, want := range map[workflowdomain.TaskStatus]softwaredomain.AssignmentState{
		workflowdomain.TaskFailed:            softwaredomain.StateFailed,
		workflowdomain.TaskRequiresAttention: softwaredomain.StateFailed,
		workflowdomain.TaskCanceled:          softwaredomain.StateFailed,
		workflowdomain.TaskSkipped:           softwaredomain.StateFailed,
		workflowdomain.TaskRunning:           softwaredomain.StatePending,
		workflowdomain.TaskSucceeded:         softwaredomain.StatePending,
	} {
		assignments := newFakeAssignmentRepo()
		_ = assignments.Upsert(context.Background(), &softwaredomain.Assignment{ServerID: "s1", Kind: softwaredomain.KindPodman, State: softwaredomain.StatePending})
		handler := workflowProjections(nil, assignments, nil, nil, nil)[completionSoftwareFailure]
		completion := softwareFailure(softwaredomain.KindPodman, []string{"s1"})
		err := handler.Project(context.Background(), workflowapp.TaskTransition{
			Workflow: &workflowdomain.Workflow{ID: "wf-1"}, Task: workflowdomain.Task{ID: "record-software", Status: status}, Completion: completion,
		})
		if err != nil {
			t.Fatalf("Project(%s) error = %v", status, err)
		}
		if got, _ := assignments.FindByServerAndKind(context.Background(), "s1", softwaredomain.KindPodman); got.State != want {
			t.Errorf("assignment after a %s Task = %s, want %s", status, got.State, want)
		}
	}
}
