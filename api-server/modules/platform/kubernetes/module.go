// Package kubernetes is the Kubernetes Platform module (k0s): the deploy, uninstall, and exporter
// playbooks under this directory and, as it is extracted from the core, its Go hooks.
package kubernetes

import (
	_ "embed"

	moduledomain "github.com/maple52046/swallow/internal/core/module/domain"
)

//go:embed module.json
var manifest []byte

// Module returns the Kubernetes Platform module declared by module.json.
func Module() (moduledomain.Module, error) {
	return moduledomain.Load(manifest, nil)
}
