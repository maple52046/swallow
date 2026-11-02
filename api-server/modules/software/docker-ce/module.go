// Package dockerce is the Docker CE Software module: its install and uninstall playbooks under this
// directory, its Software hook, the Docker Host Explorer in explorer and explorerhttp, and the
// Docker Engine client in dockerengine.
package dockerce

import (
	_ "embed"
	"fmt"

	moduledomain "github.com/maple52046/swallow/internal/core/module/domain"
	softwaredomain "github.com/maple52046/swallow/internal/core/software/domain"
)

//go:embed module.json
var manifest []byte

// Module returns the Docker CE Software module declared by module.json.
func Module() (moduledomain.Module, error) {
	return moduledomain.Load(manifest, map[string]any{softwaredomain.HookSoftware: Software{}})
}

// SpecEnableAPI is the Docker CE variant that makes the Docker Engine API listen on EngineAPIPort.
// Install normalizes an omitted value to true, so a recorded Docker CE assignment always carries an
// explicit boolean (decision 043). It is published language of the software contract.
const SpecEnableAPI = "enableApi"

// EngineAPIPort is the TCP port the playbook opens when SpecEnableAPI is true and the port the
// Docker Host Explorer connects to. It is owned here and passed to the playbook as a trusted
// variable so the listener and the explorer cannot disagree.
const EngineAPIPort = 2375

// APIEnabled reports whether a Docker CE assignment spec records an enabled Engine API.
//
// Only an explicit boolean true counts. A spec recorded before the enableApi variant existed has no
// key and reads as disabled, because no listener was ever configured on that host; treating the
// absence as the install default (true) would point the explorer at a closed port.
func APIEnabled(spec map[string]any) bool {
	enabled, ok := spec[SpecEnableAPI].(bool)
	return ok && enabled
}

// Software is Docker CE's Software hook.
type Software struct{}

// Entry is Docker CE's catalog entry: role-less, exclusive with Podman, and refused on Kubernetes
// members, whose containerd would collide with k0s.
func (Software) Entry() softwaredomain.CatalogEntry {
	return softwaredomain.CatalogEntry{
		Kind:                        softwaredomain.KindDockerCE,
		Label:                       "Docker CE",
		MutuallyExclusiveWith:       []softwaredomain.Kind{softwaredomain.KindPodman},
		RefusedForKubernetesMembers: true,
		SpecFields:                  []string{softwaredomain.SpecVersion, SpecEnableAPI},
	}
}

// ValidateSpec requires a string version and a real boolean enableApi: accepting a string such as
// "false" and reading it as enabled would open the unauthenticated listener the user declined.
func (Software) ValidateSpec(_ map[string][]softwaredomain.Role, spec map[string]any) error {
	if err := softwaredomain.ValidateOptionalString(spec, softwaredomain.SpecVersion); err != nil {
		return err
	}
	if value, ok := spec[SpecEnableAPI]; ok && value != nil {
		if _, isBool := value.(bool); !isBool {
			return fmt.Errorf("%w: spec.%s must be a boolean", softwaredomain.ErrSpecInvalid, SpecEnableAPI)
		}
	}
	return nil
}

// NormalizeSpec defaults enableApi to true (decision 043), recorded explicitly so a later read can
// tell "enabled by default" from a legacy record that predates the variant.
func (Software) NormalizeSpec(spec map[string]any) map[string]any {
	out := softwaredomain.CloneSpec(spec)
	if out == nil {
		out = map[string]any{}
	}
	if value, ok := out[SpecEnableAPI]; !ok || value == nil {
		out[SpecEnableAPI] = true
	}
	return out
}

// InstallVars always sends the listener switch so the playbook converges it both ways: true adds
// it, false (and a legacy spec without the key) removes swallow's drop-in.
func (Software) InstallVars(_ map[string][]softwaredomain.Role, spec map[string]any) map[string]any {
	vars := map[string]any{
		"swallow_docker_enable_api": APIEnabled(spec),
		"swallow_docker_api_port":   EngineAPIPort,
	}
	softwaredomain.SetIfPresent(vars, "swallow_docker_version", spec, softwaredomain.SpecVersion)
	return vars
}
