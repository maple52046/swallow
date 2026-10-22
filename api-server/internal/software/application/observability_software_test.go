package application

import (
	"context"
	"errors"
	"slices"
	"testing"

	serverdomain "github.com/maple52046/swallow/internal/server/domain"
	softwaredomain "github.com/maple52046/swallow/internal/software/domain"
)

// installedDocker records an installed Docker CE on serverID, the container runtime the default
// install method of Prometheus and Grafana runs on.
func installedDocker(t *testing.T, repo *fakeAssignmentRepo, serverID string) {
	t.Helper()
	installedRuntime(t, repo, serverID, softwaredomain.KindDockerCE)
}

func installedRuntime(t *testing.T, repo *fakeAssignmentRepo, serverID string, kind softwaredomain.Kind) {
	t.Helper()
	if err := repo.Upsert(context.Background(), &softwaredomain.Assignment{
		ServerID: serverID, Kind: kind, State: softwaredomain.StateInstalled,
	}); err != nil {
		t.Fatalf("seed %s: %v", kind, err)
	}
}

// The default container method needs a container runtime, so a standalone install refuses a
// Server whose Docker CE or Podman is missing or not yet installed, before any Workflow exists.
func TestInstallObservabilityContainerMethodRequiresInstalledRuntime(t *testing.T) {
	for _, kind := range []softwaredomain.Kind{softwaredomain.KindPrometheus, softwaredomain.KindGrafana} {
		for _, runtimeState := range []softwaredomain.AssignmentState{"", softwaredomain.StatePending, softwaredomain.StateFailed} {
			t.Run(string(kind)+"/runtime "+string(runtimeState), func(t *testing.T) {
				service, repo, launcher := newTestService(map[string]*serverdomain.Server{"a": deployedServer("a")}, nil)
				if runtimeState != "" {
					_ = repo.Upsert(context.Background(), &softwaredomain.Assignment{
						ServerID: "a", Kind: softwaredomain.KindPodman, State: runtimeState,
					})
				}
				_, err := service.Install(context.Background(), InstallInput{Kind: kind, Targets: []InstallTarget{{ServerID: "a"}}})
				if !errors.Is(err, softwaredomain.ErrRequiredSoftwareMissing) {
					t.Fatalf("Install(%s) error = %v, want ErrRequiredSoftwareMissing", kind, err)
				}
				if launcher.install {
					t.Error("a refused install must not launch a Workflow")
				}
			})
		}
	}
}

// The container method follows the runtime installed on the targets; an explicit runtime must
// match it, and targets on different runtimes cannot share one install.
func TestInstallObservabilityResolvesContainerRuntime(t *testing.T) {
	servers := map[string]*serverdomain.Server{"a": deployedServer("a"), "b": deployedServer("b")}

	service, repo, launcher := newTestService(servers, nil)
	installedRuntime(t, repo, "a", softwaredomain.KindPodman)
	if _, err := service.Install(context.Background(), InstallInput{
		Kind: softwaredomain.KindGrafana, Targets: []InstallTarget{{ServerID: "a"}},
	}); err != nil {
		t.Fatalf("Install(on podman) error = %v", err)
	}
	vars := launcher.lastLaunch.TrustedVars
	if vars["swallow_grafana_install_method"] != "container" || vars["swallow_grafana_container_runtime"] != "podman" {
		t.Errorf("trusted vars = %v, want container on podman", vars)
	}
	assignment, _ := repo.FindByServerAndKind(context.Background(), "a", softwaredomain.KindGrafana)
	if assignment.Spec["containerRuntime"] != "podman" || assignment.Spec["installMethod"] != "container" {
		t.Errorf("recorded spec = %v, want container on podman", assignment.Spec)
	}

	service, repo, _ = newTestService(servers, nil)
	installedRuntime(t, repo, "a", softwaredomain.KindPodman)
	if _, err := service.Install(context.Background(), InstallInput{
		Kind: softwaredomain.KindGrafana, Targets: []InstallTarget{{ServerID: "a"}},
		Spec: map[string]any{"containerRuntime": "docker-ce"},
	}); !errors.Is(err, softwaredomain.ErrRequiredSoftwareMissing) {
		t.Errorf("Install(docker-ce requested on podman) error = %v, want ErrRequiredSoftwareMissing", err)
	}

	service, repo, _ = newTestService(servers, nil)
	installedRuntime(t, repo, "a", softwaredomain.KindPodman)
	installedRuntime(t, repo, "b", softwaredomain.KindDockerCE)
	if _, err := service.Install(context.Background(), InstallInput{
		Kind: softwaredomain.KindPrometheus, Targets: []InstallTarget{{ServerID: "a"}, {ServerID: "b"}},
	}); !errors.Is(err, softwaredomain.ErrSpecInvalid) {
		t.Errorf("Install(mixed runtimes) error = %v, want ErrSpecInvalid", err)
	}
}

// The native method installs from the official release or package repository and needs no
// container runtime; a runtime named alongside it is a spec error.
func TestInstallObservabilityNativeMethod(t *testing.T) {
	service, repo, launcher := newTestService(map[string]*serverdomain.Server{"a": deployedServer("a")}, nil)
	if _, err := service.Install(context.Background(), InstallInput{
		Kind: softwaredomain.KindPrometheus, Targets: []InstallTarget{{ServerID: "a"}},
		Spec: map[string]any{"installMethod": "native", "version": "v3.14.1"},
	}); err != nil {
		t.Fatalf("Install(native) error = %v", err)
	}
	vars := launcher.lastLaunch.TrustedVars
	if vars["swallow_prometheus_install_method"] != "native" || vars["swallow_prometheus_container_runtime"] != "" ||
		vars["swallow_prometheus_version"] != "3.14.1" {
		t.Errorf("trusted vars = %v, want native 3.14.1 without a runtime", vars)
	}
	assignment, _ := repo.FindByServerAndKind(context.Background(), "a", softwaredomain.KindPrometheus)
	if _, present := assignment.Spec["containerRuntime"]; present || assignment.Spec["version"] != "3.14.1" {
		t.Errorf("recorded spec = %v, want version 3.14.1 and no containerRuntime", assignment.Spec)
	}

	service, _, _ = newTestService(map[string]*serverdomain.Server{"a": deployedServer("a")}, nil)
	if _, err := service.Install(context.Background(), InstallInput{
		Kind: softwaredomain.KindGrafana, Targets: []InstallTarget{{ServerID: "a"}},
		Spec: map[string]any{"installMethod": "native", "containerRuntime": "podman"},
	}); !errors.Is(err, softwaredomain.ErrSpecInvalid) {
		t.Errorf("Install(native with runtime) error = %v, want ErrSpecInvalid", err)
	}
}

// The recorded spec and the trusted vars carry the pinned defaults and domain ports, so the
// assignment always says which image tag was applied and the playbook binds the published port.
func TestInstallPrometheusNormalizesSpecIntoRecordAndTrustedVars(t *testing.T) {
	service, repo, launcher := newTestService(map[string]*serverdomain.Server{"a": deployedServer("a")}, nil)
	installedDocker(t, repo, "a")
	if _, err := service.Install(context.Background(), InstallInput{
		Kind: softwaredomain.KindPrometheus, Targets: []InstallTarget{{ServerID: "a"}},
		Spec: map[string]any{"scrapeTargets": []any{" 10.0.0.2:8001 ", "node-b:9100", ""}},
	}); err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	assignment, err := repo.FindByServerAndKind(context.Background(), "a", softwaredomain.KindPrometheus)
	if err != nil {
		t.Fatalf("assignment: %v", err)
	}
	wantTargets := []string{"10.0.0.2:8001", "node-b:9100"}
	if assignment.Spec["version"] != softwaredomain.DefaultPrometheusVersion || assignment.Spec["scrapeJob"] != "swallow" {
		t.Errorf("recorded spec = %v, want default version and job", assignment.Spec)
	}
	if got, _ := assignment.Spec["scrapeTargets"].([]string); !slices.Equal(got, wantTargets) {
		t.Errorf("recorded scrapeTargets = %v, want %v", assignment.Spec["scrapeTargets"], wantTargets)
	}
	vars := launcher.lastLaunch.TrustedVars
	if vars["swallow_prometheus_version"] != softwaredomain.DefaultPrometheusVersion ||
		vars["swallow_prometheus_port"] != softwaredomain.PrometheusPort ||
		vars["swallow_prometheus_scrape_job"] != "swallow" {
		t.Errorf("trusted vars = %v", vars)
	}
	if got, _ := vars["swallow_prometheus_scrape_targets"].([]string); !slices.Equal(got, wantTargets) {
		t.Errorf("swallow_prometheus_scrape_targets = %v, want %v", vars["swallow_prometheus_scrape_targets"], wantTargets)
	}
}

// Servers to scrape travel as ids plus one port, so the playbook resolves each Server's current
// address at run time; the ids must name Servers of the target's Site.
func TestInstallPrometheusScrapeServers(t *testing.T) {
	other := deployedServer("far")
	other.Source.SiteID = "site-2"
	servers := map[string]*serverdomain.Server{"a": deployedServer("a"), "c1": deployedServer("c1"), "far": other}

	service, repo, launcher := newTestService(servers, nil)
	installedDocker(t, repo, "a")
	if _, err := service.Install(context.Background(), InstallInput{
		Kind: softwaredomain.KindPrometheus, Targets: []InstallTarget{{ServerID: "a"}},
		Spec: map[string]any{"scrapeServerIds": []any{"c1"}, "scrapePort": float64(8001)},
	}); err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	vars := launcher.lastLaunch.TrustedVars
	if got, _ := vars["swallow_prometheus_scrape_server_ids"].([]string); !slices.Equal(got, []string{"c1"}) || vars["swallow_prometheus_scrape_port"] != 8001 {
		t.Errorf("trusted vars = %v, want server ids [c1] on port 8001", vars)
	}
	assignment, _ := repo.FindByServerAndKind(context.Background(), "a", softwaredomain.KindPrometheus)
	if assignment.Spec["scrapePort"] != 8001 {
		t.Errorf("recorded scrapePort = %#v, want int 8001", assignment.Spec["scrapePort"])
	}

	tests := []struct {
		name    string
		spec    map[string]any
		wantErr error
	}{
		{name: "ids without a port", spec: map[string]any{"scrapeServerIds": []any{"c1"}}, wantErr: softwaredomain.ErrSpecInvalid},
		{name: "port without ids", spec: map[string]any{"scrapePort": float64(8001)}, wantErr: softwaredomain.ErrSpecInvalid},
		{name: "fractional port", spec: map[string]any{"scrapeServerIds": []any{"c1"}, "scrapePort": 80.5}, wantErr: softwaredomain.ErrSpecInvalid},
		{name: "port out of range", spec: map[string]any{"scrapeServerIds": []any{"c1"}, "scrapePort": float64(70000)}, wantErr: softwaredomain.ErrSpecInvalid},
		{name: "unknown Server", spec: map[string]any{"scrapeServerIds": []any{"nope"}, "scrapePort": float64(8001)}, wantErr: serverdomain.ErrServerNotFound},
		{name: "Server at another Site", spec: map[string]any{"scrapeServerIds": []any{"far"}, "scrapePort": float64(8001)}, wantErr: softwaredomain.ErrSpecInvalid},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			service, repo, _ := newTestService(servers, nil)
			installedDocker(t, repo, "a")
			_, err := service.Install(context.Background(), InstallInput{Kind: softwaredomain.KindPrometheus, Targets: []InstallTarget{{ServerID: "a"}}, Spec: tc.spec})
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("Install(%v) error = %v, want %v", tc.spec, err, tc.wantErr)
			}
		})
	}
}

func TestInstallGrafanaDefaultsToLocalPrometheus(t *testing.T) {
	service, repo, launcher := newTestService(map[string]*serverdomain.Server{"a": deployedServer("a")}, nil)
	installedDocker(t, repo, "a")
	if _, err := service.Install(context.Background(), InstallInput{
		Kind: softwaredomain.KindGrafana, Targets: []InstallTarget{{ServerID: "a"}},
		Spec: map[string]any{"dashboards": []any{"omnistat"}},
	}); err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	vars := launcher.lastLaunch.TrustedVars
	if vars["swallow_grafana_version"] != softwaredomain.DefaultGrafanaVersion ||
		vars["swallow_grafana_port"] != softwaredomain.GrafanaPort ||
		vars["swallow_grafana_prometheus_url"] != "" ||
		vars["swallow_grafana_prometheus_port"] != softwaredomain.PrometheusPort {
		t.Errorf("trusted vars = %v", vars)
	}
	if got, _ := vars["swallow_grafana_dashboards"].([]string); !slices.Equal(got, []string{"omnistat"}) {
		t.Errorf("swallow_grafana_dashboards = %v, want [omnistat]", vars["swallow_grafana_dashboards"])
	}
}

func TestInstallObservabilityRejectsInvalidSpec(t *testing.T) {
	tests := []struct {
		name string
		kind softwaredomain.Kind
		spec map[string]any
	}{
		{name: "prometheus tag with registry", kind: softwaredomain.KindPrometheus, spec: map[string]any{"version": "evil.io/prom:1"}},
		{name: "prometheus non-string version", kind: softwaredomain.KindPrometheus, spec: map[string]any{"version": 3}},
		{name: "prometheus partial version", kind: softwaredomain.KindPrometheus, spec: map[string]any{"version": "3.15"}},
		{name: "prometheus unknown method", kind: softwaredomain.KindPrometheus, spec: map[string]any{"installMethod": "snap"}},
		{name: "grafana runtime that is not a runtime", kind: softwaredomain.KindGrafana, spec: map[string]any{"containerRuntime": "nfs"}},
		{name: "prometheus target without port", kind: softwaredomain.KindPrometheus, spec: map[string]any{"scrapeTargets": []any{"10.0.0.2"}}},
		{name: "prometheus target with scheme", kind: softwaredomain.KindPrometheus, spec: map[string]any{"scrapeTargets": []any{"http://10.0.0.2:8001"}}},
		{name: "prometheus targets not a list", kind: softwaredomain.KindPrometheus, spec: map[string]any{"scrapeTargets": "10.0.0.2:8001"}},
		{name: "prometheus bad job", kind: softwaredomain.KindPrometheus, spec: map[string]any{"scrapeJob": "has space"}},
		{name: "grafana relative datasource", kind: softwaredomain.KindGrafana, spec: map[string]any{"prometheusUrl": "prom:9090"}},
		{name: "grafana unknown bundle", kind: softwaredomain.KindGrafana, spec: map[string]any{"dashboards": []any{"k8s"}}},
		{name: "grafana non-string bundle", kind: softwaredomain.KindGrafana, spec: map[string]any{"dashboards": []any{1}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			service, repo, _ := newTestService(map[string]*serverdomain.Server{"a": deployedServer("a")}, nil)
			installedDocker(t, repo, "a")
			_, err := service.Install(context.Background(), InstallInput{Kind: tc.kind, Targets: []InstallTarget{{ServerID: "a"}}, Spec: tc.spec})
			if !errors.Is(err, softwaredomain.ErrSpecInvalid) {
				t.Fatalf("Install(%v) error = %v, want ErrSpecInvalid", tc.spec, err)
			}
		})
	}
}

// A platform deployment composes software onto Servers it is still provisioning and installs the
// required kinds in earlier Jobs, so the plan skips those two checks but keeps every other rule;
// nothing is written until the composing Workflow exists.
func TestPlanComposedInstallSkipsStateAndRequiresButValidates(t *testing.T) {
	ready := deployedServer("a")
	ready.Provisioning.State = "ready"
	service, repo, launcher := newTestService(map[string]*serverdomain.Server{"a": ready}, nil)

	plan, err := service.PlanComposedInstall(context.Background(), InstallInput{
		Kind: softwaredomain.KindPrometheus, Targets: []InstallTarget{{ServerID: "a"}},
		Spec: map[string]any{"scrapeJob": "omnistat", "scrapeTargets": []string{"10.0.0.2:8001"}},
	})
	if err != nil {
		t.Fatalf("PlanComposedInstall() error = %v", err)
	}
	if launcher.install {
		t.Error("planning must not launch a Workflow")
	}
	if _, err := repo.FindByServerAndKind(context.Background(), "a", softwaredomain.KindPrometheus); !errors.Is(err, softwaredomain.ErrAssignmentNotFound) {
		t.Errorf("planning wrote an assignment (err = %v)", err)
	}
	if plan.TrustedVars["swallow_prometheus_scrape_job"] != "omnistat" || plan.Spec["version"] != softwaredomain.DefaultPrometheusVersion ||
		plan.Spec["containerRuntime"] != "docker-ce" {
		t.Errorf("plan = %+v, want the default version on the default runtime", plan)
	}
	podmanPlan, err := service.PlanComposedInstall(context.Background(), InstallInput{
		Kind: softwaredomain.KindGrafana, Targets: []InstallTarget{{ServerID: "a"}},
		Spec: map[string]any{"containerRuntime": "podman"},
	})
	if err != nil || podmanPlan.TrustedVars["swallow_grafana_container_runtime"] != "podman" {
		t.Errorf("PlanComposedInstall(podman) = %+v, %v; want the runtime the platform installs", podmanPlan, err)
	}

	if err := service.RecordComposedInstall(context.Background(), plan, "slurm-deploy"); err != nil {
		t.Fatalf("RecordComposedInstall() error = %v", err)
	}
	assignment, err := repo.FindByServerAndKind(context.Background(), "a", softwaredomain.KindPrometheus)
	if err != nil {
		t.Fatalf("assignment: %v", err)
	}
	if assignment.State != softwaredomain.StatePending || assignment.LastWorkflowID != "slurm-deploy" || assignment.SiteID != "site-1" {
		t.Errorf("assignment = %+v, want pending under slurm-deploy", assignment)
	}

	if _, err := service.PlanComposedInstall(context.Background(), InstallInput{
		Kind: softwaredomain.KindGrafana, Targets: []InstallTarget{{ServerID: "a"}},
		Spec: map[string]any{"dashboards": []string{"nope"}},
	}); !errors.Is(err, softwaredomain.ErrSpecInvalid) {
		t.Errorf("PlanComposedInstall(bad spec) error = %v, want ErrSpecInvalid", err)
	}
	_ = repo.Upsert(context.Background(), &softwaredomain.Assignment{ServerID: "a", Kind: softwaredomain.KindPodman, State: softwaredomain.StateInstalled})
	if _, err := service.PlanComposedInstall(context.Background(), InstallInput{
		Kind: softwaredomain.KindDockerCE, Targets: []InstallTarget{{ServerID: "a"}},
	}); !errors.Is(err, softwaredomain.ErrMutuallyExclusive) {
		t.Errorf("PlanComposedInstall(docker over podman) error = %v, want ErrMutuallyExclusive", err)
	}
}
