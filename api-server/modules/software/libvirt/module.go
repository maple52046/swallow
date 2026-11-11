// Package libvirt is the libvirt (QEMU/KVM) Software module: its install and uninstall playbooks
// under this directory and its Software hook. The kind prepares a Server to be a Hypervisor
// (decision 055): KVM available, the system libvirt daemon running, and the Server's default user
// allowed to use it. It installs only the OS packages and never touches the host's networks, kernel
// command line, or existing virtual machines; creating virtual machines is not part of it.
package libvirt

import (
	_ "embed"
	"fmt"

	moduledomain "github.com/maple52046/swallow/internal/core/module/domain"
	softwaredomain "github.com/maple52046/swallow/internal/core/software/domain"
)

//go:embed module.json
var manifest []byte

// Kind is the Managed Software kind this module provides and the module's id. Its Workflow kinds and
// playbooks are derived from it and Software Assignments are keyed by it, so renaming it is a
// breaking change.
const Kind softwaredomain.Kind = "libvirt"

// Module returns the module module.json declares, with its software hook. The composition root lists
// it in compiledModules; a manifest that breaks module contract v1, or a hook set that differs from
// the one it declares, is an error that stops the process at startup.
func Module() (moduledomain.Module, error) {
	return moduledomain.Load(manifest, map[string]any{softwaredomain.HookSoftware: Software{}})
}

// Software is the libvirt Software hook. It does no I/O: what the host can do (KVM, the OS) is
// checked by the playbook on the host before anything is installed.
type Software struct{}

// Entry is libvirt's catalog entry: no spec and no install restriction of its own. libvirt coexists
// with container runtimes and Platforms, and a locked Server is refused by the check every install
// shares.
func (Software) Entry() softwaredomain.CatalogEntry {
	return softwaredomain.CatalogEntry{
		Kind:       Kind,
		Label:      "libvirt (QEMU/KVM)",
		SpecFields: []string{},
	}
}

// ValidateSpec refuses every field: the OS packages decide the QEMU and libvirt versions, so there
// is nothing to choose. Errors wrap softwaredomain.ErrSpecInvalid.
func (s Software) ValidateSpec(_ map[string][]softwaredomain.Role, spec map[string]any) error {
	if len(spec) > 0 {
		return fmt.Errorf("%w: %s takes no spec fields", softwaredomain.ErrSpecInvalid, s.Entry().Kind)
	}
	return nil
}

// NormalizeSpec records an empty spec, so every assignment of the kind stores {} rather than a
// missing spec, whatever the request sent (ValidateSpec has already refused any field).
func (Software) NormalizeSpec(map[string]any) map[string]any {
	return map[string]any{}
}

// InstallVars passes no kind-specific trusted variables: the playbook reads the login account from
// the inventory's ansible_user, like every Software playbook.
func (Software) InstallVars(map[string][]softwaredomain.Role, map[string]any) map[string]any {
	return map[string]any{}
}
