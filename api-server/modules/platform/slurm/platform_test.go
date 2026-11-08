package slurm

import (
	"errors"
	"reflect"
	"testing"

	platformdomain "github.com/maple52046/swallow/internal/core/platform/domain"
)

func deployBody() map[string]any {
	return map[string]any{
		"siteId": "site-1", "name": "lab", "type": "slurm",
		"slurm": map[string]any{
			"clusterName": "lab",
			"nodeAssignments": []any{
				map[string]any{"serverId": "m1", "controller": true, "login": true},
				map[string]any{"serverId": "m2", "compute": true},
			},
		},
	}
}

// The nodeAssignments become members with their Slurm roles, and the rest of the slurm object stays
// a setting; rendering puts the assignments back.
func TestSpecRoundTripsThroughTheDeployRequest(t *testing.T) {
	spec, err := Platform{}.SpecFromDeployRequest(deployBody())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(spec.Members, []platformdomain.PlatformMember{
		{ServerID: "m1", Roles: []string{RoleController, RoleLogin}}, {ServerID: "m2", Roles: []string{RoleCompute}},
	}) || spec.Settings["slurm"].(map[string]any)["nodeAssignments"] != nil {
		t.Fatalf("spec = %+v", spec)
	}
	assignments := Platform{}.DeployRequest(spec)["slurm"].(map[string]any)["nodeAssignments"].([]any)
	if first := assignments[0].(map[string]any); first["controller"] != true || first["login"] != true || first["compute"] != false {
		t.Errorf("rendered assignment = %+v", first)
	}
}

// Adding a member and enabling a monitoring feature converge directly; disabling one is refused
// (not automated); a new storage or role is destructive; the cluster name cannot change.
func TestChangesFollowTheClassification(t *testing.T) {
	applied, _ := Platform{}.SpecFromDeployRequest(deployBody())
	desired := applied.Clone()
	desired.Members = append(desired.Members, platformdomain.PlatformMember{ServerID: "m3", Roles: []string{RoleCompute}})
	desired.Settings["deviceMetricsExporter"] = map[string]any{"enabled": true}
	changes, err := Platform{}.Changes(&applied, desired)
	if err != nil || changes.Destructive() || len(changes.Unsupported) != 0 || len(changes.Actions) != 2 {
		t.Fatalf("add a member and enable the exporter = %+v, %v", changes, err)
	}

	withExporter := desired.Clone()
	withoutExporter := withExporter.Clone()
	withoutExporter.Settings["deviceMetricsExporter"] = map[string]any{"enabled": false}
	changes, _ = Platform{}.Changes(&withExporter, withoutExporter)
	if changes.Destructive() || !reflect.DeepEqual(changes.Unsupported, []string{"disable-deviceMetricsExporter"}) {
		t.Errorf("disable the exporter = %+v; want a non-destructive change swallow cannot carry out", changes)
	}

	desired = applied.Clone()
	desired.Members[1].Roles = []string{RoleCompute, RoleLogin}
	changes, _ = Platform{}.Changes(&applied, desired)
	if !changes.Destructive() || !reflect.DeepEqual(changes.Unsupported, []string{"change-node-role"}) {
		t.Errorf("new roles = %+v", changes)
	}

	// The monitoring dashboards are on when the setting is omitted: turning them on converges by
	// redeploying, turning them off is refused.
	withoutDashboards := applied.Clone()
	withoutDashboards.Settings["monitoringDashboards"] = false
	changes, _ = Platform{}.Changes(&withoutDashboards, applied)
	if changes.Destructive() || len(changes.Unsupported) != 0 || changes.Realization != platformdomain.RealizeDeploy ||
		len(changes.Actions) != 1 || changes.Actions[0].Action != "enable-monitoring-dashboards" {
		t.Errorf("turn the dashboards on = %+v", changes)
	}
	changes, _ = Platform{}.Changes(&applied, withoutDashboards)
	if changes.Destructive() || !reflect.DeepEqual(changes.Unsupported, []string{"disable-monitoring-dashboards"}) {
		t.Errorf("turn the dashboards off = %+v; want a non-destructive change swallow cannot carry out", changes)
	}
	explicit := applied.Clone()
	explicit.Settings["monitoringDashboards"] = true
	if changes, _ = (Platform{}).Changes(&applied, explicit); !changes.Empty() {
		t.Errorf("an explicit true over an omitted setting = %+v, want no change", changes)
	}

	// Omnistat's hardware counters switch on a running Platform: alone they reconfigure only
	// Omnistat, an absent setting is off, and any other Omnistat change is still refused.
	withOmnistat := applied.Clone()
	withOmnistat.Settings["omnistat"] = map[string]any{"enabled": true, "hardwareCounters": false}
	withCounters := withOmnistat.Clone()
	withCounters.Settings["omnistat"] = map[string]any{"enabled": true, "hardwareCounters": true}
	withoutKey := withOmnistat.Clone()
	withoutKey.Settings["omnistat"] = map[string]any{"enabled": true}
	for _, change := range []struct {
		from, to platformdomain.PlatformSpec
		action   string
	}{
		{withOmnistat, withCounters, "enable-hardware-counters"},
		{withoutKey, withCounters, "enable-hardware-counters"},
		{withCounters, withOmnistat, "disable-hardware-counters"},
		{withCounters, withoutKey, "disable-hardware-counters"},
	} {
		changes, _ = Platform{}.Changes(&change.from, change.to)
		if changes.Destructive() || len(changes.Unsupported) != 0 || changes.Realization != platformdomain.RealizeHardwareCounters ||
			len(changes.Actions) != 1 || changes.Actions[0].Action != change.action {
			t.Errorf("hardwareCounters %v -> %v = %+v; want only %s, realized by the hardware counters change",
				change.from.Settings["omnistat"], change.to.Settings["omnistat"], changes, change.action)
		}
	}
	if changes, _ = (Platform{}).Changes(&withOmnistat, withoutKey); !changes.Empty() {
		t.Errorf("an absent hardwareCounters over false = %+v, want no change", changes)
	}
	withMember := withCounters.Clone()
	withMember.Members = append(withMember.Members, platformdomain.PlatformMember{ServerID: "s9", Roles: []string{RoleCompute}})
	if changes, _ = (Platform{}).Changes(&withOmnistat, withMember); len(changes.Unsupported) != 0 || changes.Realization != platformdomain.RealizeDeploy {
		t.Errorf("counters with a new member = %+v; want the convergent deploy, which reruns Omnistat with them", changes)
	}
	nativeCounters := withCounters.Clone()
	nativeCounters.Settings["omnistat"] = map[string]any{"enabled": true, "hardwareCounters": true, "installMethod": "native"}
	changes, _ = Platform{}.Changes(&withOmnistat, nativeCounters)
	if changes.Destructive() || !reflect.DeepEqual(changes.Unsupported, []string{"change-omnistat"}) {
		t.Errorf("counters with a new install method = %+v; want the unsupported change-omnistat to refuse the write", changes)
	}

	desired = applied.Clone()
	slurm := map[string]any{"clusterName": "other"}
	desired.Settings["slurm"] = slurm
	if _, err := (Platform{}).Changes(&applied, desired); !errors.Is(err, platformdomain.ErrImmutableSpecField) {
		t.Errorf("a new cluster name: err = %v, want ErrImmutableSpecField", err)
	}
}
