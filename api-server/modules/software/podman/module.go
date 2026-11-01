// Package podman is the Podman Software module: its install and uninstall playbooks under this
// directory and, as it is extracted from the core, its Go hooks.
package podman

import (
	_ "embed"

	moduledomain "github.com/maple52046/swallow/internal/core/module/domain"
)

//go:embed module.json
var manifest []byte

// Module returns the Podman Software module declared by module.json.
func Module() (moduledomain.Module, error) {
	return moduledomain.Load(manifest, nil)
}
