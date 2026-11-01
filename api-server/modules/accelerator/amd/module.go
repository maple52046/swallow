// Package amd is the AMD Accelerator module: the GPU Operator playbook under this directory and the
// accelerator hook that tells the core which GPUs are AMD workload capacity.
package amd

import (
	_ "embed"
	"strings"

	moduledomain "github.com/maple52046/swallow/internal/core/module/domain"
	platformdomain "github.com/maple52046/swallow/internal/core/platform/domain"
	serverdomain "github.com/maple52046/swallow/internal/core/server/domain"
)

//go:embed module.json
var manifest []byte

// Module returns the AMD Accelerator module declared by module.json.
func Module() (moduledomain.Module, error) {
	return moduledomain.Load(manifest, map[string]any{platformdomain.HookAccelerator: Accelerator{}})
}

// ID is the module id, which a Managed Software kind names as its required accelerator.
const ID = "amd"

// Accelerator is the AMD module's accelerator hook.
type Accelerator struct{}

// ID is "amd".
func (Accelerator) ID() string { return ID }

// Label is "AMD".
func (Accelerator) Label() string { return "AMD" }

// IsCompute reports whether the group is AMD workload capacity: a compute GPU whose provider vendor
// names AMD, or whose model is the "1002:product" PCI id a provider falls back to when it cannot
// name the device. AMD display controllers are not workload capacity and never match.
func (Accelerator) IsCompute(gpu serverdomain.GPU) bool {
	if gpu.ClassifiedKind() != serverdomain.GPUKindCompute {
		return false
	}
	vendor := strings.ToLower(gpu.Vendor)
	return strings.Contains(vendor, "advanced micro devices") ||
		strings.Contains(vendor, "amd") ||
		strings.HasPrefix(strings.ToLower(strings.TrimSpace(gpu.Model)), "1002:")
}
