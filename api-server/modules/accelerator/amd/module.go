// Package amd is the AMD Accelerator module: the GPU Operator playbook under this directory and, as
// it is extracted from the core, the GPU-vendor hooks Platform modules ask for.
package amd

import (
	_ "embed"

	moduledomain "github.com/maple52046/swallow/internal/core/module/domain"
)

//go:embed module.json
var manifest []byte

// Module returns the AMD Accelerator module declared by module.json.
func Module() (moduledomain.Module, error) {
	return moduledomain.Load(manifest, nil)
}
