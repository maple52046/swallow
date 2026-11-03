package kubernetes

import (
	"errors"
	"reflect"
	"testing"

	platformdomain "github.com/maple52046/swallow/internal/core/platform/domain"
)

func deployBody() map[string]any {
	return map[string]any{
		"siteId": "site-1", "name": "lab", "type": "kubernetes",
		"k0sVersion": "v1.36.3+k0s.2", "podCidr": "10.244.0.0/16", "apiVip": "10.0.0.200", "apiVipPrefix": float64(24),
		"roleAssignments": []any{
			map[string]any{"serverId": "s1", "role": "control-plane", "runWorkloads": true},
			map[string]any{"serverId": "s2", "role": "worker"},
		},
	}
}

// A deploy request becomes a spec whose members carry the roles, and renders back to the same
// Kubernetes fields; the Platform's identity fields are not settings, the monitoring dashboards
// setting is.
func TestSpecRoundTripsThroughTheDeployRequest(t *testing.T) {
	withoutDashboards := deployBody()
	withoutDashboards[platformdomain.SettingMonitoringDashboards] = false
	spec, err := Platform{}.SpecFromDeployRequest(withoutDashboards)
	if err != nil {
		t.Fatal(err)
	}
	if spec.Settings[platformdomain.SettingMonitoringDashboards] != false ||
		(Platform{}).DeployRequest(spec)[platformdomain.SettingMonitoringDashboards] != false {
		t.Errorf("monitoringDashboards did not round-trip: spec settings %v", spec.Settings)
	}
	spec, err = Platform{}.SpecFromDeployRequest(deployBody())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(spec.Members, []platformdomain.PlatformMember{
		{ServerID: "s1", Roles: []string{RoleControlPlane, RoleWorkloads}}, {ServerID: "s2", Roles: []string{RoleWorker}},
	}) || spec.Settings["name"] != nil || spec.Settings["k0sVersion"] != "v1.36.3+k0s.2" {
		t.Fatalf("spec = %+v", spec)
	}
	body := Platform{}.DeployRequest(spec)
	assignments := body["roleAssignments"].([]any)
	if body["apiVip"] != "10.0.0.200" || assignments[0].(map[string]any)["runWorkloads"] != true || assignments[1].(map[string]any)["role"] != RoleWorker {
		t.Errorf("rendered body = %+v", body)
	}
	if _, err := (Platform{}).SpecFromDeployRequest(map[string]any{"roleAssignments": []any{map[string]any{"serverId": "s1", "role": "boss"}}}); !errors.Is(err, platformdomain.ErrInvalidSpec) {
		t.Errorf("unknown role: err = %v, want ErrInvalidSpec", err)
	}
}

// The classification of desired-state-model §5.2: adding a member and enabling the GPU Operator
// converge directly; removing a member, changing a role, and upgrading are destructive and not yet
// automated; network identity cannot change.
func TestChangesFollowTheClassification(t *testing.T) {
	applied, _ := Platform{}.SpecFromDeployRequest(deployBody())
	desired := applied.Clone()
	desired.Members = append(desired.Members, platformdomain.PlatformMember{ServerID: "s3", Roles: []string{RoleWorker}})
	desired.Settings["gpuOperator"] = map[string]any{"enabled": true}
	changes, err := Platform{}.Changes(&applied, desired)
	if err != nil || changes.Destructive() || len(changes.Unsupported) != 0 || len(changes.Actions) != 2 {
		t.Fatalf("add a member and enable the operator = %+v, %v; want two direct, supported actions", changes, err)
	}

	desired = applied.Clone()
	desired.Members = desired.Members[:1]
	desired.Members[0].Roles = []string{RoleControlPlane}
	desired.Settings["k0sVersion"] = "v1.37.0+k0s.0"
	changes, err = Platform{}.Changes(&applied, desired)
	if err != nil || !changes.Destructive() || !reflect.DeepEqual(changes.Unsupported, []string{"remove-member", "change-node-role", "upgrade"}) {
		t.Fatalf("remove, re-role, upgrade = %+v, %v", changes, err)
	}

	desired = applied.Clone()
	desired.Settings["podCidr"] = "10.250.0.0/16"
	if _, err := (Platform{}).Changes(&applied, desired); !errors.Is(err, platformdomain.ErrImmutableSpecField) {
		t.Errorf("a new podCidr: err = %v, want ErrImmutableSpecField", err)
	}

	// The monitoring dashboards are on when the setting is omitted: turning them on converges by
	// redeploying, turning them off is refused, and an explicit true over an omitted one is no change.
	withoutDashboards := applied.Clone()
	withoutDashboards.Settings[platformdomain.SettingMonitoringDashboards] = false
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
	explicit.Settings[platformdomain.SettingMonitoringDashboards] = true
	if changes, _ = (Platform{}).Changes(&applied, explicit); !changes.Empty() {
		t.Errorf("an explicit true over an omitted setting = %+v, want no change", changes)
	}

	created, err := Platform{}.Changes(nil, applied)
	if err != nil || !created.Destructive() || len(created.Actions) != 3 {
		t.Errorf("creation = %+v, %v; want install plus a join per member, destructive", created, err)
	}
	absent := applied.Clone()
	absent.Desired = platformdomain.PlatformAbsent
	if uninstall, _ := (Platform{}).Changes(&applied, absent); !uninstall.Destructive() || uninstall.Actions[0].Action != "uninstall" {
		t.Errorf("uninstall = %+v", uninstall)
	}
}
