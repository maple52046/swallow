package app

import (
	"context"
	"reflect"
	"testing"

	discoveryapp "github.com/maple52046/swallow/internal/core/discovery/application"
	softwaredomain "github.com/maple52046/swallow/internal/core/software/domain"
	nodeexporter "github.com/maple52046/swallow/modules/software/node-exporter"
)

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

	got, err := installedExporters{catalog: testModules(t).software, assignments: assignments}.InstalledExporters(context.Background())
	if err != nil {
		t.Fatalf("InstalledExporters() error = %v", err)
	}
	want := map[discoveryapp.InstalledExporter]bool{
		{ServerID: "s1", Name: "node-exporter", Port: nodeexporter.Port}:                                  true,
		{ServerID: "s4", Name: "node-exporter", Port: nodeexporter.Port}:                                  true,
		{ServerID: "s1", Name: "device-metrics-exporter", Port: softwaredomain.DeviceMetricsExporterPort}: true,
	}
	gotSet := map[discoveryapp.InstalledExporter]bool{}
	for _, exporter := range got {
		gotSet[exporter] = true
	}
	if !reflect.DeepEqual(gotSet, want) {
		t.Errorf("InstalledExporters() = %v, want %v", got, want)
	}
}
