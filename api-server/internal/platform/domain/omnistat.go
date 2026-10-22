package domain

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Omnistat on a Swallow-deployed Slurm Platform (decision 063, glossary Omnistat). The exporter and
// the Slurm job attribution belong to the Platform; Prometheus and Grafana are Managed Software the
// same deployment composes onto the observability Server (the primary controller).
const (
	// OmnistatVersion is the Omnistat release Swallow installs. The playbook pins its source archive,
	// SR-IOV compatibility patch, and Python dependency set per version.
	OmnistatVersion = "1.15.0"
	// OmnistatExporterPort is where every compute node's exporter listens. It must not be one of the
	// host exporter ports Swallow scrapes (9100 node, 5000 RDC), decision 003.
	OmnistatExporterPort = 8001
	// OmnistatGrafanaLinkName is the Monitoring Link a successful install registers and an
	// uninstall removes; other links stay operator-owned.
	OmnistatGrafanaLinkName = "Omnistat Grafana"
	// OmnistatScrapeJob is the Prometheus job name of the exporters.
	OmnistatScrapeJob = "omnistat"
	// OmnistatDashboardBundle is the Grafana dashboard bundle with Omnistat's RMS dashboards.
	OmnistatDashboardBundle = "omnistat"
)

// OmnistatState is where the latest Swallow change to a Platform's Omnistat stands, projected from
// the Workflow Task that installs it. Values are the glossary's Omnistat State.
type OmnistatState string

const (
	OmnistatInstalling OmnistatState = "installing"
	OmnistatReady      OmnistatState = "ready"
	OmnistatFailed     OmnistatState = "failed"
	// OmnistatAbsent means the Platform was uninstalled, which removed the exporter.
	OmnistatAbsent OmnistatState = "absent"
)

// OmnistatInstallMethod is how the deployment installs the Prometheus and Grafana Omnistat uses:
// the Managed Software install method of both kinds.
type OmnistatInstallMethod string

const (
	// OmnistatInstallContainer runs both as containers on the observability Server's container
	// runtime. It is the default.
	OmnistatInstallContainer OmnistatInstallMethod = "container"
	// OmnistatInstallNative installs both from their official release and package repository.
	OmnistatInstallNative OmnistatInstallMethod = "native"
)

// OmnistatSettings is the operator's Omnistat intent in a Slurm deploy request. An empty
// InstallMethod is the container method.
type OmnistatSettings struct {
	Enabled       bool
	InstallMethod OmnistatInstallMethod
}

// NormalizeOmnistatSettings defaults the install method and refuses an unknown one.
func NormalizeOmnistatSettings(settings OmnistatSettings) (OmnistatSettings, error) {
	method := OmnistatInstallMethod(strings.TrimSpace(string(settings.InstallMethod)))
	switch method {
	case "":
		method = OmnistatInstallContainer
	case OmnistatInstallContainer, OmnistatInstallNative:
	default:
		return OmnistatSettings{}, fmt.Errorf("%w: installMethod must be %s or %s, got %q", ErrInvalidOmnistat,
			OmnistatInstallContainer, OmnistatInstallNative, settings.InstallMethod)
	}
	return OmnistatSettings{Enabled: settings.Enabled, InstallMethod: method}, nil
}

// Omnistat is the Platform's Swallow-managed Omnistat record. ObservabilityServerID names the
// Server running Prometheus and Grafana, installed by InstallMethod. LastWorkflowID is the fence:
// only that Workflow's Task may move the State. Written only through OmnistatRecorder, never by a
// Platform Update.
type Omnistat struct {
	Enabled               bool
	Version               string
	InstallMethod         OmnistatInstallMethod
	ObservabilityServerID string
	State                 OmnistatState
	LastWorkflowID        string
	UpdatedAt             time.Time
}

// Managed reports whether Swallow currently manages Omnistat on the Platform. A nil record (never
// enabled) and an absent one (uninstalled) are both unmanaged.
func (o *Omnistat) Managed() bool {
	return o != nil && o.State != OmnistatAbsent
}

// OmnistatLaunch adds Omnistat to a Slurm deployment Workflow: Prometheus and Grafana composed onto
// ObservabilityServerID by InstallMethod (the container method also composes that Server's
// container runtime), then the exporter on every ComputeServerIDs node. TrustedVars are the
// configure-omnistat playbook's swallow_omnistat_* variables.
type OmnistatLaunch struct {
	ObservabilityServerID string
	ComputeServerIDs      []string
	InstallMethod         OmnistatInstallMethod
	TrustedVars           map[string]any
}

// OmnistatRecorder persists a Platform's Omnistat record. It is separate from PlatformRepository so
// the record has exactly two writers: the deploy use case accepting it and the observer of the Task
// applying it (plus the uninstall completion marking it absent).
type OmnistatRecorder interface {
	// SetOmnistat replaces the record; nil removes it. It returns ErrPlatformNotFound for a missing
	// Platform.
	SetOmnistat(ctx context.Context, platformID string, omnistat *Omnistat) error
	// UpdateOmnistatState moves the state only while the record's LastWorkflowID equals workflowID
	// and reports whether it did. A missing Platform or a different latest Workflow is not an error:
	// the observer of a superseded or deleted Platform's Task has nothing to do.
	UpdateOmnistatState(ctx context.Context, platformID, workflowID string, state OmnistatState, at time.Time) (bool, error)
}

// UpsertMonitoringLink returns links with the link of the same name replaced, or the link appended
// when no link has that name. The result is not yet normalized; callers run
// NormalizeMonitoringLinks, which enforces the count limit.
func UpsertMonitoringLink(links []MonitoringLink, link MonitoringLink) []MonitoringLink {
	out := make([]MonitoringLink, 0, len(links)+1)
	replaced := false
	for _, existing := range links {
		if existing.Name == link.Name {
			out = append(out, link)
			replaced = true
			continue
		}
		out = append(out, existing)
	}
	if !replaced {
		out = append(out, link)
	}
	return out
}

// RemoveMonitoringLink returns links without the link named name.
func RemoveMonitoringLink(links []MonitoringLink, name string) []MonitoringLink {
	out := make([]MonitoringLink, 0, len(links))
	for _, existing := range links {
		if existing.Name != name {
			out = append(out, existing)
		}
	}
	return out
}

var (
	// ErrInvalidOmnistat means an Omnistat request cannot apply, for example on a Kubernetes
	// Platform or when the process cannot record it.
	ErrInvalidOmnistat = errors.New("invalid Omnistat request")
)
