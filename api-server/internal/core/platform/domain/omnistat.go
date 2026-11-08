package domain

import (
	"context"
	"errors"
	"slices"
	"time"
)

// Omnistat on a swallow-deployed Slurm Platform (decision 063, glossary Omnistat). The exporter and
// the Slurm job attribution belong to the Platform; the Site metrics store scrapes the exporters
// (decision 003), and the Platform's own dashboards, when it has them, show them.
const (
	// OmnistatVersion is the Omnistat release swallow installs. The playbook pins its source archive,
	// SR-IOV compatibility patch, and Python dependency set per version.
	OmnistatVersion = "1.15.0"
	// OmnistatExporterPort is where every compute node's exporter listens. It must not be one of the
	// host exporter ports swallow scrapes (9100 node, 5000 RDC), decision 003.
	OmnistatExporterPort = 8001
	// OmnistatScrapeJob is the exporter's name in the scrape targets (the exporter label).
	OmnistatScrapeJob = "omnistat"
	// OmnistatDashboardBundle is the Grafana dashboard bundle with Omnistat's RMS dashboards.
	OmnistatDashboardBundle = "omnistat"
)

// OmnistatState is where the latest swallow change to a Platform's Omnistat stands, projected from
// the Workflow Task that installs it. Values are the glossary's Omnistat State.
type OmnistatState string

const (
	OmnistatInstalling OmnistatState = "installing"
	OmnistatReady      OmnistatState = "ready"
	OmnistatFailed     OmnistatState = "failed"
	// OmnistatAbsent means the Platform was uninstalled, which removed the exporter.
	OmnistatAbsent OmnistatState = "absent"
)

// OmnistatSettings is the user's Omnistat intent in a Slurm deploy request. InstallMethod chooses
// how the Platform's own dashboards are installed; empty is the container method.
// HardwareCounters asks each compute node to also collect the GPU hardware counters (glossary
// Omnistat Hardware Counters); false when the request omits it.
type OmnistatSettings struct {
	Enabled          bool
	InstallMethod    ObservabilityInstallMethod
	HardwareCounters bool
}

// NormalizeOmnistatSettings defaults the install method and refuses an unknown one.
func NormalizeOmnistatSettings(settings OmnistatSettings) (OmnistatSettings, error) {
	method, err := normalizeObservabilityInstallMethod(settings.InstallMethod, ErrInvalidOmnistat)
	if err != nil {
		return OmnistatSettings{}, err
	}
	return OmnistatSettings{Enabled: settings.Enabled, InstallMethod: method, HardwareCounters: settings.HardwareCounters}, nil
}

// HardwareCounterState is whether a compute node collects the GPU hardware counters its deployment
// asked for. Values are the glossary's (Omnistat Hardware Counters).
type HardwareCounterState string

const (
	HardwareCountersCollecting  HardwareCounterState = "collecting"
	HardwareCountersUnavailable HardwareCounterState = "unavailable"
)

// HardwareCounterNode is one compute node's outcome as its install reported it. Reason is empty while
// collecting and otherwise one of the glossary's reasons; the reasons name the vendor's runtime, so
// the composition root, which reads them from the install's result, owns their list.
type HardwareCounterNode struct {
	ServerID string
	State    HardwareCounterState
	Reason   string
}

// HardwareCounters is the hardware counters part of the record: Requested is the deployment's intent,
// written when the deployment is accepted; Nodes is observed, written only by the install's
// completion under the record's fence, and empty until it ran.
type HardwareCounters struct {
	Requested bool
	Nodes     []HardwareCounterNode
}

// Omnistat is the Platform's swallow-managed Omnistat record. ObservabilityServerID names the
// primary controller, which hosts the Platform's own dashboards (installed by InstallMethod) and
// is where omnistat-query reads them. LastWorkflowID is the fence: only that Workflow's Task may
// move the State. Written only through OmnistatRecorder, never by a Platform Update.
type Omnistat struct {
	Enabled               bool
	Version               string
	InstallMethod         ObservabilityInstallMethod
	ObservabilityServerID string
	State                 OmnistatState
	HardwareCounters      HardwareCounters
	LastWorkflowID        string
	UpdatedAt             time.Time
}

// Managed reports whether swallow currently manages Omnistat on the Platform. A nil record (never
// enabled) and an absent one (uninstalled) are both unmanaged.
func (o *Omnistat) Managed() bool {
	return o != nil && o.State != OmnistatAbsent
}

// OmnistatLaunch adds Omnistat to a Slurm deployment Workflow: the exporter on every
// ComputeServerIDs node, admitting only the node itself and the Site metrics store.
// ObservabilityServerID is the primary controller, whose dashboards omnistat-query reads, and
// InstallMethod how those dashboards are installed. TrustedVars are the configure-omnistat
// playbook's swallow_omnistat_* variables the deploy use case owns; HardwareCounters is the
// request's intent, which the composition root hands the playbook with the variable it names.
type OmnistatLaunch struct {
	ObservabilityServerID string
	ComputeServerIDs      []string
	InstallMethod         ObservabilityInstallMethod
	HardwareCounters      bool
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
	// SetHardwareCounterNodes replaces the nodes' hardware counters outcome under the same fence as
	// the state and reports whether it did; like the state, a missing Platform or a different latest
	// Workflow changes nothing and is not an error. Requested is left as the deployment wrote it.
	SetHardwareCounterNodes(ctx context.Context, platformID, workflowID string, nodes []HardwareCounterNode, at time.Time) (bool, error)
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

// RemoveMonitoringLinks returns links without the links named in names.
func RemoveMonitoringLinks(links []MonitoringLink, names ...string) []MonitoringLink {
	out := make([]MonitoringLink, 0, len(links))
	for _, existing := range links {
		if !slices.Contains(names, existing.Name) {
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
