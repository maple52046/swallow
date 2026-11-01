package app

import (
	"fmt"
	"slices"

	"github.com/maple52046/swallow/internal/adapters/metrics/promstack"
	"github.com/maple52046/swallow/internal/adapters/osprovisioning/maas"
	moduledomain "github.com/maple52046/swallow/internal/core/module/domain"
	provisioningdomain "github.com/maple52046/swallow/internal/core/provisioning/domain"
	sitedomain "github.com/maple52046/swallow/internal/core/site/domain"
	softwaredomain "github.com/maple52046/swallow/internal/core/software/domain"
	workflowdomain "github.com/maple52046/swallow/internal/core/workflow/domain"
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

// composedModules is what the compiled modules give a process: the registry and what the core
// contexts read from it.
type composedModules struct {
	registry *moduledomain.Registry
	// software is the Managed Software catalog, one kind per Software module.
	software *softwaredomain.Catalog
	// workflowKinds are the Workflow kinds the build accepts.
	workflowKinds workflowdomain.WorkflowKinds
}

// composeModules assembles the registry and derives the software catalog and the accepted Workflow
// kinds from it. api, worker, and ansible-executor all call it first, so a module that breaks the
// contract stops every process the same way.
func composeModules() (composedModules, error) {
	registry, err := moduleRegistry()
	if err != nil {
		return composedModules{}, err
	}
	software, err := softwareCatalog(registry)
	if err != nil {
		return composedModules{}, err
	}
	kinds, err := workflowKinds(registry)
	if err != nil {
		return composedModules{}, err
	}
	return composedModules{registry: registry, software: software, workflowKinds: kinds}, nil
}

// softwareCatalog collects the Software hook of every Software module. A module's id is its kind,
// and its manifest declares the kind's install and uninstall Workflow kinds and playbooks, which the
// software launcher derives from the kind.
func softwareCatalog(registry *moduledomain.Registry) (*softwaredomain.Catalog, error) {
	var software []softwaredomain.Software
	for _, module := range registry.Modules() {
		manifest := module.Manifest
		if manifest.Kind != moduledomain.KindSoftware {
			continue
		}
		hook, ok := module.Hooks[softwaredomain.HookSoftware].(softwaredomain.Software)
		if !ok {
			return nil, fmt.Errorf("software module %s has no %q hook", manifest.Ref(), softwaredomain.HookSoftware)
		}
		kind := hook.Entry().Kind
		if string(kind) != manifest.ID {
			return nil, fmt.Errorf("software module %s provides kind %q, want its id", manifest.Ref(), kind)
		}
		for _, declared := range []struct {
			list []string
			name string
		}{
			{manifest.WorkflowKinds, kind.InstallWorkflowKind()}, {manifest.WorkflowKinds, kind.UninstallWorkflowKind()},
			{manifest.Playbooks, kind.InstallPlaybook()}, {manifest.Playbooks, kind.UninstallPlaybook()},
		} {
			if !slices.Contains(declared.list, declared.name) {
				return nil, fmt.Errorf("software module %s does not declare %q", manifest.Ref(), declared.name)
			}
		}
		software = append(software, hook)
	}
	return softwaredomain.NewCatalog(software...)
}

// workflowKinds adds the Workflow kinds of every Software module to the built-in ones, requiring
// deployed targets as software deployment does. The Platform and Accelerator modules' kinds are
// still built in to the workflow context.
func workflowKinds(registry *moduledomain.Registry) (workflowdomain.WorkflowKinds, error) {
	kinds := workflowdomain.WorkflowKinds{}
	for _, module := range registry.Modules() {
		if module.Manifest.Kind != moduledomain.KindSoftware {
			continue
		}
		declared := make([]workflowdomain.WorkflowKind, 0, len(module.Manifest.WorkflowKinds))
		for _, kind := range module.Manifest.WorkflowKinds {
			declared = append(declared, workflowdomain.WorkflowKind(kind))
		}
		var err error
		if kinds, err = kinds.With(softwaredomain.RequiredProvisioningState, declared...); err != nil {
			return workflowdomain.WorkflowKinds{}, fmt.Errorf("module %s: %w", module.Manifest.Ref(), err)
		}
	}
	return kinds, nil
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
