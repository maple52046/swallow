// Package nodeexporter is the Prometheus Node Exporter Software module: its install and uninstall
// playbooks under this directory and its Software hook. The exporter is the OS distribution's
// package, so the module pins no version.
package nodeexporter

import (
	_ "embed"
	"fmt"

	moduledomain "github.com/maple52046/swallow/internal/core/module/domain"
	softwaredomain "github.com/maple52046/swallow/internal/core/software/domain"
)

//go:embed module.json
var manifest []byte

// Kind is the Managed Software kind this module provides. The OS deployment launchers compose it
// after every provisioned Server's time synchronization, so they name it.
const Kind softwaredomain.Kind = "node-exporter"

// Port is the host port the exporter listens on. swallow's Prometheus discovery scrapes it on every
// Server that has the exporter, so it must not change.
const Port = 9100

// Module returns the Node Exporter Software module declared by module.json.
func Module() (moduledomain.Module, error) {
	return moduledomain.Load(manifest, map[string]any{softwaredomain.HookSoftware: Software{}})
}

// Software is the Node Exporter's Software hook.
type Software struct{}

// Entry is the exporter's catalog entry: no spec and no install restriction of its own. OS
// provisioning composes it on every Server it deploys, so a host never runs a second node exporter.
func (Software) Entry() softwaredomain.CatalogEntry {
	return softwaredomain.CatalogEntry{
		Kind:       Kind,
		Label:      "Node Exporter",
		Exporter:   &softwaredomain.Exporter{Name: string(Kind), Port: Port},
		SpecFields: []string{},
	}
}

// ValidateSpec refuses every field: the OS package decides the version.
func (s Software) ValidateSpec(_ map[string][]softwaredomain.Role, spec map[string]any) error {
	if len(spec) > 0 {
		return fmt.Errorf("%w: %s takes no spec fields", softwaredomain.ErrSpecInvalid, s.Entry().Kind)
	}
	return nil
}

// NormalizeSpec records an empty spec.
func (Software) NormalizeSpec(map[string]any) map[string]any {
	return map[string]any{}
}

// InstallVars passes the port the playbook checks the exporter answers on.
func (Software) InstallVars(map[string][]softwaredomain.Role, map[string]any) map[string]any {
	return map[string]any{"swallow_node_exporter_port": Port}
}
