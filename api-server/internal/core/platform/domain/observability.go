package domain

import (
	"errors"
	"fmt"
	"strings"
)

// ObservabilityInstallMethod is how a Slurm deployment installs its Platform's own Grafana (the
// Managed Software install method of that kind). The Slurm monitoring features (Omnistat, decision
// 063, and the Device Metrics Exporter, decision 066) carry it; without either, the Grafana takes
// the container method.
type ObservabilityInstallMethod string

const (
	// ObservabilityInstallContainer runs it as a container on the primary controller's container
	// runtime. It is the default.
	ObservabilityInstallContainer ObservabilityInstallMethod = "container"
	// ObservabilityInstallNative installs it from its official package repository.
	ObservabilityInstallNative ObservabilityInstallMethod = "native"
)

// normalizeObservabilityInstallMethod defaults an empty method to the container method and refuses
// an unknown one with invalid, the requesting feature's sentinel, so the delivery layer reports the
// feature the user asked for.
func normalizeObservabilityInstallMethod(method ObservabilityInstallMethod, invalid error) (ObservabilityInstallMethod, error) {
	normalized := ObservabilityInstallMethod(strings.TrimSpace(string(method)))
	switch normalized {
	case "":
		return ObservabilityInstallContainer, nil
	case ObservabilityInstallContainer, ObservabilityInstallNative:
		return normalized, nil
	default:
		return "", fmt.Errorf("%w: installMethod must be %s or %s, got %q", invalid,
			ObservabilityInstallContainer, ObservabilityInstallNative, method)
	}
}

// MonitoringDashboardsLaunch gives a deployment's Platform its own monitoring dashboards
// (monitoring.md §6.2): a read-only dashboard server whose datasource reads the Site metrics store
// through swallow's Platform metrics gateway with the Platform's monitoring token, so it sees only
// the Platform's members. Every Platform may have one, GPU or not; the Platform spec's
// monitoringDashboards setting turns it off. How it is delivered belongs to the Platform type: a
// Slurm Platform runs it as Managed Software on ServerID (the primary controller) by InstallMethod;
// a Kubernetes Platform runs it in the cluster, installed from ServerID (the initial controller) and
// reached on its EndpointServerIDs, with the GPU dashboards when GPUMetrics is set.
type MonitoringDashboardsLaunch struct {
	ServerID      string
	InstallMethod ObservabilityInstallMethod
	// EndpointServerIDs are the members users reach the dashboards on, in order, the first being
	// the one the Monitoring Link names; empty means ServerID itself.
	EndpointServerIDs []string
	// GPUMetrics means the Platform runs a GPU exporter its type does not otherwise describe to the
	// launcher (the GPU Operator's Device Metrics Exporter), so the dashboards include its bundle.
	GPUMetrics bool
}

// ErrMonitoringEndpointMissing means a deployment would install monitoring that must reach swallow
// or be reached by the Site metrics store — a Platform's dashboards read the metrics gateway at the
// installation's public URL, and an exporter with an allowlist admits the store's scrape addresses —
// but the installation has not configured that address. The wrapped message names the setting.
var ErrMonitoringEndpointMissing = errors.New("the installation has not configured how its Servers and its Site metrics store reach each other")
