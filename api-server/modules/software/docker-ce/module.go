// Package dockerce is the Docker CE Software module: its install and uninstall playbooks under this
// directory, the Docker Engine client in dockerengine, and, as it is extracted from the core, its Go
// hooks.
package dockerce

import (
	_ "embed"

	moduledomain "github.com/maple52046/swallow/internal/core/module/domain"
)

//go:embed module.json
var manifest []byte

// Module returns the Docker CE Software module declared by module.json.
func Module() (moduledomain.Module, error) {
	return moduledomain.Load(manifest, nil)
}
