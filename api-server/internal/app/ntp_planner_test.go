package app

import (
	"context"
	"errors"
	"reflect"
	"testing"

	operationapp "github.com/maple52046/swallow/internal/operation/application"
	operationdomain "github.com/maple52046/swallow/internal/operation/domain"
	platformdomain "github.com/maple52046/swallow/internal/platform/domain"
	provisioningdomain "github.com/maple52046/swallow/internal/provisioning/domain"
	serverdomain "github.com/maple52046/swallow/internal/server/domain"
)

// ntpTestServers is a FindByID-only ServerRepository for the time Task tests. Servers named in
// deployed are deployed; every other id is ready.
type ntpTestServers struct {
	serverdomain.ServerRepository
	deployed map[string]bool
}

func (r ntpTestServers) FindByID(_ context.Context, id string) (*serverdomain.Server, error) {
	state := "ready"
	if r.deployed[id] {
		state = "deployed"
	}
	return &serverdomain.Server{
		ID:           id,
		Source:       serverdomain.Source{SiteID: "site-1", IntegrationID: "maas-1"},
		Observed:     serverdomain.Observed{Hostname: id},
		Provisioning: &serverdomain.ProvisioningStatus{State: state},
	}, nil
}

// ntpTestAutomation answers FindBySiteID with a runnable configuration unless disabled.
type ntpTestAutomation struct {
	operationdomain.AutomationConfigurationRepository
	disabled bool
}

func (r ntpTestAutomation) FindBySiteID(_ context.Context, siteID string) (*operationdomain.AutomationConfiguration, error) {
	return &operationdomain.AutomationConfiguration{
		SiteID: siteID, Enabled: !r.disabled, CredentialSource: operationdomain.CredentialSourceDeploymentKey,
	}, nil
}

// ntpTestCatalog registers only the configure-ntp playbook.
type ntpTestCatalog struct{}

func (ntpTestCatalog) Resolve(name string) (string, error) {
	if name == ntpPlaybook {
		return "/automation/playbooks/" + name + ".yml", nil
	}
	return "", operationdomain.ErrPlaybookNotAllowed
}

func newNTPTestPlanner(server string, deployed map[string]bool, automationDisabled bool) ntpStepPlanner {
	operations := operationapp.NewExecutionService(
		nil, ntpTestServers{deployed: deployed}, ntpTestAutomation{disabled: automationDisabled}, ntpTestCatalog{}, nil, nil, nil,
	)
	return ntpStepPlanner{server: server, operations: operations}
}

func serverRefs(ids ...string) []operationdomain.ResourceReference {
	return serverResourceRefs(ids)
}

func extraVars(t *testing.T, task operationdomain.Task) map[string]any {
	t.Helper()
	vars, ok := task.Parameters["extraVars"].(map[string]any)
	if !ok {
		t.Fatalf("task %s has no extraVars: %v", task.ID, task.Parameters)
	}
	return vars
}

// Every provisioned Server gets its own configure-ntp Task after its provision-os only, with the
// installation's NTP server frozen as the trusted variable and the caller's Job.
func TestNTPPlannerConfigureTasks(t *testing.T) {
	planner := newNTPTestPlanner("10.170.168.20", nil, false)
	tasks, err := planner.configureTasks(context.Background(), "op-1", "admin", "", []string{"s1", "s2"})
	if err != nil {
		t.Fatalf("configureTasks() error = %v", err)
	}
	if got := taskIDs(tasks); !reflect.DeepEqual(got, []string{"configure-ntp-s1", "configure-ntp-s2"}) {
		t.Fatalf("configureTasks() = %v, want one Task per Server", got)
	}
	for index, serverID := range []string{"s1", "s2"} {
		task := tasks[index]
		if task.Kind != "ansible-playbook" || task.Job != "" || task.Executor != operationdomain.RunnerKindAnsible ||
			task.Parameters["playbook"] != ntpPlaybook {
			t.Errorf("task %s = %+v, want the configure-ntp ansible Task with no Job", task.ID, task)
		}
		if !reflect.DeepEqual(task.DependsOn, []string{"provision-" + serverID}) ||
			!reflect.DeepEqual(task.Targets, serverRefs(serverID)) {
			t.Errorf("task %s DependsOn %v Targets %v, want only %s", task.ID, task.DependsOn, task.Targets, serverID)
		}
		vars := extraVars(t, task)
		if !reflect.DeepEqual(vars[ntpServersVar], []string{"10.170.168.20"}) ||
			vars["swallow_operation_id"] != "op-1" || vars["swallow_operation_kind"] != "configure-ntp" {
			t.Errorf("task %s extraVars = %v, want the NTP server under op-1/configure-ntp", task.ID, vars)
		}
	}
}

// Time sync is not optional: an installation without an NTP server and a Site whose automation
// cannot run both refuse the deployment, while an unwired planner leaves the Workflow unchanged.
func TestNTPPlannerRefusesWithoutServerOrAutomation(t *testing.T) {
	ctx := context.Background()
	if tasks, err := (ntpStepPlanner{}).configureTasks(ctx, "op-1", "admin", "", []string{"s1"}); err != nil || tasks != nil {
		t.Errorf("unwired configureTasks() = %v, %v; want nil, nil", tasks, err)
	}
	if _, err := newNTPTestPlanner("", nil, false).configureTasks(ctx, "op-1", "admin", "", []string{"s1"}); !errors.Is(err, provisioningdomain.ErrNTPServerMissing) {
		t.Errorf("configureTasks() without an NTP server = %v, want ErrNTPServerMissing", err)
	}
	if _, err := newNTPTestPlanner("10.0.0.5", nil, true).configureTasks(ctx, "op-1", "admin", "", []string{"s1"}); !errors.Is(err, operationdomain.ErrAutomationDisabled) {
		t.Errorf("configureTasks() with automation disabled = %v, want ErrAutomationDisabled", err)
	}
}

// existing_os: nothing is provisioned, so the ensure-os Job has no time Task at all and the install
// step waits only for SSH readiness; the clock check runs inside the platform's install playbook.
func TestBuildEnsureOSExistingOSAddsNoTimeTask(t *testing.T) {
	launch := platformdomain.DeploymentLaunch{
		TargetServerIDs: []string{"s1", "s2"}, RequestedBy: "admin", TimeSyncCheck: platformdomain.TimeSyncCheckWarn,
	}
	prepared := &operationapp.PreparedAnsibleStep{SiteID: "site-1", Targets: serverRefs("s1", "s2")}
	launcher := platformDeploymentLauncher{ntp: newNTPTestPlanner("10.170.168.20", map[string]bool{"s1": true, "s2": true}, false)}

	plan, err := launcher.buildEnsureOS(context.Background(), launch, "op-1", false, prepared)
	if err != nil {
		t.Fatalf("buildEnsureOS() error = %v", err)
	}
	if got := taskIDs(plan.steps); !reflect.DeepEqual(got, []string{"wait-for-ssh"}) {
		t.Fatalf("ensure-os Tasks = %v, want only [wait-for-ssh]", got)
	}
	if !reflect.DeepEqual(plan.installDeps, []string{"wait-for-ssh"}) {
		t.Errorf("installDeps = %v, want [wait-for-ssh]", plan.installDeps)
	}
}

// The install step's trusted variables gain the clock-check mode, bound, and NTP server; an unset
// mode is enforce, a missing NTP server is an empty list, and the caller's map is not modified.
func TestNTPPlannerWithCheckVars(t *testing.T) {
	trusted := map[string]any{"swallow_k0s_version": "v1.36.3+k0s.2"}
	vars := newNTPTestPlanner("10.170.168.20", nil, false).withCheckVars(trusted, platformdomain.TimeSyncCheckWarn)
	if vars["swallow_k0s_version"] != "v1.36.3+k0s.2" || vars[timeSyncCheckModeVar] != "warn" ||
		vars[timeSyncMaxOffsetVar] != timeSyncMaxOffsetSeconds || !reflect.DeepEqual(vars[ntpServersVar], []string{"10.170.168.20"}) {
		t.Errorf("withCheckVars() = %v, want the platform vars plus warn, the bound, and the NTP server", vars)
	}
	if len(trusted) != 1 {
		t.Errorf("withCheckVars() modified its input: %v", trusted)
	}
	vars = (ntpStepPlanner{}).withCheckVars(nil, "")
	if vars[timeSyncCheckModeVar] != "enforce" || !reflect.DeepEqual(vars[ntpServersVar], []string{}) {
		t.Errorf("withCheckVars() without a server or mode = %v, want enforce and no NTP server", vars)
	}
}

// configure-ntp Tasks of a deploy-kubernetes or configure-slurm Workflow carry the Workflow's kind
// like the install Task, but must not run the platform completion, which only install-platform can
// satisfy.
func TestPlatformDeploymentObserverIgnoresTheTimeTasks(t *testing.T) {
	observer := platformDeploymentObserver{}
	for _, kind := range []operationdomain.WorkflowKind{
		operationdomain.WorkflowKindDeployKubernetes, operationdomain.WorkflowKindConfigureSlurm,
	} {
		execution := &operationdomain.AnsibleExecution{OperationID: "op-1", Kind: kind, PlatformID: "plat-1", StepID: ntpStepPrefix + "s1"}
		if err := observer.AnsibleStepSucceeded(context.Background(), execution, operationdomain.RunnerResult{}); err != nil {
			t.Errorf("%s configure-ntp completion err = %v, want nil", kind, err)
		}
	}
}

func taskIDs(tasks []operationdomain.Task) []string {
	ids := make([]string, 0, len(tasks))
	for _, task := range tasks {
		ids = append(ids, task.ID)
	}
	return ids
}
