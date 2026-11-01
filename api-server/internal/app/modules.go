package app

import (
	"fmt"

	moduledomain "github.com/maple52046/swallow/internal/core/module/domain"
	"github.com/maple52046/swallow/modules/accelerator/amd"
	"github.com/maple52046/swallow/modules/platform/kubernetes"
	"github.com/maple52046/swallow/modules/platform/slurm"
	devicemetricsexporter "github.com/maple52046/swallow/modules/software/device-metrics-exporter"
	dockerce "github.com/maple52046/swallow/modules/software/docker-ce"
	"github.com/maple52046/swallow/modules/software/grafana"
	"github.com/maple52046/swallow/modules/software/nfs"
	"github.com/maple52046/swallow/modules/software/podman"
	"github.com/maple52046/swallow/modules/software/prometheus"
)

// compiledModules are the modules this build includes, by their root package's Module function.
var compiledModules = []func() (moduledomain.Module, error){
	kubernetes.Module, slurm.Module, amd.Module,
	dockerce.Module, podman.Module, nfs.Module, prometheus.Module, grafana.Module, devicemetricsexporter.Module,
}

// moduleRegistry returns the registry of the modules compiled into this build (ADR 075). api,
// worker, and ansible-executor all build it here, so they agree on which modules, Workflow kinds,
// and completion types exist; a module that breaks the contract stops the process.
func moduleRegistry() (*moduledomain.Registry, error) {
	modules := make([]moduledomain.Module, 0, len(compiledModules))
	for _, load := range compiledModules {
		module, err := load()
		if err != nil {
			return nil, fmt.Errorf("load module: %w", err)
		}
		modules = append(modules, module)
	}
	registry, err := moduledomain.NewRegistry(modules...)
	if err != nil {
		return nil, fmt.Errorf("assemble modules: %w", err)
	}
	return registry, nil
}

// coreWorkflowKinds are the Workflow kinds the core plans itself; every other kind belongs to
// exactly one module.
var coreWorkflowKinds = []string{
	"deploy-os", "release-os", "recover-server", "verify-os-image", "inspect-hardware",
	"enroll-virtual-machines", "install-exporters", "uninstall-exporters", "configure-ntp", "custom",
}

// coreCompletions are the completion types the core declares itself; every other type belongs to
// exactly one module.
var coreCompletions = []string{
	completionServerDeployment, completionServerDeploymentCleared, completionSoftwareFailure,
	completionPlatformUninstall,
}
