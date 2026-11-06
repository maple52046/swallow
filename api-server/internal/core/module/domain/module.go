// Package domain is the module contract (decision 075, core-and-extensions.md §5): the manifest a
// module declares itself with, and the registry the composition root assembles from the modules
// compiled into a build.
package domain

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// ContractVersion1 is the only module contract version this core supports.
const ContractVersion1 = 1

// SupportedContractVersions are the module contract versions the core loads; any other version is
// refused at start.
var SupportedContractVersions = []int{ContractVersion1}

// Kind is what a module adds to swallow: a Platform runtime, a GPU vendor's accelerators, or one
// piece of host software (glossary: Extension Module).
type Kind string

// The module kinds.
const (
	KindPlatform    Kind = "platform"
	KindAccelerator Kind = "accelerator"
	KindSoftware    Kind = "software"
)

// Valid reports whether k is a module kind.
func (k Kind) Valid() bool {
	return k == KindPlatform || k == KindAccelerator || k == KindSoftware
}

// Ports a module may require of the installation, named by provider interface rather than by
// adapter (core-and-extensions.md §3.1).
const (
	PortOSProvisioning   = "os-provisioning"
	PortHostAutomation   = "host-automation"
	PortWorkflowExecutor = "workflow-executor"
	PortMetrics          = "metrics"
	PortBMC              = "bmc"
	PortHypervisor       = "hypervisor"
)

var knownPorts = map[string]bool{
	PortOSProvisioning: true, PortHostAutomation: true, PortWorkflowExecutor: true,
	PortMetrics: true, PortBMC: true, PortHypervisor: true,
}

// Hook names one piece of a module's logic that data cannot express, and why.
type Hook struct {
	Name   string `json:"name"`
	Reason string `json:"reason"`
}

// Manifest is a module's declaration of itself (modules/<kind>/<id>/module.json). Every list is
// what the module owns: no two modules may claim the same Workflow kind, completion type, route,
// or playbook.
type Manifest struct {
	ContractVersion int    `json:"contractVersion"`
	Kind            Kind   `json:"kind"`
	ID              string `json:"id"`
	Version         string `json:"version"`
	// Playbooks are the names in the release's playbook allowlist that the module runs.
	Playbooks []string `json:"playbooks"`
	// WorkflowKinds are the Workflow kinds the module plans.
	WorkflowKinds []string `json:"workflowKinds"`
	// TaskKinds are the Task kinds only this module declares; generic kinds belong to the core.
	TaskKinds []string `json:"taskKinds"`
	// Completions are the completion types the module declares on its Tasks.
	Completions []string `json:"completions"`
	// Spec is the schema of the resource spec the module defines, when it defines one: the JSON
	// Schema subset SpecSchema reads.
	Spec json.RawMessage `json:"spec,omitempty"`
	// Hooks are the module's Go hooks, each with the reason data cannot express it.
	Hooks []Hook `json:"hooks"`
	// Capabilities are the provider interfaces the module needs (the Port constants).
	Capabilities []string `json:"capabilities"`
	// Routes are the path prefixes under the API the module serves.
	Routes []string `json:"routes"`
	// Artifacts are what the module's host automation fetches, from the Site's Local Repository or
	// from the upstreams they name. Unlike the lists above they are not owned: modules may declare
	// the same source or file when the declarations agree.
	Artifacts Artifacts `json:"artifacts,omitempty"`
}

// Ref is "<kind>/<id>", the module's directory under modules/.
func (m Manifest) Ref() string {
	return string(m.Kind) + "/" + m.ID
}

// Definition is the definition a Workflow the module plans records: "<kind>/<id>@<version>".
func (m Manifest) Definition() string {
	return m.Ref() + "@" + m.Version
}

var (
	// ErrInvalidManifest means a module's manifest is malformed or breaks the contract.
	ErrInvalidManifest = errors.New("invalid module manifest")
	// ErrUnsupportedContract means a module declares a contract version this core does not load.
	ErrUnsupportedContract = errors.New("unsupported module contract version")
	// ErrModuleConflict means two modules claim the same identity or owned name.
	ErrModuleConflict = errors.New("module conflict")
)

var (
	moduleIDPattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)
	semverPattern   = regexp.MustCompile(`^\d+\.\d+\.\d+(-[0-9A-Za-z.-]+)?$`)
)

// ParseManifest decodes a module.json, refusing unknown fields so a misspelled key is an error
// rather than a silently ignored declaration, writes its artifact signing key fingerprints in upper
// case, and validates it.
func ParseManifest(data []byte) (Manifest, error) {
	var manifest Manifest
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return Manifest{}, fmt.Errorf("%w: %v", ErrInvalidManifest, err)
	}
	manifest.Artifacts.normalize()
	if err := manifest.Validate(); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

// Validate checks one manifest on its own: a supported contract version, a known kind, a
// kebab-case id, a SemVer version, unique names in every list, a reason for every hook, known
// ports, routes under /api/v1/, a well-formed spec schema when present, and artifacts that pass
// Artifacts.Validate (the error then wraps both ErrInvalidManifest and ErrInvalidArtifact).
func (m Manifest) Validate() error {
	if !supported(m.ContractVersion) {
		return fmt.Errorf("%w: module %s declares contract version %d, this core loads %v",
			ErrUnsupportedContract, m.Ref(), m.ContractVersion, SupportedContractVersions)
	}
	if !m.Kind.Valid() {
		return fmt.Errorf("%w: kind %q is not platform, accelerator, or software", ErrInvalidManifest, m.Kind)
	}
	if !moduleIDPattern.MatchString(m.ID) {
		return fmt.Errorf("%w: id %q must be kebab-case", ErrInvalidManifest, m.ID)
	}
	if !semverPattern.MatchString(m.Version) {
		return fmt.Errorf("%w: module %s version %q is not SemVer", ErrInvalidManifest, m.Ref(), m.Version)
	}
	for field, names := range map[string][]string{
		"playbooks": m.Playbooks, "workflowKinds": m.WorkflowKinds, "taskKinds": m.TaskKinds,
		"completions": m.Completions, "capabilities": m.Capabilities, "routes": m.Routes,
	} {
		if duplicate := firstDuplicate(names); duplicate != "" {
			return fmt.Errorf("%w: module %s lists %s %q twice", ErrInvalidManifest, m.Ref(), field, duplicate)
		}
	}
	hookNames := make([]string, 0, len(m.Hooks))
	for _, hook := range m.Hooks {
		if strings.TrimSpace(hook.Name) == "" || strings.TrimSpace(hook.Reason) == "" {
			return fmt.Errorf("%w: module %s has a hook without a name or a reason", ErrInvalidManifest, m.Ref())
		}
		hookNames = append(hookNames, hook.Name)
	}
	if duplicate := firstDuplicate(hookNames); duplicate != "" {
		return fmt.Errorf("%w: module %s lists hook %q twice", ErrInvalidManifest, m.Ref(), duplicate)
	}
	for _, port := range m.Capabilities {
		if !knownPorts[port] {
			return fmt.Errorf("%w: module %s requires unknown provider interface %q", ErrInvalidManifest, m.Ref(), port)
		}
	}
	for _, route := range m.Routes {
		if !strings.HasPrefix(route, "/api/v1/") {
			return fmt.Errorf("%w: module %s route %q is not under /api/v1/", ErrInvalidManifest, m.Ref(), route)
		}
	}
	if _, err := m.SpecSchema(); err != nil {
		return err
	}
	if err := m.Artifacts.Validate(); err != nil {
		return fmt.Errorf("%w: module %s artifacts: %w", ErrInvalidManifest, m.Ref(), err)
	}
	return nil
}

// SpecSchema returns the module's spec schema, or nil when the module defines no spec.
func (m Manifest) SpecSchema() (*SpecSchema, error) {
	if len(m.Spec) == 0 {
		return nil, nil
	}
	schema, err := ParseSpecSchema(m.Spec)
	if err != nil {
		return nil, fmt.Errorf("%w: module %s spec schema: %v", ErrInvalidManifest, m.Ref(), err)
	}
	return schema, nil
}

// Module is a module compiled into the build: its manifest and the implementations of the hooks
// the manifest lists, by hook name. A consumer asserts a hook to the core port it calls.
type Module struct {
	Manifest Manifest
	Hooks    map[string]any
}

// Load returns the module whose embedded module.json is manifest, with its hook implementations.
// A module's root Go package calls it with the manifest it embeds.
func Load(manifest []byte, hooks map[string]any) (Module, error) {
	parsed, err := ParseManifest(manifest)
	if err != nil {
		return Module{}, err
	}
	return Module{Manifest: parsed, Hooks: hooks}, nil
}

// Registry is every module of a build, validated together. api, worker, and ansible-executor build
// it from the same list, so they agree on which Workflow kinds and completion types exist.
type Registry struct {
	modules        []Module
	byRef          map[string]Module
	byWorkflowKind map[string]Module
	byCompletion   map[string]Module
}

// NewRegistry validates the modules on their own and against each other: each manifest is valid,
// each module's hook implementations are exactly its declared hooks, no two modules share a
// reference, Workflow kind, completion type, route, or playbook, and modules that declare the same
// artifact source or file agree on it (artifactClaims.claim).
func NewRegistry(modules ...Module) (*Registry, error) {
	registry := &Registry{
		byRef: map[string]Module{}, byWorkflowKind: map[string]Module{}, byCompletion: map[string]Module{},
	}
	routes, playbooks := map[string]string{}, map[string]string{}
	artifacts := newArtifactClaims()
	for _, module := range modules {
		manifest := module.Manifest
		if err := manifest.Validate(); err != nil {
			return nil, err
		}
		if err := hooksMatch(module); err != nil {
			return nil, err
		}
		ref := manifest.Ref()
		if _, exists := registry.byRef[ref]; exists {
			return nil, fmt.Errorf("%w: module %s is registered twice", ErrModuleConflict, ref)
		}
		registry.byRef[ref] = module
		for _, kind := range manifest.WorkflowKinds {
			if owner, exists := registry.byWorkflowKind[kind]; exists {
				return nil, fmt.Errorf("%w: Workflow kind %q is claimed by %s and %s", ErrModuleConflict, kind, owner.Manifest.Ref(), ref)
			}
			registry.byWorkflowKind[kind] = module
		}
		for _, completion := range manifest.Completions {
			if owner, exists := registry.byCompletion[completion]; exists {
				return nil, fmt.Errorf("%w: completion type %q is claimed by %s and %s", ErrModuleConflict, completion, owner.Manifest.Ref(), ref)
			}
			registry.byCompletion[completion] = module
		}
		if err := claim(routes, manifest.Routes, ref, "route"); err != nil {
			return nil, err
		}
		if err := claim(playbooks, manifest.Playbooks, ref, "playbook"); err != nil {
			return nil, err
		}
		if err := artifacts.claim(ref, manifest.Artifacts); err != nil {
			return nil, err
		}
		registry.modules = append(registry.modules, module)
	}
	sort.Slice(registry.modules, func(i, j int) bool {
		return registry.modules[i].Manifest.Ref() < registry.modules[j].Manifest.Ref()
	})
	return registry, nil
}

// Modules returns every registered module, ordered by reference.
func (r *Registry) Modules() []Module {
	return append([]Module(nil), r.modules...)
}

// Module returns the module "<kind>/<id>".
func (r *Registry) Module(kind Kind, id string) (Module, bool) {
	module, ok := r.byRef[string(kind)+"/"+id]
	return module, ok
}

// ForWorkflowKind returns the module that plans Workflows of kind.
func (r *Registry) ForWorkflowKind(kind string) (Module, bool) {
	module, ok := r.byWorkflowKind[kind]
	return module, ok
}

// ForCompletion returns the module that declares completion type.
func (r *Registry) ForCompletion(completion string) (Module, bool) {
	module, ok := r.byCompletion[completion]
	return module, ok
}

func supported(version int) bool {
	for _, candidate := range SupportedContractVersions {
		if candidate == version {
			return true
		}
	}
	return false
}

func hooksMatch(module Module) error {
	declared := map[string]bool{}
	for _, hook := range module.Manifest.Hooks {
		declared[hook.Name] = true
		if _, ok := module.Hooks[hook.Name]; !ok {
			return fmt.Errorf("%w: module %s declares hook %q without implementing it", ErrInvalidManifest, module.Manifest.Ref(), hook.Name)
		}
	}
	for name := range module.Hooks {
		if !declared[name] {
			return fmt.Errorf("%w: module %s implements hook %q its manifest does not declare", ErrInvalidManifest, module.Manifest.Ref(), name)
		}
	}
	return nil
}

func claim(owners map[string]string, names []string, ref, what string) error {
	for _, name := range names {
		if owner, exists := owners[name]; exists {
			return fmt.Errorf("%w: %s %q is claimed by %s and %s", ErrModuleConflict, what, name, owner, ref)
		}
		owners[name] = ref
	}
	return nil
}

func firstDuplicate(names []string) string {
	seen := map[string]bool{}
	for _, name := range names {
		if seen[name] {
			return name
		}
		seen[name] = true
	}
	return ""
}
