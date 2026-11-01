package app

import (
	"fmt"

	"github.com/maple52046/swallow/internal/adapters/metrics/promstack"
	"github.com/maple52046/swallow/internal/adapters/osprovisioning/maas"
	moduledomain "github.com/maple52046/swallow/internal/core/module/domain"
	provisioningdomain "github.com/maple52046/swallow/internal/core/provisioning/domain"
	sitedomain "github.com/maple52046/swallow/internal/core/site/domain"
	"github.com/maple52046/swallow/modules/accelerator/amd"
	"github.com/maple52046/swallow/modules/platform/kubernetes"
	"github.com/maple52046/swallow/modules/platform/platformapi"
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

// integrationProviders are the provider kinds the adapters and Platform modules compiled into this
// build offer for each Integration kind; the core validates Integrations against it.
func integrationProviders() sitedomain.IntegrationProviders {
	return sitedomain.IntegrationProviders{
		sitedomain.IntegrationKindProvisioner: {maas.ProviderKind},
		sitedomain.IntegrationKindMetrics:     {promstack.ProviderKind},
		sitedomain.IntegrationKindPlatform:    {platformapi.ProviderKindKubernetes, platformapi.ProviderKindSlurm},
	}
}

// bootChainRenderers are the Boot ISO chain renderers of the provisioner adapters compiled into
// this build, by the provider kind of their Integration.
func bootChainRenderers() provisioningdomain.BootChainRenderers {
	return provisioningdomain.BootChainRenderers{maas.ProviderKind: maas.BootChainRenderer{}}
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
