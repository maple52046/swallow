// Package podman is the Podman Software module: its install and uninstall playbooks under this
// directory and its Software hook.
package podman

import (
	_ "embed"

	moduledomain "github.com/maple52046/swallow/internal/core/module/domain"
	softwaredomain "github.com/maple52046/swallow/internal/core/software/domain"
)

//go:embed module.json
var manifest []byte

// Module returns the Podman Software module declared by module.json.
func Module() (moduledomain.Module, error) {
	return moduledomain.Load(manifest, map[string]any{softwaredomain.HookSoftware: Software{}})
}

// Software is Podman's Software hook.
type Software struct{}

// Entry is Podman's catalog entry: role-less, exclusive with Docker CE, and refused on Kubernetes
// members, whose containerd would collide with k0s.
func (Software) Entry() softwaredomain.CatalogEntry {
	return softwaredomain.CatalogEntry{
		Kind:                        softwaredomain.KindPodman,
		Label:                       "Podman",
		MutuallyExclusiveWith:       []softwaredomain.Kind{softwaredomain.KindDockerCE},
		RefusedForKubernetesMembers: true,
		SpecFields:                  []string{softwaredomain.SpecVersion},
	}
}

// ValidateSpec requires a string version when one is given.
func (Software) ValidateSpec(_ map[string][]softwaredomain.Role, spec map[string]any) error {
	return softwaredomain.ValidateOptionalString(spec, softwaredomain.SpecVersion)
}

// NormalizeSpec applies no defaults.
func (Software) NormalizeSpec(spec map[string]any) map[string]any {
	return softwaredomain.CloneSpec(spec)
}

// InstallVars passes the pinned package version, when there is one.
func (Software) InstallVars(_ map[string][]softwaredomain.Role, spec map[string]any) map[string]any {
	vars := map[string]any{}
	softwaredomain.SetIfPresent(vars, "swallow_podman_version", spec, softwaredomain.SpecVersion)
	return vars
}
