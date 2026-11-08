// Package victoriametrics is the VictoriaMetrics Software module: its install and uninstall
// playbooks under this directory and its Software hook. The kind is one pinned single-node
// VictoriaMetrics per Server, installed natively from the release archive module.json declares. A
// VictoriaMetrics a Slurm job starts and stops for itself is that job's runtime, not this kind.
package victoriametrics

import (
	_ "embed"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	moduledomain "github.com/maple52046/swallow/internal/core/module/domain"
	softwaredomain "github.com/maple52046/swallow/internal/core/software/domain"
)

//go:embed module.json
var manifest []byte

// Kind is the Managed Software kind this module provides and the module's id. Its Workflow kinds and
// playbooks are derived from it and Software Assignments are keyed by it, so renaming it is a
// breaking change.
const Kind softwaredomain.Kind = "victoriametrics"

// Version is the VictoriaMetrics release every install applies. It is the only release module.json
// declares as a file artifact and the role pins, so the three change together; an assignment records
// it.
const Version = "1.155.0"

// Port is where VictoriaMetrics serves its HTTP API (writes, queries, and /health) on the address the
// spec's listen selects. A Grafana reads it there through its prometheusUrl.
const Port = 8428

// DefaultRetention is VictoriaMetrics's own default retention, one month, which an install records
// when the spec names none.
const DefaultRetention = "1M"

// retentionPattern is a retention VictoriaMetrics accepts that also names its unit: a whole number of
// days, weeks, months, or years. VictoriaMetrics reads a bare number as months and m as minutes, and
// stops at start below one day, so neither form is offered. The module's spec schema states the same
// format for the core and the dashboard.
var retentionPattern = regexp.MustCompile(`^([1-9][0-9]{0,4})([dwMy])$`)

// maxRetention bounds a retention at 100 years in each unit. VictoriaMetrics itself refuses more than
// 1200 months at start, which the schema's format cannot express.
var maxRetention = map[string]int{"d": 36500, "w": 5214, "M": 1200, "y": 100}

// Module returns the VictoriaMetrics Software module declared by module.json.
func Module() (moduledomain.Module, error) {
	return moduledomain.Load(manifest, map[string]any{softwaredomain.HookSoftware: Software{}})
}

// Software is VictoriaMetrics's Software hook. It holds no state and does no I/O, so the core calls
// it freely while it plans an install or uninstall.
type Software struct{}

// Entry is VictoriaMetrics's catalog entry: one native install path, no roles, and no install
// restriction of its own. It declares no exporter for the Site Prometheus, because its own metrics
// are on the loopback address unless the spec asks for the management address.
func (Software) Entry() softwaredomain.CatalogEntry {
	return softwaredomain.CatalogEntry{
		Kind:       Kind,
		Label:      "VictoriaMetrics",
		SpecFields: []string{softwaredomain.SpecListen, softwaredomain.SpecRetention},
	}
}

// ValidateSpec bounds the retention. The core checks the spec against the module's spec schema first
// (known keys, string values, the listen choices, and the retention's format), so the bound is the
// only rule left; reading the retention checks its format again.
func (Software) ValidateSpec(_ map[string][]softwaredomain.Role, spec map[string]any) error {
	retention := strings.TrimSpace(softwaredomain.SpecString(spec, softwaredomain.SpecRetention))
	if retention == "" {
		return nil
	}
	match := retentionPattern.FindStringSubmatch(retention)
	if match == nil {
		return fmt.Errorf("%w: spec.%s %q must be a whole number with d, w, M, or y, such as 30d or 1M",
			softwaredomain.ErrSpecInvalid, softwaredomain.SpecRetention, retention)
	}
	count, _ := strconv.Atoi(match[1]) // at most five digits, so it always parses
	if limit := maxRetention[match[2]]; count > limit {
		return fmt.Errorf("%w: spec.%s %q is longer than 100 years (at most %d%s)",
			softwaredomain.ErrSpecInvalid, softwaredomain.SpecRetention, retention, limit, match[2])
	}
	return nil
}

// NormalizeSpec records the pinned release and the listen address and retention the install
// applies, so the assignment never disagrees with what the playbook ran.
func (Software) NormalizeSpec(spec map[string]any) map[string]any {
	out := softwaredomain.CloneSpec(spec)
	if out == nil {
		out = map[string]any{}
	}
	out[softwaredomain.SpecVersion] = Version
	// The same listen choice as Prometheus: an API without authentication stays on loopback unless
	// the management address is asked for.
	softwaredomain.SetDefaultString(out, softwaredomain.SpecListen, string(softwaredomain.PrometheusListenLoopback))
	softwaredomain.SetDefaultString(out, softwaredomain.SpecRetention, DefaultRetention)
	return out
}

// InstallVars passes the recorded release, listen address, and retention, and the port. A record
// this module wrote carries all three; an uninstall may pass a record without a spec, so each falls
// back to what an install would have recorded.
func (s Software) InstallVars(_ map[string][]softwaredomain.Role, spec map[string]any) map[string]any {
	recorded := s.NormalizeSpec(nil)
	for _, key := range []string{softwaredomain.SpecVersion, softwaredomain.SpecListen, softwaredomain.SpecRetention} {
		if value := strings.TrimSpace(softwaredomain.SpecString(spec, key)); value != "" {
			recorded[key] = value
		}
	}
	return map[string]any{
		"swallow_victoriametrics_version":   recorded[softwaredomain.SpecVersion],
		"swallow_victoriametrics_listen":    recorded[softwaredomain.SpecListen],
		"swallow_victoriametrics_retention": recorded[softwaredomain.SpecRetention],
		"swallow_victoriametrics_port":      Port,
	}
}
