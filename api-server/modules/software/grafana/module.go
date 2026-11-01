// Package grafana is the Grafana Software module: its install and uninstall playbooks under this
// directory and its Software hook.
package grafana

import (
	_ "embed"
	"strings"

	moduledomain "github.com/maple52046/swallow/internal/core/module/domain"
	softwaredomain "github.com/maple52046/swallow/internal/core/software/domain"
)

//go:embed module.json
var manifest []byte

// Module returns the Grafana Software module declared by module.json.
func Module() (moduledomain.Module, error) {
	return moduledomain.Load(manifest, map[string]any{softwaredomain.HookSoftware: Software{}})
}

// Software is Grafana's Software hook.
type Software struct{}

// Entry is Grafana's catalog entry: a pinned container under Docker CE or Podman, or a native
// install (decision 063).
func (Software) Entry() softwaredomain.CatalogEntry {
	return softwaredomain.CatalogEntry{
		Kind:              softwaredomain.KindGrafana,
		Label:             "Grafana",
		InstallMethods:    []softwaredomain.InstallMethod{softwaredomain.InstallMethodContainer, softwaredomain.InstallMethodNative},
		ContainerRuntimes: []softwaredomain.Kind{softwaredomain.KindDockerCE, softwaredomain.KindPodman},
		SpecFields: []string{softwaredomain.SpecInstallMethod, softwaredomain.SpecContainerRuntime, softwaredomain.SpecVersion,
			softwaredomain.SpecPrometheusURL, softwaredomain.SpecDashboards},
	}
}

// ValidateSpec checks the install method, release version, datasource URL, and bundles.
func (s Software) ValidateSpec(_ map[string][]softwaredomain.Role, spec map[string]any) error {
	if err := softwaredomain.ValidateMethodAndVersion(s.Entry(), spec); err != nil {
		return err
	}
	if err := softwaredomain.ValidateOptionalString(spec, softwaredomain.SpecPrometheusURL); err != nil {
		return err
	}
	if raw := strings.TrimSpace(softwaredomain.SpecString(spec, softwaredomain.SpecPrometheusURL)); raw != "" {
		if err := softwaredomain.ValidatePrometheusURL(raw); err != nil {
			return err
		}
	}
	dashboards, err := softwaredomain.SpecStringList(spec, softwaredomain.SpecDashboards)
	if err != nil {
		return err
	}
	for _, dashboard := range dashboards {
		if err := softwaredomain.ValidateDashboardBundle(dashboard); err != nil {
			return err
		}
	}
	return nil
}

// NormalizeSpec records the pinned image tag, the trimmed datasource URL, and the bundles as a plain
// string array.
func (s Software) NormalizeSpec(spec map[string]any) map[string]any {
	out := softwaredomain.CloneSpec(spec)
	if out == nil {
		out = map[string]any{}
	}
	softwaredomain.NormalizeMethodAndVersion(s.Entry(), out, softwaredomain.DefaultGrafanaVersion)
	out[softwaredomain.SpecPrometheusURL] = strings.TrimSpace(softwaredomain.SpecString(out, softwaredomain.SpecPrometheusURL))
	dashboards, _ := softwaredomain.SpecStringList(out, softwaredomain.SpecDashboards)
	out[softwaredomain.SpecDashboards] = softwaredomain.NonNilStrings(dashboards)
	return out
}

// InstallVars passes the normalized spec. An empty datasource URL tells the playbook to read the
// Prometheus on the same Server, on the domain's port.
func (Software) InstallVars(_ map[string][]softwaredomain.Role, spec map[string]any) map[string]any {
	dashboards, _ := softwaredomain.SpecStringList(spec, softwaredomain.SpecDashboards)
	return map[string]any{
		"swallow_grafana_install_method":    softwaredomain.SpecString(spec, softwaredomain.SpecInstallMethod),
		"swallow_grafana_container_runtime": softwaredomain.SpecString(spec, softwaredomain.SpecContainerRuntime),
		"swallow_grafana_version":           softwaredomain.SpecString(spec, softwaredomain.SpecVersion),
		"swallow_grafana_port":              softwaredomain.GrafanaPort,
		"swallow_grafana_prometheus_url":    strings.TrimSpace(softwaredomain.SpecString(spec, softwaredomain.SpecPrometheusURL)),
		"swallow_grafana_prometheus_port":   softwaredomain.PrometheusPort,
		"swallow_grafana_dashboards":        softwaredomain.NonNilStrings(dashboards),
	}
}
