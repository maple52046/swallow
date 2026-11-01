// Package slurm is the Slurm Platform module: the deploy, uninstall, and Omnistat playbooks under
// this directory and, as it is extracted from the core, its Go hooks.
package slurm

import (
	_ "embed"

	moduledomain "github.com/maple52046/swallow/internal/core/module/domain"
)

//go:embed module.json
var manifest []byte

// Module returns the Slurm Platform module declared by module.json.
func Module() (moduledomain.Module, error) {
	return moduledomain.Load(manifest, nil)
}
