package app

import (
	"context"
	"reflect"
	"testing"

	discoveryapp "github.com/maple52046/swallow/internal/core/discovery/application"
	platformdomain "github.com/maple52046/swallow/internal/core/platform/domain"
	softwaredomain "github.com/maple52046/swallow/internal/core/software/domain"
	"github.com/maple52046/swallow/modules/platform/slurm"
	nodeexporter "github.com/maple52046/swallow/modules/software/node-exporter"
)

// platformList is an exporterPlatforms over fixed Platforms.
type platformList []*platformdomain.Platform

func (p platformList) List(context.Context, string) ([]*platformdomain.Platform, error) {
	return p, nil
}

// exporterSet collects exporters for an order-free comparison.
func exporterSet(exporters []discoveryapp.InstalledExporter) map[discoveryapp.InstalledExporter]bool {
	set := map[discoveryapp.InstalledExporter]bool{}
	for _, exporter := range exporters {
		set[exporter] = true
	}
	return set
}

// Only a kind that declares an exporter is reported, and only for a Server whose assignment is
// installed or still uninstalling: a pending or failed install may have nothing listening.
func TestInstalledExportersReportsDeclaredExportersOfInstalledAssignments(t *testing.T) {
	assignments := newFakeAssignmentRepo()
	for _, a := range []*softwaredomain.Assignment{
		{ServerID: "s1", Kind: nodeexporter.Kind, State: softwaredomain.StateInstalled},
		{ServerID: "s2", Kind: nodeexporter.Kind, State: softwaredomain.StatePending},
		{ServerID: "s3", Kind: nodeexporter.Kind, State: softwaredomain.StateFailed},
		{ServerID: "s4", Kind: nodeexporter.Kind, State: softwaredomain.StateUninstalling},
		{ServerID: "s1", Kind: softwaredomain.KindDeviceMetricsExporter, State: softwaredomain.StateInstalled},
		{ServerID: "s1", Kind: softwaredomain.KindDockerCE, State: softwaredomain.StateInstalled},
	} {
		if err := assignments.Upsert(context.Background(), a); err != nil {
			t.Fatal(err)
		}
	}

	got, err := installedExporters{catalog: testModules(t).software, assignments: assignments, platforms: platformList{}}.
		InstalledExporters(context.Background())
	if err != nil {
		t.Fatalf("InstalledExporters() error = %v", err)
	}
	want := map[discoveryapp.InstalledExporter]bool{
		{ServerID: "s1", Name: "node-exporter", Port: nodeexporter.Port}:                                  true,
		{ServerID: "s4", Name: "node-exporter", Port: nodeexporter.Port}:                                  true,
		{ServerID: "s1", Name: "device-metrics-exporter", Port: softwaredomain.DeviceMetricsExporterPort}: true,
	}
	if gotSet := exporterSet(got); !reflect.DeepEqual(gotSet, want) {
		t.Errorf("InstalledExporters() = %v, want %v", got, want)
	}
}

// A Slurm Platform whose Omnistat is ready runs the exporter on the compute members of the spec it
// realized; controllers and login hosts run none, and an Omnistat that is installing, failed, or
// absent, or a Kubernetes Platform, reports nothing.
func TestInstalledExportersReportsOmnistatOnReadyPlatforms(t *testing.T) {
	members := func(ids ...string) *platformdomain.PlatformSpec {
		spec := &platformdomain.PlatformSpec{Members: []platformdomain.PlatformMember{{ServerID: "c1", Roles: []string{slurm.RoleController, slurm.RoleLogin}}}}
		for _, id := range ids {
			spec.Members = append(spec.Members, platformdomain.PlatformMember{ServerID: id, Roles: []string{slurm.RoleCompute}})
		}
		return spec
	}
	withOmnistat := func(id string, state platformdomain.OmnistatState, applied, spec *platformdomain.PlatformSpec) *platformdomain.Platform {
		return &platformdomain.Platform{
			ID: id, Type: platformdomain.PlatformTypeSlurm, Omnistat: &platformdomain.Omnistat{Enabled: true, State: state},
			Spec: spec, Status: platformdomain.PlatformStatus{Applied: applied},
		}
	}
	platforms := platformList{
		// The applied spec is what the exporter was installed on; a member the spec adds is not yet.
		withOmnistat("ready", platformdomain.OmnistatReady, members("n1", "n2"), members("n1", "n2", "n3")),
		// Before any Workflow succeeded under a spec, the spec itself names the members.
		withOmnistat("adopted", platformdomain.OmnistatReady, nil, members("n4")),
		withOmnistat("installing", platformdomain.OmnistatInstalling, members("n5"), members("n5")),
		withOmnistat("failed", platformdomain.OmnistatFailed, members("n6"), members("n6")),
		withOmnistat("absent", platformdomain.OmnistatAbsent, members("n7"), members("n7")),
		{ID: "no-omnistat", Type: platformdomain.PlatformTypeSlurm, Spec: members("n8")},
		{ID: "k8s", Type: platformdomain.PlatformTypeKubernetes, Omnistat: &platformdomain.Omnistat{State: platformdomain.OmnistatReady},
			Spec: members("n9")},
	}

	got, err := installedExporters{catalog: testModules(t).software, assignments: newFakeAssignmentRepo(), platforms: platforms}.
		InstalledExporters(context.Background())
	if err != nil {
		t.Fatalf("InstalledExporters() error = %v", err)
	}
	want := map[discoveryapp.InstalledExporter]bool{}
	for _, id := range []string{"n1", "n2", "n4"} {
		want[discoveryapp.InstalledExporter{ServerID: id, Name: "omnistat", Port: platformdomain.OmnistatExporterPort}] = true
	}
	if gotSet := exporterSet(got); !reflect.DeepEqual(gotSet, want) {
		t.Errorf("InstalledExporters() = %v, want %v", got, want)
	}
}
