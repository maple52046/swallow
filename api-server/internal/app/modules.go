package app

import (
	"fmt"
	"slices"

	"github.com/maple52046/swallow/internal/adapters/localrepo/pulp"
	"github.com/maple52046/swallow/internal/adapters/metrics/promstack"
	"github.com/maple52046/swallow/internal/adapters/osprovisioning/maas"
	localrepodomain "github.com/maple52046/swallow/internal/core/localrepo/domain"
	moduledomain "github.com/maple52046/swallow/internal/core/module/domain"
	platformdomain "github.com/maple52046/swallow/internal/core/platform/domain"
	provisioningdomain "github.com/maple52046/swallow/internal/core/provisioning/domain"
	sitedomain "github.com/maple52046/swallow/internal/core/site/domain"
	softwaredomain "github.com/maple52046/swallow/internal/core/software/domain"
	workflowdomain "github.com/maple52046/swallow/internal/core/workflow/domain"
	"github.com/maple52046/swallow/modules/accelerator/amd"
	"github.com/maple52046/swallow/modules/platform/kubernetes"
	"github.com/maple52046/swallow/modules/platform/kubernetes/kubeapi"
	"github.com/maple52046/swallow/modules/platform/slurm"
	"github.com/maple52046/swallow/modules/platform/slurm/slurmapi"
	devicemetricsexporter "github.com/maple52046/swallow/modules/software/device-metrics-exporter"
	dockerce "github.com/maple52046/swallow/modules/software/docker-ce"
	"github.com/maple52046/swallow/modules/software/grafana"
	"github.com/maple52046/swallow/modules/software/nfs"
	nodeexporter "github.com/maple52046/swallow/modules/software/node-exporter"
	"github.com/maple52046/swallow/modules/software/podman"
	"github.com/maple52046/swallow/modules/software/prometheus"
	"github.com/maple52046/swallow/modules/software/victoriametrics"
)

// compiledModules are the modules this build includes, by their root package's Module function.
var compiledModules = []func() (moduledomain.Module, error){
	kubernetes.Module, slurm.Module, amd.Module,
	dockerce.Module, podman.Module, nfs.Module, prometheus.Module, grafana.Module, devicemetricsexporter.Module,
	nodeexporter.Module, victoriametrics.Module,
}

// moduleRegistry returns the registry of the modules compiled into this build (decision 075). api,
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
	// platformReaders are the Platform modules' reader builders, one per Platform type.
	platformReaders []platformdomain.ReaderBuilder
	// accelerators are the Accelerator modules' accelerator hooks, by module id.
	accelerators platformdomain.Accelerators
	// credentials are the Platform modules' credential readers.
	credentials []platformdomain.CredentialReader
	// platforms are the Platform modules' platform hooks, by Platform type.
	platforms map[platformdomain.PlatformType]platformdomain.PlatformModule
	// artifacts are the modules' declarations of what their deployments fetch, from the Site's
	// Local Repository or from these upstreams.
	artifacts []localrepodomain.ModuleDeclaration
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
	readers, err := platformReaders(registry)
	if err != nil {
		return composedModules{}, err
	}
	accelerators, err := moduleAccelerators(registry)
	if err != nil {
		return composedModules{}, err
	}
	credentials, err := credentialReaders(registry)
	if err != nil {
		return composedModules{}, err
	}
	platforms, err := platformModules(registry)
	if err != nil {
		return composedModules{}, err
	}
	return composedModules{
		registry: registry, software: software, workflowKinds: kinds, platformReaders: readers,
		accelerators: accelerators, credentials: credentials, platforms: platforms,
		artifacts: localRepositoryDeclarations(registry),
	}, nil
}

// localRepositoryDeclarations restates the modules' artifact declarations in the Local Repository's
// terms, with each OS package source's signing key. The registry has validated them and their
// agreement across modules.
func localRepositoryDeclarations(registry *moduledomain.Registry) []localrepodomain.ModuleDeclaration {
	var declarations []localrepodomain.ModuleDeclaration
	for _, declared := range registry.Artifacts() {
		declaration := localrepodomain.ModuleDeclaration{
			ModuleID: declared.ModuleID, Images: declared.Artifacts.Images, Python: declared.Artifacts.Python,
		}
		for _, source := range declared.Artifacts.Sources {
			declaration.Sources = append(declaration.Sources, localrepodomain.Source{
				Name: source.Name, Format: localrepodomain.Format(source.Format), URL: source.URL,
				Suites: source.Suites, Components: source.Components, Architectures: source.Architectures,
			})
			if key := source.SigningKey; key != nil {
				if declaration.SigningKeys == nil {
					declaration.SigningKeys = map[string]localrepodomain.SigningKey{}
				}
				declaration.SigningKeys[source.Name] = localrepodomain.SigningKey{URL: key.URL, Fingerprint: key.Fingerprint}
			}
		}
		for _, file := range declared.Artifacts.Files {
			declaration.Files = append(declaration.Files, localrepodomain.File{Path: file.Path, URL: file.URL, SHA256: file.SHA256})
		}
		declarations = append(declarations, declaration)
	}
	return declarations
}

// platformModules collects the platform hook of every Platform module, by the Platform type it
// handles; a module's id is that type.
func platformModules(registry *moduledomain.Registry) (map[platformdomain.PlatformType]platformdomain.PlatformModule, error) {
	platforms := map[platformdomain.PlatformType]platformdomain.PlatformModule{}
	for _, module := range registry.Modules() {
		if module.Manifest.Kind != moduledomain.KindPlatform {
			continue
		}
		hook, ok := module.Hooks[platformdomain.HookPlatform].(platformdomain.PlatformModule)
		if !ok {
			return nil, fmt.Errorf("platform module %s has no %q hook", module.Manifest.Ref(), platformdomain.HookPlatform)
		}
		if string(hook.PlatformType()) != module.Manifest.ID {
			return nil, fmt.Errorf("platform module %s handles Platform type %q, want its id", module.Manifest.Ref(), hook.PlatformType())
		}
		platforms[hook.PlatformType()] = hook
	}
	return platforms, nil
}

// credentialReaders collects the platform-credential hook of every Platform module; the completion
// type a reader handles must be one its manifest declares.
func credentialReaders(registry *moduledomain.Registry) ([]platformdomain.CredentialReader, error) {
	var readers []platformdomain.CredentialReader
	for _, module := range registry.Modules() {
		if module.Manifest.Kind != moduledomain.KindPlatform {
			continue
		}
		hook, ok := module.Hooks[platformdomain.HookCredential].(platformdomain.CredentialReader)
		if !ok {
			return nil, fmt.Errorf("platform module %s has no %q hook", module.Manifest.Ref(), platformdomain.HookCredential)
		}
		if !slices.Contains(module.Manifest.Completions, hook.Completion()) {
			return nil, fmt.Errorf("platform module %s reads credentials of completion %q, which its manifest does not declare",
				module.Manifest.Ref(), hook.Completion())
		}
		readers = append(readers, hook)
	}
	return readers, nil
}

// moduleAccelerators collects the accelerator hook of every Accelerator module; a module's id is
// its accelerator's id.
func moduleAccelerators(registry *moduledomain.Registry) (platformdomain.Accelerators, error) {
	accelerators := platformdomain.Accelerators{}
	for _, module := range registry.Modules() {
		if module.Manifest.Kind != moduledomain.KindAccelerator {
			continue
		}
		hook, ok := module.Hooks[platformdomain.HookAccelerator].(platformdomain.Accelerator)
		if !ok {
			return nil, fmt.Errorf("accelerator module %s has no %q hook", module.Manifest.Ref(), platformdomain.HookAccelerator)
		}
		if hook.ID() != module.Manifest.ID {
			return nil, fmt.Errorf("accelerator module %s provides accelerator %q, want its id", module.Manifest.Ref(), hook.ID())
		}
		accelerators[hook.ID()] = hook
	}
	return accelerators, nil
}

// platformReaders collects the platform-reader hook of every Platform module.
func platformReaders(registry *moduledomain.Registry) ([]platformdomain.ReaderBuilder, error) {
	var readers []platformdomain.ReaderBuilder
	for _, module := range registry.Modules() {
		if module.Manifest.Kind != moduledomain.KindPlatform {
			continue
		}
		hook, ok := module.Hooks[platformdomain.HookPlatformReader].(platformdomain.ReaderBuilder)
		if !ok {
			return nil, fmt.Errorf("platform module %s has no %q hook", module.Manifest.Ref(), platformdomain.HookPlatformReader)
		}
		readers = append(readers, hook)
	}
	return readers, nil
}

// softwareCatalog collects the Software hook of every Software module, with the spec schema the
// module's manifest declares. A module's id is its kind, and its manifest declares the kind's install
// and uninstall Workflow kinds and playbooks, which the software launcher derives from the kind.
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
	catalog, err := softwaredomain.NewCatalog(software...)
	if err != nil {
		return nil, err
	}
	for _, module := range registry.Modules() {
		if module.Manifest.Kind != moduledomain.KindSoftware {
			continue
		}
		schema, err := module.Manifest.SpecSchema()
		if err != nil {
			return nil, err
		}
		if schema == nil {
			continue
		}
		if err := catalog.AttachSpecSchema(softwaredomain.Kind(module.Manifest.ID), schema); err != nil {
			return nil, err
		}
	}
	return catalog, nil
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
		sitedomain.IntegrationKindPlatform:    {kubeapi.ProviderKind, slurmapi.ProviderKind},
		sitedomain.IntegrationKindRepository:  {pulp.ProviderKind},
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
	"enroll-virtual-machines", "configure-ntp", "custom",
}

// coreCompletions are the completion types the core declares itself; every other type belongs to
// exactly one module.
var coreCompletions = []string{
	completionServerDeployment, completionServerDeploymentCleared, completionSoftwareFailure,
	completionPlatformUninstall, completionPlatformGrafanaResult,
}
