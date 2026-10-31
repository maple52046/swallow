package application

import (
	"context"
	"errors"
	"strings"
	"testing"

	serverdomain "github.com/maple52046/swallow/internal/core/server/domain"
	softwaredomain "github.com/maple52046/swallow/internal/core/software/domain"
)

// gpuServer is a deployed Server whose GPU Inventory lists AMD compute GPUs beside a console GPU.
func gpuServer(id string) *serverdomain.Server {
	server := deployedServer(id)
	server.Observed.GPUs = []serverdomain.GPU{
		{Vendor: "Red Hat, Inc.", Model: "Virtio 1.0 GPU", Count: 1, Kind: serverdomain.GPUKindDisplay},
		{Vendor: "Advanced Micro Devices, Inc. [AMD/ATI]", Count: 4, Kind: serverdomain.GPUKindCompute},
	}
	return server
}

type fakePlatformNames map[string][2]string

func (n fakePlatformNames) PlatformName(_ context.Context, platformID string) (string, string, error) {
	if entry, ok := n[platformID]; ok {
		return entry[0], entry[1], nil
	}
	return "", "", errors.New("platform not found")
}

func TestDeviceMetricsExporterStandaloneInstall(t *testing.T) {
	service, repo, launcher := newTestService(map[string]*serverdomain.Server{"a": gpuServer("a")}, nil)
	if _, err := service.Install(context.Background(), InstallInput{
		Kind: softwaredomain.KindDeviceMetricsExporter, Targets: []InstallTarget{{ServerID: "a"}},
	}); err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	vars := launcher.lastLaunch.TrustedVars
	if vars["swallow_device_metrics_exporter_version"] != softwaredomain.DeviceMetricsExporterVersion ||
		vars["swallow_device_metrics_exporter_port"] != softwaredomain.DeviceMetricsExporterPort ||
		vars["swallow_device_metrics_exporter_job_attribution"] != string(softwaredomain.JobAttributionNone) {
		t.Fatalf("trusted vars = %v, want the pinned version, port 5001, and no job attribution", vars)
	}
	assignment, err := repo.FindByServerAndKind(context.Background(), "a", softwaredomain.KindDeviceMetricsExporter)
	if err != nil || assignment.Spec[softwaredomain.SpecVersion] != softwaredomain.DeviceMetricsExporterVersion {
		t.Fatalf("assignment = %+v, %v; want the pinned version recorded", assignment, err)
	}
}

// Each standalone refusal is a specific error before any Workflow is launched.
func TestDeviceMetricsExporterStandaloneRefusals(t *testing.T) {
	noGPU := deployedServer("no-gpu")
	claimed := gpuServer("claimed")
	claimed.PlatformClaim = &serverdomain.PlatformClaim{PlatformID: "platform-a"}
	member := gpuServer("member")
	cases := []struct {
		name   string
		server string
		spec   map[string]any
		want   error
		text   string
	}{
		{name: "no AMD compute GPU", server: "no-gpu", want: softwaredomain.ErrTargetIneligible, text: "no AMD compute GPU"},
		{name: "Platform claim", server: "claimed", want: softwaredomain.ErrPlatformClaimed, text: "belongs to slurm Platform lab-slurm"},
		{name: "Kubernetes member", server: "member", want: softwaredomain.ErrKubernetesMember},
		{name: "spec fields", server: "member", spec: map[string]any{"version": "1.5.3"}, want: softwaredomain.ErrSpecInvalid},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			service, _, launcher := newTestService(
				map[string]*serverdomain.Server{"no-gpu": noGPU, "claimed": claimed, "member": member},
				map[string]bool{"member": true},
			)
			service.AttachPlatformNames(fakePlatformNames{"platform-a": {"lab-slurm", "slurm"}})
			_, err := service.Install(context.Background(), InstallInput{
				Kind: softwaredomain.KindDeviceMetricsExporter, Targets: []InstallTarget{{ServerID: tc.server}}, Spec: tc.spec,
			})
			if !errors.Is(err, tc.want) || !strings.Contains(err.Error(), tc.text) {
				t.Fatalf("Install() error = %v, want %v containing %q", err, tc.want, tc.text)
			}
			if launcher.install {
				t.Fatal("a refused install launched a Workflow")
			}
		})
	}
}

// A Slurm deployment composes the kind onto Servers its Platform claims; the claim refusal exists
// to leave those Servers to the Platform, so it does not apply to the Platform itself.
func TestDeviceMetricsExporterComposedOntoClaimedServers(t *testing.T) {
	claimed := gpuServer("a")
	claimed.PlatformClaim = &serverdomain.PlatformClaim{PlatformID: "platform-a"}
	service, _, _ := newTestService(map[string]*serverdomain.Server{"a": claimed}, nil)
	install, err := service.PlanComposedInstall(context.Background(), InstallInput{
		Kind: softwaredomain.KindDeviceMetricsExporter, Targets: []InstallTarget{{ServerID: "a"}},
	})
	if err != nil {
		t.Fatalf("PlanComposedInstall() error = %v", err)
	}
	if install.TrustedVars["swallow_device_metrics_exporter_port"] != softwaredomain.DeviceMetricsExporterPort {
		t.Fatalf("trusted vars = %v, want the exporter port", install.TrustedVars)
	}
}

func TestDeviceMetricsExporterUninstallRefusesClaimedServers(t *testing.T) {
	server := gpuServer("a")
	service, repo, launcher := newTestService(map[string]*serverdomain.Server{"a": server}, nil)
	_ = repo.Upsert(context.Background(), &softwaredomain.Assignment{
		ServerID: "a", Kind: softwaredomain.KindDeviceMetricsExporter, State: softwaredomain.StateInstalled,
		Spec: map[string]any{softwaredomain.SpecVersion: softwaredomain.DeviceMetricsExporterVersion},
	})

	server.PlatformClaim = &serverdomain.PlatformClaim{PlatformID: "platform-a"}
	_, err := service.Uninstall(context.Background(), UninstallInput{Kind: softwaredomain.KindDeviceMetricsExporter, ServerIDs: []string{"a"}})
	if !errors.Is(err, softwaredomain.ErrPlatformClaimed) || !strings.Contains(err.Error(), "Platform platform-a") {
		t.Fatalf("Uninstall() error = %v, want ErrPlatformClaimed naming the Platform by id", err)
	}
	if launcher.uninstall {
		t.Fatal("a refused uninstall launched a Workflow")
	}

	server.PlatformClaim = nil
	if _, err := service.Uninstall(context.Background(), UninstallInput{Kind: softwaredomain.KindDeviceMetricsExporter, ServerIDs: []string{"a"}}); err != nil {
		t.Fatalf("Uninstall() of an unclaimed Server error = %v", err)
	}
	if launcher.lastLaunch.TrustedVars["swallow_device_metrics_exporter_version"] != softwaredomain.DeviceMetricsExporterVersion {
		t.Fatalf("uninstall vars = %v, want the recorded version", launcher.lastLaunch.TrustedVars)
	}
}
