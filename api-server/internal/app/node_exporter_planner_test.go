package app

import (
	"context"
	"reflect"
	"testing"

	platformdomain "github.com/maple52046/swallow/internal/core/platform/domain"
	softwareapp "github.com/maple52046/swallow/internal/core/software/application"
	softwaredomain "github.com/maple52046/swallow/internal/core/software/domain"
	"github.com/maple52046/swallow/internal/core/store/storetest"
	workflowapp "github.com/maple52046/swallow/internal/core/workflow/application"
	workflowdomain "github.com/maple52046/swallow/internal/core/workflow/domain"
	nodeexporter "github.com/maple52046/swallow/modules/software/node-exporter"
)

// newNodeExporterTestPlanner wires the planner over deployed or ready test Servers, the build's
// Software modules, and an in-memory assignment store.
func newNodeExporterTestPlanner(t *testing.T, deployed map[string]bool) (nodeExporterPlanner, *fakeAssignmentRepo) {
	t.Helper()
	modules := testModules(t)
	servers := ntpTestServers{deployed: deployed}
	assignments := newFakeAssignmentRepo()
	return nodeExporterPlanner{
		software: softwareapp.NewSoftwareService(&storetest.Transactor{}, modules.software, modules.accelerators,
			assignments, servers, nil, nil),
		operations: newTestExecutionService(servers, ntpTestAutomation{}, slurmTestCatalog{}, modules.workflowKinds),
	}, assignments
}

// Each Server gets an install Task after the Task named for it and a record Task after that, both in
// the caller's Job: the install runs the Node Exporter's software playbook with its port and marks
// the Server's assignment failed when it fails; recording the plan writes a pending assignment.
func TestNodeExporterPlannerComposesAfterEachServersTimeTask(t *testing.T) {
	planner, assignments := newNodeExporterTestPlanner(t, nil)
	after := map[string]string{"s1": "configure-ntp-s1", "s2": "provision-s2"}

	planned, err := planner.installTasks(context.Background(), "op-1", "admin", "", []string{"s1", "s2"}, after)
	if err != nil {
		t.Fatalf("installTasks() error = %v", err)
	}
	if got := taskIDs(planned.tasks); !reflect.DeepEqual(got, []string{
		"install-node-exporter-s1", "record-node-exporter-s1", "install-node-exporter-s2", "record-node-exporter-s2",
	}) {
		t.Fatalf("installTasks() = %v, want an install and a record Task per Server", got)
	}
	for index, serverID := range []string{"s1", "s2"} {
		install, record := planned.tasks[2*index], planned.tasks[2*index+1]
		if install.Job != "" || install.Executor != workflowdomain.RunnerKindHostAutomation ||
			install.Parameters["playbook"] != "deploy-node-exporter" ||
			!reflect.DeepEqual(install.DependsOn, []string{after[serverID]}) || !reflect.DeepEqual(install.Targets, serverRefs(serverID)) {
			t.Errorf("install Task %s = %+v", install.ID, install)
		}
		if vars := extraVars(t, install); vars["swallow_node_exporter_port"] != nodeexporter.Port ||
			vars["swallow_operation_kind"] != "configure-node-exporter" {
			t.Errorf("install Task %s extraVars = %v, want the port under configure-node-exporter", install.ID, vars)
		}
		if want := []workflowdomain.Completion{softwareFailure(nodeexporter.Kind, []string{serverID})}; !reflect.DeepEqual(install.Completion, want) {
			t.Errorf("install Task %s completion = %v, want %v", install.ID, install.Completion, want)
		}
		if record.Kind != softwareStepKindRecord || !reflect.DeepEqual(record.DependsOn, []string{install.ID}) ||
			record.Parameters["softwareKind"] != string(nodeexporter.Kind) || !reflect.DeepEqual(record.Parameters["serverIds"], []string{serverID}) {
			t.Errorf("record Task %s = %+v", record.ID, record)
		}
	}

	planner.record(context.Background(), planned, "op-1")
	for _, serverID := range []string{"s1", "s2"} {
		got, err := assignments.FindByServerAndKind(context.Background(), serverID, nodeexporter.Kind)
		if err != nil || got.State != softwaredomain.StatePending || got.LastWorkflowID != "op-1" {
			t.Errorf("assignment of %s = %+v, %v; want pending under op-1", serverID, got, err)
		}
	}
}

// An unwired planner and an empty Server list add nothing.
func TestNodeExporterPlannerUnwiredAddsNothing(t *testing.T) {
	planned, err := (nodeExporterPlanner{}).installTasks(context.Background(), "op-1", "admin", "", []string{"s1"}, nil)
	if err != nil || len(planned.tasks) != 0 || planned.install != nil {
		t.Errorf("unwired installTasks() = %+v, %v; want nothing", planned, err)
	}
	planner, _ := newNodeExporterTestPlanner(t, nil)
	planned, err = planner.installTasks(context.Background(), "op-1", "admin", "", nil, nil)
	if err != nil || len(planned.tasks) != 0 {
		t.Errorf("installTasks() of no Servers = %+v, %v; want nothing", planned, err)
	}
	(nodeExporterPlanner{}).record(context.Background(), planned, "op-1")
}

// A Server's time Task is what its Node Exporter waits for; without one, its provision-os Task.
func TestAfterTimeSync(t *testing.T) {
	ntp := []workflowdomain.Task{{ID: "configure-ntp-s1", Targets: serverRefs("s1")}}
	if got := afterTimeSync([]string{"s1", "s2"}, ntp); !reflect.DeepEqual(got, map[string]string{
		"s1": "configure-ntp-s1", "s2": "provision-s2",
	}) {
		t.Errorf("afterTimeSync() = %v", got)
	}
}

// existing_os provisions nothing, so the ensure-os Job composes no Node Exporter either.
func TestBuildEnsureOSExistingOSComposesNoNodeExporter(t *testing.T) {
	planner, _ := newNodeExporterTestPlanner(t, map[string]bool{"s1": true})
	launch := platformdomain.DeploymentLaunch{
		Platform:        &platformdomain.Platform{ID: "plat-1", Type: platformdomain.PlatformTypeKubernetes},
		TargetServerIDs: []string{"s1"}, RequestedBy: "admin",
	}
	launcher := platformDeploymentLauncher{
		ntp:          newNTPTestPlanner("10.140.168.20", map[string]bool{"s1": true}, false),
		nodeExporter: planner,
	}
	plan, err := launcher.buildEnsureOS(context.Background(), launch, "op-1", false,
		&workflowapp.PreparedAnsibleStep{SiteID: "site-1", Targets: serverRefs("s1")})
	if err != nil {
		t.Fatalf("buildEnsureOS() error = %v", err)
	}
	if got := taskIDs(plan.steps); !reflect.DeepEqual(got, []string{"wait-for-ssh"}) || plan.exporters.install != nil {
		t.Errorf("ensure-os Tasks = %v, exporters %+v; want only wait-for-ssh", got, plan.exporters)
	}
}

// An uninstall that keeps its Servers installs the Node Exporter again on each deployed one after
// the uninstall step, whatever the Platform type; one that releases them adds nothing.
func TestUninstallNodeExporterFollowsTheUninstallOfKeptServers(t *testing.T) {
	planner, _ := newNodeExporterTestPlanner(t, map[string]bool{"s1": true})
	launcher := platformDeploymentLauncher{servers: ntpTestServers{deployed: map[string]bool{"s1": true}}, nodeExporter: planner}
	for _, platformType := range platformdomain.ValidPlatformTypes {
		launch := platformdomain.UninstallLaunch{
			Platform:        &platformdomain.Platform{ID: "p1", Type: platformType},
			TargetServerIDs: []string{"s1", "s2"},
		}
		planned, err := launcher.uninstallNodeExporter(context.Background(), "op-1", launch)
		if err != nil {
			t.Fatalf("%s: uninstallNodeExporter() error = %v", platformType, err)
		}
		if got := taskIDs(planned.tasks); !reflect.DeepEqual(got, []string{"install-node-exporter-s1", "record-node-exporter-s1"}) {
			t.Fatalf("%s: uninstallNodeExporter() = %v, want the deployed Server only", platformType, got)
		}
		if install := planned.tasks[0]; install.Job != softwareConfigureJob(nodeexporter.Kind) ||
			!reflect.DeepEqual(install.DependsOn, []string{uninstallPlatformStepID}) {
			t.Errorf("%s: install Task = %+v, want its own Job after the uninstall step", platformType, install)
		}
	}

	released, err := launcher.uninstallNodeExporter(context.Background(), "op-1", platformdomain.UninstallLaunch{
		Platform: &platformdomain.Platform{ID: "p1"}, TargetServerIDs: []string{"s1"}, ReleaseServers: true,
	})
	if err != nil || len(released.tasks) != 0 {
		t.Errorf("releasing uninstallNodeExporter() = %+v, %v; want nothing", released, err)
	}
}
