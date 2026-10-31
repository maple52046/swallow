package app

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/maple52046/swallow/internal/adapters/hostautomation/ansible"
	softwaredomain "github.com/maple52046/swallow/internal/core/software/domain"
)

// Every catalog kind must install and uninstall through a playbook the shipped manifest registers,
// under a valid Workflow kind that requires a deployed host: a kind added to the catalog without
// both mappings would be accepted by the API and then refused at launch.
func TestEverySoftwareKindHasRegisteredPlaybooks(t *testing.T) {
	modules := filepath.Join("..", "..", "modules")
	catalog, err := ansible.LoadManifestCatalog(filepath.Join(modules, "manifest.json"), modules)
	if err != nil {
		t.Fatalf("load shipped manifest: %v", err)
	}
	for _, entry := range softwaredomain.Catalog() {
		for mode, table := range map[string]map[softwaredomain.Kind]softwarePlaybook{
			"install": softwareInstallPlaybooks, "uninstall": softwareUninstallPlaybooks,
		} {
			mapping, ok := table[entry.Kind]
			if !ok {
				t.Errorf("%s has no %s playbook", entry.Kind, mode)
				continue
			}
			if _, err := catalog.Resolve(mapping.playbook); err != nil {
				t.Errorf("%s %s playbook %q: %v", entry.Kind, mode, mapping.playbook, err)
			}
			if !mapping.workflowKind.Valid() || mapping.workflowKind.RequiredProvisioningState() != "deployed" {
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
