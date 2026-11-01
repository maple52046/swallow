// Package prometheus is the Prometheus Software module: its install and uninstall playbooks under
// this directory and its Software hook.
package prometheus

import (
	_ "embed"
	"fmt"
	"strings"

	moduledomain "github.com/maple52046/swallow/internal/core/module/domain"
	softwaredomain "github.com/maple52046/swallow/internal/core/software/domain"
)

//go:embed module.json
var manifest []byte

// Module returns the Prometheus Software module declared by module.json.
func Module() (moduledomain.Module, error) {
	return moduledomain.Load(manifest, map[string]any{softwaredomain.HookSoftware: Software{}})
}

// Software is Prometheus's Software hook.
type Software struct{}

// Entry is Prometheus's catalog entry: a pinned container under Docker CE or Podman, or a native
// install (decision 063).
func (Software) Entry() softwaredomain.CatalogEntry {
	return softwaredomain.CatalogEntry{
		Kind:              softwaredomain.KindPrometheus,
		Label:             "Prometheus",
		InstallMethods:    []softwaredomain.InstallMethod{softwaredomain.InstallMethodContainer, softwaredomain.InstallMethodNative},
		ContainerRuntimes: []softwaredomain.Kind{softwaredomain.KindDockerCE, softwaredomain.KindPodman},
		SpecFields: []string{softwaredomain.SpecInstallMethod, softwaredomain.SpecContainerRuntime, softwaredomain.SpecVersion,
			softwaredomain.SpecListen, softwaredomain.SpecRetention, softwaredomain.SpecScrapeJob,
			softwaredomain.SpecScrapeTargets, softwaredomain.SpecScrapeServerIDs, softwaredomain.SpecScrapePort},
	}
}

// ValidateSpec checks the install method, release version, listen address, retention, job name,
// and targets.
func (s Software) ValidateSpec(_ map[string][]softwaredomain.Role, spec map[string]any) error {
	if err := softwaredomain.ValidateMethodAndVersion(s.Entry(), spec); err != nil {
		return err
	}
	for _, field := range []struct {
		key      string
		validate func(string) error
	}{
		{softwaredomain.SpecListen, softwaredomain.ValidatePrometheusListen},
		{softwaredomain.SpecRetention, softwaredomain.ValidatePrometheusRetention},
		{softwaredomain.SpecScrapeJob, softwaredomain.ValidateScrapeJob},
	} {
		if err := softwaredomain.ValidateOptionalString(spec, field.key); err != nil {
			return err
		}
		if value := strings.TrimSpace(softwaredomain.SpecString(spec, field.key)); value != "" {
			if err := field.validate(value); err != nil {
				return err
			}
		}
	}
	targets, err := softwaredomain.SpecStringList(spec, softwaredomain.SpecScrapeTargets)
	if err != nil {
		return err
	}
	serverIDs, err := softwaredomain.SpecStringList(spec, softwaredomain.SpecScrapeServerIDs)
	if err != nil {
		return err
	}
	if total := len(targets) + len(serverIDs); total > softwaredomain.MaxPrometheusScrapeTargets {
		return fmt.Errorf("%w: spec.%s and spec.%s have %d entries, at most %d", softwaredomain.ErrSpecInvalid,
			softwaredomain.SpecScrapeTargets, softwaredomain.SpecScrapeServerIDs, total, softwaredomain.MaxPrometheusScrapeTargets)
	}
	for _, target := range targets {
		if err := softwaredomain.ValidateScrapeTarget(target); err != nil {
			return err
		}
	}
	port, present, err := softwaredomain.SpecPort(spec, softwaredomain.SpecScrapePort)
	if err != nil {
		return err
	}
	if len(serverIDs) > 0 && !present {
		return fmt.Errorf("%w: spec.%s requires spec.%s", softwaredomain.ErrSpecInvalid, softwaredomain.SpecScrapeServerIDs, softwaredomain.SpecScrapePort)
	}
	if present && len(serverIDs) == 0 {
		return fmt.Errorf("%w: spec.%s is only valid with spec.%s (port %d)", softwaredomain.ErrSpecInvalid, softwaredomain.SpecScrapePort, softwaredomain.SpecScrapeServerIDs, port)
	}
	return nil
}

// NormalizeSpec records the pinned image tag, the listen address, retention, and job defaults, and
// the target lists as plain string arrays.
func (s Software) NormalizeSpec(spec map[string]any) map[string]any {
	out := softwaredomain.CloneSpec(spec)
	if out == nil {
		out = map[string]any{}
	}
	softwaredomain.NormalizeMethodAndVersion(s.Entry(), out, softwaredomain.DefaultPrometheusVersion)
	softwaredomain.SetDefaultString(out, softwaredomain.SpecListen, string(softwaredomain.PrometheusListenLoopback))
	softwaredomain.SetDefaultString(out, softwaredomain.SpecRetention, softwaredomain.DefaultPrometheusRetention)
	softwaredomain.SetDefaultString(out, softwaredomain.SpecScrapeJob, softwaredomain.DefaultPrometheusScrapeJob)
	targets, _ := softwaredomain.SpecStringList(out, softwaredomain.SpecScrapeTargets)
	out[softwaredomain.SpecScrapeTargets] = softwaredomain.NonNilStrings(targets)
	serverIDs, _ := softwaredomain.SpecStringList(out, softwaredomain.SpecScrapeServerIDs)
	out[softwaredomain.SpecScrapeServerIDs] = softwaredomain.NonNilStrings(serverIDs)
	if port, present, _ := softwaredomain.SpecPort(out, softwaredomain.SpecScrapePort); present {
		out[softwaredomain.SpecScrapePort] = port
	}
	return out
}

// InstallVars passes the normalized spec. The method, version, and job are always present (and the
// runtime for the container method) and the target lists are never nil; the port comes from the
// domain so a composing platform agrees with it.
func (Software) InstallVars(_ map[string][]softwaredomain.Role, spec map[string]any) map[string]any {
	vars := map[string]any{
		"swallow_prometheus_install_method":    softwaredomain.SpecString(spec, softwaredomain.SpecInstallMethod),
		"swallow_prometheus_container_runtime": softwaredomain.SpecString(spec, softwaredomain.SpecContainerRuntime),
		"swallow_prometheus_version":           softwaredomain.SpecString(spec, softwaredomain.SpecVersion),
		"swallow_prometheus_listen":            softwaredomain.SpecString(spec, softwaredomain.SpecListen),
		"swallow_prometheus_retention":         softwaredomain.SpecString(spec, softwaredomain.SpecRetention),
		"swallow_prometheus_port":              softwaredomain.PrometheusPort,
		"swallow_prometheus_scrape_job":        softwaredomain.SpecString(spec, softwaredomain.SpecScrapeJob),
	}
	targets, _ := softwaredomain.SpecStringList(spec, softwaredomain.SpecScrapeTargets)
	vars["swallow_prometheus_scrape_targets"] = softwaredomain.NonNilStrings(targets)
	// Server ids, not addresses: the playbook resolves each from the run's inventory snapshot.
	serverIDs, _ := softwaredomain.SpecStringList(spec, softwaredomain.SpecScrapeServerIDs)
	vars["swallow_prometheus_scrape_server_ids"] = softwaredomain.NonNilStrings(serverIDs)
	if port, present, _ := softwaredomain.SpecPort(spec, softwaredomain.SpecScrapePort); present {
		vars["swallow_prometheus_scrape_port"] = port
	}
	return vars
}

// ServerReferences are the scraped Servers, which the playbook resolves from the target's Site.
func (Software) ServerReferences(spec map[string]any) []softwaredomain.ServerReference {
	serverIDs, _ := softwaredomain.SpecStringList(spec, softwaredomain.SpecScrapeServerIDs)
	refs := make([]softwaredomain.ServerReference, 0, len(serverIDs))
	for _, id := range serverIDs {
		refs = append(refs, softwaredomain.ServerReference{Field: softwaredomain.SpecScrapeServerIDs, ServerID: id})
	}
	return refs
}
