package domain

import serverdomain "github.com/maple52046/swallow/internal/core/server/domain"

// HookAccelerator is the hook name an Accelerator module lists in its module.json for its
// Accelerator.
const HookAccelerator = "accelerator"

// Accelerator is the hook an Accelerator module implements: what one GPU vendor's accelerators
// mean to swallow (core-and-extensions §4.2). The core and the other modules ask it instead of
// recognizing a vendor themselves (§5.4).
type Accelerator interface {
	// ID is the Accelerator module's id.
	ID() string
	// Label is the vendor's operator-facing name.
	Label() string
	// IsCompute reports whether a GPU group of a Server's inventory is this vendor's workload
	// capacity; the vendor's display controllers are not.
	IsCompute(gpu serverdomain.GPU) bool
}

// Accelerators are the Accelerator modules of a build, by id.
type Accelerators map[string]Accelerator

// HasComputeGPU reports whether gpus include workload capacity of the Accelerator id. An id the
// build does not include has none.
func (a Accelerators) HasComputeGPU(id string, gpus []serverdomain.GPU) bool {
	accelerator, ok := a[id]
	return ok && HasComputeGPU(accelerator, gpus)
}

// Label is the Accelerator's operator-facing name, or the id when the build does not include it.
func (a Accelerators) Label(id string) string {
	if accelerator, ok := a[id]; ok {
		return accelerator.Label()
	}
	return id
}

// HasComputeGPU reports whether any group of gpus is the accelerator's workload capacity. A nil
// accelerator (a build without one) finds none.
func HasComputeGPU(accelerator Accelerator, gpus []serverdomain.GPU) bool {
	if accelerator == nil {
		return false
	}
	for _, gpu := range gpus {
		if accelerator.IsCompute(gpu) {
			return true
		}
	}
	return false
}
