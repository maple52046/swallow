// Package devicemetricsexporter is the AMD Device Metrics Exporter Software module: its install and
// uninstall playbooks under this directory and its Software hook.
package devicemetricsexporter

import (
	_ "embed"
	"fmt"
	"strings"

	moduledomain "github.com/maple52046/swallow/internal/core/module/domain"
	softwaredomain "github.com/maple52046/swallow/internal/core/software/domain"
)

//go:embed module.json
var manifest []byte

// Module returns the Device Metrics Exporter Software module declared by module.json.
func Module() (moduledomain.Module, error) {
	return moduledomain.Load(manifest, map[string]any{softwaredomain.HookSoftware: Software{}})
}

// Software is the Device Metrics Exporter's Software hook.
type Software struct{}

// Entry is the exporter's catalog entry (decision 068): one native install path and no user
// spec, only on Servers with AMD compute GPUs, and left to the Platform on Servers a Platform
// claims or that are Kubernetes members.
func (Software) Entry() softwaredomain.CatalogEntry {
	return softwaredomain.CatalogEntry{
		Kind:                        softwaredomain.KindDeviceMetricsExporter,
		Label:                       "AMD Device Metrics Exporter",
		RefusedForKubernetesMembers: true,
		RefusedForClaimedServers:    true,
		// The AMD Accelerator module's id; a Software module names it rather than importing it.
		RequiredAccelerator: "amd",
		Exporter: &softwaredomain.Exporter{
			Name: string(softwaredomain.KindDeviceMetricsExporter),
			Port: softwaredomain.DeviceMetricsExporterPort,
		},
		SpecFields: []string{},
	}
}

// ValidateSpec refuses every field: the version is pinned and recorded by NormalizeSpec, so there
// is nothing to choose yet.
func (s Software) ValidateSpec(_ map[string][]softwaredomain.Role, spec map[string]any) error {
	if len(spec) > 0 {
		return fmt.Errorf("%w: %s takes no spec fields", softwaredomain.ErrSpecInvalid, s.Entry().Kind)
	}
	return nil
}

// NormalizeSpec records the pinned version the install applies.
func (Software) NormalizeSpec(map[string]any) map[string]any {
	return map[string]any{softwaredomain.SpecVersion: softwaredomain.DeviceMetricsExporterVersion}
}

// InstallVars passes the recorded version, the port, and no job attribution. A composing Slurm
// deployment sends its own attribution and topology on top of these.
func (Software) InstallVars(_ map[string][]softwaredomain.Role, spec map[string]any) map[string]any {
	// A record written by this release always carries the version; fall back for safety.
	version := strings.TrimSpace(softwaredomain.SpecString(spec, softwaredomain.SpecVersion))
	if version == "" {
		version = softwaredomain.DeviceMetricsExporterVersion
	}
	return map[string]any{
		"swallow_device_metrics_exporter_version":         version,
		"swallow_device_metrics_exporter_port":            softwaredomain.DeviceMetricsExporterPort,
		"swallow_device_metrics_exporter_job_attribution": string(softwaredomain.JobAttributionNone),
	}
}
