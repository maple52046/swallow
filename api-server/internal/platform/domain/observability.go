package domain

import (
	"fmt"
	"strings"
)

// ObservabilityInstallMethod is how a Slurm deployment installs the Prometheus and Grafana its
// monitoring feature uses (Omnistat, decision 063, or the Device Metrics Exporter, decision 066):
// the Managed Software install method of both kinds. The features are mutually exclusive, so a
// deployment has at most one.
type ObservabilityInstallMethod string

const (
	// ObservabilityInstallContainer runs both as containers on the observability Server's container
	// runtime. It is the default.
	ObservabilityInstallContainer ObservabilityInstallMethod = "container"
	// ObservabilityInstallNative installs both from their official release and package repository.
	ObservabilityInstallNative ObservabilityInstallMethod = "native"
)

// normalizeObservabilityInstallMethod defaults an empty method to the container method and refuses
// an unknown one with invalid, the requesting feature's sentinel, so the delivery layer reports the
// feature the operator asked for.
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
