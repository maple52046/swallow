// Package devicemetricsexporter is the AMD Device Metrics Exporter Software module: its install and
// uninstall playbooks under this directory and, as it is extracted from the core, its Go hooks.
package devicemetricsexporter

import (
	_ "embed"

	moduledomain "github.com/maple52046/swallow/internal/core/module/domain"
)

//go:embed module.json
var manifest []byte

// Module returns the Device Metrics Exporter Software module declared by module.json.
func Module() (moduledomain.Module, error) {
	return moduledomain.Load(manifest, nil)
}
