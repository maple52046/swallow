package app

import (
	"context"
	"fmt"
	"slices"

	discoveryapp "github.com/maple52046/swallow/internal/core/discovery/application"
	platformdomain "github.com/maple52046/swallow/internal/core/platform/domain"
	serverdomain "github.com/maple52046/swallow/internal/core/server/domain"
	softwaredomain "github.com/maple52046/swallow/internal/core/software/domain"
	"github.com/maple52046/swallow/modules/platform/kubernetes"
	"github.com/maple52046/swallow/modules/platform/slurm"
)

// installedExporters is the discovery context's ExporterInventory, over three sources.
//
// Managed Software: a kind whose catalog entry declares an exporter runs it on every Server whose
// assignment of that kind is installed, or uninstalling (the package is still there until the
// uninstall finishes).
//
// Platform features: a Slurm Platform whose Omnistat is ready runs the Omnistat exporter on every
// compute member of its applied spec (the spec the last successful Workflow realized; the spec
// itself for a Platform with none applied yet). A Kubernetes Platform whose GPU Operator is ready
// with its metrics exporter on runs the Device Metrics Exporter, as the Managed Software kind
// declares it, on every workload member of its applied spec whose GPU Inventory lists the
// accelerator's compute GPUs: those are the nodes the operator's Node Feature Discovery manages, as
// far as swallow knows them.
//
// A pending or failed install is not reported, nor is a feature that is installing again: the
// exporter may not be running, and a target that cannot answer only produces failing series. A
// Server and exporter two sources name is reported once.
type installedExporters struct {
	catalog     *softwaredomain.Catalog
	assignments softwaredomain.AssignmentRepository
	platforms   exporterPlatforms
	// servers reads the GPU Inventory of the GPU Operator's candidate members, and accelerator is
	// the Accelerator module whose compute GPUs the operator serves. Without either, no GPU Operator
	// exporter is reported.
	servers     exporterServers
	accelerator platformdomain.Accelerator
}

// exporterPlatforms lists every Platform with its feature records and spec, in every Site.
type exporterPlatforms interface {
	List(ctx context.Context, siteID string) ([]*platformdomain.Platform, error)
}

// exporterServers reads the Servers a filter names.
type exporterServers interface {
	List(ctx context.Context, filter serverdomain.ListFilter) (serverdomain.ListResult, error)
}

// InstalledExporters lists the declared exporters of every installed assignment and ready
// Platform feature.
func (e installedExporters) InstalledExporters(ctx context.Context) ([]discoveryapp.InstalledExporter, error) {
	var exporters []discoveryapp.InstalledExporter
	for _, entry := range e.catalog.Entries() {
		if entry.Exporter == nil {
			continue
		}
		assignments, err := e.assignments.List(ctx, softwaredomain.AssignmentFilter{Kind: entry.Kind})
		if err != nil {
			return nil, fmt.Errorf("list the %s assignments: %w", entry.Kind, err)
		}
		for _, assignment := range assignments {
			if assignment.State != softwaredomain.StateInstalled && assignment.State != softwaredomain.StateUninstalling {
				continue
			}
			exporters = append(exporters, discoveryapp.InstalledExporter{
				ServerID: assignment.ServerID, Name: entry.Exporter.Name, Port: entry.Exporter.Port,
			})
		}
	}
	platforms, err := e.platforms.List(ctx, "")
	if err != nil {
		return nil, fmt.Errorf("list the Platforms: %w", err)
	}
	var gpuCandidates []string
	for _, platform := range platforms {
		for _, serverID := range omnistatExporterServers(platform) {
			exporters = append(exporters, discoveryapp.InstalledExporter{
				ServerID: serverID, Name: platformdomain.OmnistatScrapeJob, Port: platformdomain.OmnistatExporterPort,
			})
		}
		gpuCandidates = append(gpuCandidates, gpuOperatorExporterCandidates(platform)...)
	}
	operatorExporters, err := e.gpuOperatorExporters(ctx, gpuCandidates)
	if err != nil {
		return nil, err
	}
	return uniqueExporters(append(exporters, operatorExporters...)), nil
}

// gpuOperatorExporters returns the Device Metrics Exporter on each candidate whose GPU Inventory
// lists the accelerator's compute GPUs, in one Server read.
func (e installedExporters) gpuOperatorExporters(ctx context.Context, candidates []string) ([]discoveryapp.InstalledExporter, error) {
	software, ok := e.catalog.Lookup(softwaredomain.KindDeviceMetricsExporter)
	if len(candidates) == 0 || !ok || software.Entry().Exporter == nil || e.servers == nil || e.accelerator == nil {
		return nil, nil
	}
	declared := software.Entry().Exporter
	result, err := e.servers.List(ctx, serverdomain.ListFilter{IDs: candidates})
	if err != nil {
		return nil, fmt.Errorf("read the GPU Operator's members: %w", err)
	}
	var exporters []discoveryapp.InstalledExporter
	for _, server := range result.Servers {
		if platformdomain.HasComputeGPU(e.accelerator, server.Observed.GPUs) {
			exporters = append(exporters, discoveryapp.InstalledExporter{ServerID: server.ID, Name: declared.Name, Port: declared.Port})
		}
	}
	return exporters, nil
}

// uniqueExporters drops a repeated Server and exporter, keeping the first.
func uniqueExporters(exporters []discoveryapp.InstalledExporter) []discoveryapp.InstalledExporter {
	seen := make(map[discoveryapp.InstalledExporter]bool, len(exporters))
	unique := exporters[:0]
	for _, exporter := range exporters {
		if !seen[exporter] {
			seen[exporter] = true
			unique = append(unique, exporter)
		}
	}
	return unique
}

// realizedSpec is the spec a Platform's exporters were installed for: the applied spec, else the
// spec itself for a Platform with none applied yet.
func realizedSpec(platform *platformdomain.Platform) *platformdomain.PlatformSpec {
	if platform.Status.Applied != nil {
		return platform.Status.Applied
	}
	return platform.Spec
}

// omnistatExporterServers returns the compute members of a Slurm Platform whose Omnistat is ready,
// which configure-omnistat installed the exporter on; none for any other Platform.
func omnistatExporterServers(platform *platformdomain.Platform) []string {
	if platform.Type != platformdomain.PlatformTypeSlurm || platform.Omnistat == nil ||
		platform.Omnistat.State != platformdomain.OmnistatReady {
		return nil
	}
	spec := realizedSpec(platform)
	if spec == nil {
		return nil
	}
	var serverIDs []string
	for _, member := range spec.Members {
		if slices.Contains(member.Roles, slurm.RoleCompute) {
			serverIDs = append(serverIDs, member.ServerID)
		}
	}
	return serverIDs
}

// gpuOperatorExporterCandidates returns the workload members of a Kubernetes Platform whose GPU
// Operator is ready with its metrics exporter on; none for any other Platform. A controller that
// runs no workloads is not a Kubernetes node, so the operator runs nothing there.
func gpuOperatorExporterCandidates(platform *platformdomain.Platform) []string {
	operator := platform.GPUOperator
	if platform.Type != platformdomain.PlatformTypeKubernetes || operator == nil ||
		operator.State != platformdomain.GPUOperatorReady || !operator.Settings.Enabled || !operator.Settings.MetricsExporter {
		return nil
	}
	spec := realizedSpec(platform)
	if spec == nil {
		return nil
	}
	var serverIDs []string
	for _, member := range spec.Members {
		if (kubernetes.Platform{}).ReportsMembership(member) {
			serverIDs = append(serverIDs, member.ServerID)
		}
	}
	return serverIDs
}
